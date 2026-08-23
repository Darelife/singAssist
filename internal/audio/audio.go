package audio

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"singAssist/internal/config"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
)

var AudioContext *audio.Context

func init() {
	AudioContext = audio.NewContext(config.SampleRate)
}

type Mode int

const (
	ModeSinging Mode = iota
	ModeInstrumental
	ModeFullMix
	ModeNoAudio
)

/*
LoadResult contains the results from loading and analyzing a song.

Fields:
  - Player: Ebiten audio player for playback (nil for ModeNoAudio)
  - SongPitch: Slice of pitch values at 10ms intervals (100 samples/second)
*/
type LoadResult struct {
	Player    *audio.Player
	SongPitch []float64
}

/*
LoadAndAnalyzeSong loads audio from a song directory and analyzes pitch.

Input:
  - songDir: string - Path to song directory (e.g., "songs/MySong")
  - mode: Mode - Playback mode (ModeSinging, ModeInstrumental, ModeFullMix, ModeNoAudio)
  - onMessage: func(string) - Callback for status messages (can be nil)

Called by:
  - app.calibrateAndPlay after microphone calibration completes

Task:
  - Load appropriate audio file based on mode
  - Run audio separation if needed (vocals/accompaniment)
  - Create audio player for playback
  - Analyze pitch throughout the song

Logic:
 1. Get file paths from config.GetSongPaths
 2. For ModeSinging/ModeInstrumental: check if separated files exist
 3. If separation needed: run separate.py using config.GetPythonPath
 4. Open appropriate audio file (vocals/accompaniment/original)
 5. Decode MP3 to PCM data
 6. Create ebiten audio.Player from PCM (skip for ModeNoAudio)
 7. Run analyzePitch to extract pitch contour

Output:
  - *LoadResult: Contains Player and SongPitch data
  - error: nil on success, descriptive error on failure
*/
func LoadAndAnalyzeSong(songDir string, mode Mode, onMessage func(string)) (*LoadResult, error) {
	paths := config.GetSongPaths(songDir)
	var audioFile string

	if mode == ModeSinging || mode == ModeInstrumental {
		needsSeparation := false
		if mode == ModeSinging {
			if _, err := os.Stat(paths.VocalsFile); os.IsNotExist(err) {
				needsSeparation = true
			}
		} else {
			if _, err := os.Stat(paths.AccompFile); os.IsNotExist(err) {
				needsSeparation = true
			}
		}

		if needsSeparation {
			log.Println("Running audio separation (this may take a minute)...")
			if onMessage != nil {
				onMessage("Separating audio (may take a minute)...")
			}

			pythonCmd := config.GetPythonPath()
			log.Printf("Using Python: %s", pythonCmd)

			cmd := exec.Command(pythonCmd, "separate.py", paths.SongFile, songDir)
			output, err := cmd.CombinedOutput()
			log.Printf("Separator output: %s", string(output))

			if err != nil {
				return nil, fmt.Errorf("separation failed: %v\nOutput: %s", err, string(output))
			}
		}

		if mode == ModeSinging {
			audioFile = paths.VocalsFile
			log.Println("Using vocals track")
		} else {
			audioFile = paths.AccompFile
			log.Println("Using accompaniment track")
		}
	} else {
		audioFile = paths.SongFile
		log.Println("Using full mix")
	}

	f, err := os.Open(audioFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", audioFile, err)
	}
	defer f.Close()

	d, err := mp3.DecodeWithoutResampling(f)
	if err != nil {
		return nil, err
	}

	var pcmData bytes.Buffer
	if _, err := io.Copy(&pcmData, d); err != nil {
		return nil, err
	}
	pcmBytes := pcmData.Bytes()

	result := &LoadResult{}

	if mode != ModeNoAudio {
		playerRead := bytes.NewReader(pcmBytes)
		result.Player, err = AudioContext.NewPlayer(playerRead)
		if err != nil {
			return nil, err
		}
	}

	result.SongPitch = analyzePitch(pcmBytes, mode)

	return result, nil
}

/*
analyzePitch extracts pitch values from PCM audio data.

Input:
  - pcmBytes: []byte - Raw PCM audio data (16-bit stereo, 44100Hz)
  - mode: Mode - Used to adjust frequency range and energy thresholds

Called by:
  - LoadAndAnalyzeSong after loading PCM data

Task:
  - Process audio in 30ms chunks for ~3x faster analysis
  - Detect fundamental frequency per chunk via FFT-accelerated pYIN
    (multi-threshold candidate generation)
  - Viterbi-decode each contiguous voiced segment to smooth the pitch
    track and suppress octave jumps, then triplicate each value to
    maintain 10ms output timing

Logic:
 1. Calculate step size: 30ms chunks (1323 samples * 4 bytes = 5292 bytes)
 2. For each chunk:
    a. Convert bytes to float32 samples (left channel only)
    b. If energy is below threshold: flush any buffered voiced segment
    through Viterbi decoding, then record silence
    c. Otherwise: compute weighted pitch candidates (yinDiffFFT +
    pyinCandidates) and buffer them into the current segment
 3. Flush the final segment once chunk processing ends
 4. Filter non-vocal frequencies for ModeSinging
 5. Append each decoded pitch value 3 times to maintain 10ms timing
 6. Apply gap-filling for instrumental/full mix modes

Output:
  - []float64: Pitch values at 10ms intervals (100 per second)
*/
func analyzePitch(pcmBytes []byte, mode Mode) []float64 {
	stepBytes := int(float64(config.SampleRate)*0.03) * 4
	totalSamples := len(pcmBytes) / 4

	minF, maxF := 40.0, 2000.0
	if mode == ModeSinging {
		minF = 100.0
		maxF = 1200.0
	}
	minPeriod := int(float64(config.SampleRate) / maxF)
	maxPeriod := int(float64(config.SampleRate) / minF)

	minEnergy := calibrateSilenceFromAudio(pcmBytes, stepBytes, mode)
	log.Printf("Calibrated silence threshold: %.6f", minEnergy)

	numChunks := totalSamples / (stepBytes / 4)
	chunkPitch := make([]float64, 0, numChunks)
	floatBuf := make([]float32, stepBytes/4)

	var segment [][]PitchCandidate
	flushSegment := func() {
		if len(segment) == 0 {
			return
		}
		decoded := viterbiDecodeSegment(segment)
		for _, f := range decoded {
			if mode == ModeSinging && (f < 80 || f > 1000) {
				f = 0
			}
			chunkPitch = append(chunkPitch, f)
		}
		segment = nil
	}

	startTime := time.Now()
	log.Println("Starting pitch analysis...")

	for i := 0; i < len(pcmBytes)-stepBytes; i += stepBytes {
		chunk := pcmBytes[i : i+stepBytes]
		for j := 0; j < len(chunk); j += 4 {
			s16 := int16(chunk[j]) | int16(chunk[j+1])<<8
			floatBuf[j/4] = float32(s16) / 32768.0
		}

		energy := CalculateEnergy(floatBuf)
		if energy < minEnergy {
			flushSegment()
			chunkPitch = append(chunkPitch, 0)
			continue
		}

		diff := yinDiffFFT(floatBuf, maxPeriod)
		cmndf := cmndfFromDiff(diff)
		cands := pyinCandidates(cmndf, minPeriod, maxPeriod, config.SampleRate)
		if len(cands) == 0 {
			flushSegment()
			chunkPitch = append(chunkPitch, 0)
			continue
		}

		segment = append(segment, cands)
	}
	flushSegment()

	songPitch := make([]float64, 0, len(chunkPitch)*3)
	for _, p := range chunkPitch {
		songPitch = append(songPitch, p, p, p)
	}

	if mode == ModeInstrumental || mode == ModeFullMix {
		songPitch = fillShortGaps(songPitch, 20)
	}

	log.Printf("Analysis done in %v", time.Since(startTime))
	return songPitch
}

/*
calibrateSilenceFromAudio samples the audio to find a good silence threshold.

Input:
  - pcmBytes: []byte - Raw PCM audio data
  - stepBytes: int - Size of each analysis chunk
  - mode: Mode - Current playback mode

Called by:
  - analyzePitch at the start of analysis

Task:
  - Sample audio energy and determine adaptive silence threshold

Logic:
 1. Sample first 5 seconds of audio
 2. Calculate energy for each chunk
 3. Find 10th percentile as baseline noise
 4. Return threshold above baseline

Output:
  - float64: Energy threshold for silence detection
*/
func calibrateSilenceFromAudio(pcmBytes []byte, stepBytes int, mode Mode) float64 {
	sampleCount := 500
	if len(pcmBytes)/stepBytes < sampleCount {
		sampleCount = len(pcmBytes) / stepBytes
	}
	energies := make([]float64, 0, sampleCount)
	floatBuf := make([]float32, stepBytes/4)

	for i := 0; i < sampleCount*stepBytes && i < len(pcmBytes)-stepBytes; i += stepBytes {
		chunk := pcmBytes[i : i+stepBytes]
		for j := 0; j < len(chunk); j += 4 {
			s16 := int16(chunk[j]) | int16(chunk[j+1])<<8
			floatBuf[j/4] = float32(s16) / 32768.0
		}
		energies = append(energies, CalculateEnergy(floatBuf))
	}

	if len(energies) == 0 {
		if mode == ModeSinging {
			return 0.005
		}
		return 0.001
	}

	sortedEnergies := make([]float64, len(energies))
	copy(sortedEnergies, energies)
	for i := 0; i < len(sortedEnergies)-1; i++ {
		for j := i + 1; j < len(sortedEnergies); j++ {
			if sortedEnergies[j] < sortedEnergies[i] {
				sortedEnergies[i], sortedEnergies[j] = sortedEnergies[j], sortedEnergies[i]
			}
		}
	}

	percentile10 := sortedEnergies[len(sortedEnergies)/10]

	threshold := percentile10 * 3.0
	if mode == ModeSinging && threshold < 0.005 {
		threshold = 0.005
	}
	if mode != ModeSinging && threshold < 0.001 {
		threshold = 0.001
	}

	return threshold
}

/*
fillShortGaps interpolates pitch values across short silence gaps.

Input:
  - pitches: []float64 - Raw pitch data with 0s for silence
  - maxGapFrames: int - Maximum gap size to fill (in 10ms frames)

Called by:
  - analyzePitch for instrumental/full mix modes

Task:
  - Fill short gaps to prevent spiky visualization

Logic:
 1. Find sequences of zeros
 2. If gap is shorter than maxGapFrames and has valid pitches on both sides
 3. Interpolate linearly between the boundary pitches

Output:
  - []float64: Pitch data with short gaps filled
*/
func fillShortGaps(pitches []float64, maxGapFrames int) []float64 {
	result := make([]float64, len(pitches))
	copy(result, pitches)

	i := 0
	for i < len(result) {
		if result[i] <= 0 {
			gapStart := i
			for i < len(result) && result[i] <= 0 {
				i++
			}
			gapEnd := i
			gapLen := gapEnd - gapStart

			if gapLen <= maxGapFrames && gapStart > 0 && gapEnd < len(result) {
				startPitch := result[gapStart-1]
				endPitch := result[gapEnd]

				if startPitch > 0 && endPitch > 0 {
					for j := gapStart; j < gapEnd; j++ {
						t := float64(j-gapStart+1) / float64(gapLen+1)
						result[j] = startPitch + t*(endPitch-startPitch)
					}
				}
			}
		} else {
			i++
		}
	}

	return result
}

/*
CalculateEnergy computes the average power of audio samples.

Input:
  - samples: []float32 - Audio samples normalized to [-1, 1]

Called by:
  - analyzePitch when filtering silence
  - MicHandler.Calibrate when measuring background noise
  - MicHandler.DetectPitchFromMic when gating noise

Task:
  - Calculate RMS-squared energy for voice activity detection

Logic:
 1. Sum squares of all samples
 2. Divide by sample count for average

Output:
  - float64: Average energy (0.0 = silence, higher = louder)
*/
func CalculateEnergy(samples []float32) float64 {
	e := 0.0
	for _, s := range samples {
		e += float64(s * s)
	}
	return e / float64(len(samples))
}
