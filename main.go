package main

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font/basicfont"
)

const (
	sampleRate     = 44100
	bufferSize     = 2048
	analysisChunk  = 1024
	screenW        = 1000
	screenH        = 600
	pixelsPerSec   = 150.0
	analysisStep   = 441
	latencyCorrect = 0.1
)

// Game States
type GameState int

const (
	StateStartScreen GameState = iota
	StateCalibrating
	StatePlaying
)

// Modes
type Mode int

const (
	ModeSinging Mode = iota
	ModeInstrumental
	ModeFullMix
)

var (
	audioContext *audio.Context
)

type App struct {
	state       GameState
	mode        Mode
	audioPlayer *audio.Player
	songFile    string // Path to the song file

	// Data
	songPitch []float64
	userPitch []float64 // [TimeMs, Pitch, TimeMs, Pitch...]

	// Mic
	micStream *portaudio.Stream
	micBuffer []float32
	micPitch  float64

	// Noise Calibration
	calibrationEnd time.Time
	noiseThreshold float64
	noiseFreq      float64

	// Stabilization
	pitchSmoother *Smoother

	mu      sync.Mutex
	message string
}

func init() {
	audioContext = audio.NewContext(sampleRate)
}

func main() {
	portaudio.Initialize()
	defer portaudio.Terminate()

	// Parse command line args
	songFile := "song.mp3"
	if len(os.Args) > 1 {
		songFile = os.Args[1]
	}

	app := &App{
		state:         StateStartScreen,
		songFile:      songFile,
		micBuffer:     make([]float32, bufferSize),
		userPitch:     make([]float64, 0),
		pitchSmoother: NewSmoother(5),
	}

	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("SingAssist")
	if err := ebiten.RunGame(app); err != nil {
		log.Fatal(err)
	}
}

func (a *App) Update() error {
	sw, sh := ebiten.WindowSize()

	// Input Handling for Start Screen
	if a.state == StateStartScreen {
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			x, y := ebiten.CursorPosition()
			// Button 1: Singing
			if inRect(x, y, sw/2-100, sh/2-90, 200, 50) {
				a.startGame(ModeSinging)
			}
			// Button 2: Instrumental
			if inRect(x, y, sw/2-100, sh/2-30, 200, 50) {
				a.startGame(ModeInstrumental)
			}
			// Button 3: Full Mix
			if inRect(x, y, sw/2-100, sh/2+30, 200, 50) {
				a.startGame(ModeFullMix)
			}
		}
	} else if a.state == StatePlaying {
		// Fullscreen toggle (F key)
		if inpututil.IsKeyJustPressed(ebiten.KeyF) {
			ebiten.SetFullscreen(!ebiten.IsFullscreen())
		}

		// Pause/Play (Space key)
		if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
			if a.audioPlayer != nil {
				if a.audioPlayer.IsPlaying() {
					a.audioPlayer.Pause()
				} else {
					a.audioPlayer.Play()
				}
			}
		}

		// Rewind 10s (Left arrow)
		if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
			if a.audioPlayer != nil {
				pos := a.audioPlayer.Position()
				newPos := pos - 10*time.Second
				if newPos < 0 {
					newPos = 0
				}
				a.audioPlayer.SetPosition(newPos)
			}
		}

		// Forward 10s (Right arrow)
		if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
			if a.audioPlayer != nil {
				pos := a.audioPlayer.Position()
				a.audioPlayer.SetPosition(pos + 10*time.Second)
			}
		}

		// Exit (ESC key)
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			ebiten.SetFullscreen(false)
			a.state = StateStartScreen
			if a.audioPlayer != nil {
				a.audioPlayer.Pause()
				a.audioPlayer.Close()
				a.audioPlayer = nil
			}
			if a.micStream != nil {
				a.micStream.Close() // Stop mic
				a.micStream = nil
			}
		}
	}

	return nil
}

func (a *App) startGame(m Mode) {
	a.mode = m
	a.state = StateCalibrating
	a.calibrationEnd = time.Now().Add(2 * time.Second)
	a.message = "Calibrating background noise..."
	a.userPitch = make([]float64, 0)
	a.pitchSmoother.Reset()

	// Start Mic
	var err error
	a.micStream, err = portaudio.OpenDefaultStream(1, 0, sampleRate, len(a.micBuffer), a.micBuffer)
	if err != nil {
		log.Fatal(err)
	}
	a.micStream.Start()
	go a.micLoop()
}

func (a *App) startPlayback() {
	a.state = StatePlaying
	a.message = "Loading Song..."

	go func() {
		if err := a.loadAndAnalyzeSong(a.songFile); err != nil {
			a.mu.Lock()
			a.message = "Error: " + err.Error()
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.message = ""
		a.audioPlayer.Play()
		a.mu.Unlock()
	}()
}

func (a *App) micLoop() {
	// Temporary buffer for calibration
	var calibrationEnergies []float64
	var calibrationFreqs []float64

	for {
		if a.micStream == nil {
			return
		}
		a.micStream.Read()

		// Calibration Phase
		if a.state == StateCalibrating {
			energy := calculateEnergy(a.micBuffer)
			calibrationEnergies = append(calibrationEnergies, energy)

			// Estimate dominant freq to ignore hums
			f := detectPitch(a.micBuffer, 40, 2000)
			if f > 0 {
				calibrationFreqs = append(calibrationFreqs, f)
			}

			if time.Now().After(a.calibrationEnd) {
				// Finish Calibration
				maxE := 0.0
				for _, e := range calibrationEnergies {
					if e > maxE {
						maxE = e
					}
				}
				a.noiseThreshold = maxE * 1.5 // Safety margin

				// Find modal freq? Simple Avg for now if consistent
				// Or actually, simplistic: just trust the threshold.
				// Most noise is broadband or mains hum (50/60hz).
				// We'll set a min energy requirement.

				a.mu.Lock()
				a.startPlayback()
				a.mu.Unlock()
			}
			continue
		}

		if a.state != StatePlaying {
			continue
		}

		// Detection
		minF, maxF := 40.0, 2000.0
		if a.mode == ModeSinging {
			minF, maxF = 85.0, 1100.0
		}

		// 1. Gate noise
		energy := calculateEnergy(a.micBuffer)
		if energy < a.noiseThreshold {
			a.mu.Lock()
			a.micPitch = 0
			a.mu.Unlock()
			continue
		}

		rawPitch := detectPitch(a.micBuffer, minF, maxF)
		smoothedPitch := a.pitchSmoother.Smooth(rawPitch)

		// Ignore if still deemed noise or jumpy?

		a.mu.Lock()
		a.micPitch = smoothedPitch

		if a.audioPlayer != nil && a.audioPlayer.IsPlaying() {
			pos := a.audioPlayer.Position()
			a.userPitch = append(a.userPitch, float64(pos.Milliseconds()), smoothedPitch)
		}
		a.mu.Unlock()
	}
}

func (a *App) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)

	sw, sh := ebiten.WindowSize()

	if a.state == StateStartScreen {
		drawButton(screen, sw/2-100, sh/2-90, 200, 50, "Vocals Only", color.RGBA{0, 200, 100, 255})
		drawButton(screen, sw/2-100, sh/2-30, 200, 50, "Instrumental", color.RGBA{100, 100, 200, 255})
		drawButton(screen, sw/2-100, sh/2+30, 200, 50, "Full Mix", color.RGBA{200, 100, 100, 255})
		return
	}

	if a.state == StateCalibrating {
		msg := "Calibrating Silence...\nPlease stay quiet."
		text.Draw(screen, msg, basicfont.Face7x13, sw/2-60, sh/2, color.White)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.message != "" {
		ebitenutil.DebugPrint(screen, a.message)
	}

	if a.audioPlayer == nil || !a.audioPlayer.IsPlaying() {
		return
	}

	// Visualization
	currTime := a.audioPlayer.Position().Seconds()

	// Stats (Top Left)
	playNote, _ := freqToNote(a.micPitch)
	songNoteStr := "--"
	songFreq := 0.0

	// Find Song Pitch at current time
	// a.songPitch is 100 samples/sec
	sIdx := int(currTime * 100)
	if sIdx >= 0 && sIdx < len(a.songPitch) {
		songFreq = a.songPitch[sIdx]
		n, _ := freqToNote(songFreq)
		if songFreq > 10 {
			songNoteStr = n
		}
	}

	stats := fmt.Sprintf("USER: %-4s (%.0f Hz)\nSONG: %-4s (%.0f Hz)", playNote, a.micPitch, songNoteStr, songFreq)
	ebitenutil.DebugPrintAt(screen, stats, 10, 10)

	// Draw Pitch Graph (Note Based)
	// Y Axis = MIDI Note.
	// Visible Range: C2 (36) to C6 (84) -> 48 semitones.
	// Height per semitone = screenH / 60

	offsetY := float64(sh) - 50
	scaleY := float64(sh-100) / 60.0 // Pixels per semitone
	// Base Note = 30 (F#1)
	baseMidi := 30.0

	freqToY := func(f float64) float64 {
		if f <= 0 {
			return -100
		}
		m := freqToMidi(f)
		return offsetY - (m-baseMidi)*scaleY
	}

	offsetX := float64(sw) * 0.2

	drawPitchLine := func(data []float64, stepSec float64, col color.Color, connect bool) {
		var prevX, prevY float64
		first := true

		startIdx := int((currTime - 3.0) / stepSec)
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := int((currTime + 5.0) / stepSec)
		if endIdx >= len(data) {
			endIdx = len(data) - 1
		}

		for i := startIdx; i <= endIdx; i++ {
			p := data[i]
			if p <= 5 {
				first = true
				continue
			}

			t := float64(i) * stepSec
			x := (t-currTime)*pixelsPerSec + offsetX
			y := freqToY(p)

			if y < 0 || y > float64(sh) {
				first = true
				continue
			}

			if !first && connect {
				ebitenutil.DrawLine(screen, prevX, prevY, x, y, col)
			} else {
				ebitenutil.DrawRect(screen, x, y, 3, 3, col)
			}

			prevX, prevY = x, y
			first = false
		}
	}

	// Draw Song
	drawPitchLine(a.songPitch, 0.01, color.RGBA{100, 150, 255, 255}, true)

	// Draw User Trail
	uStartI := 0
	if len(a.userPitch) > 2000 {
		uStartI = len(a.userPitch) - 2000
		if uStartI%2 != 0 {
			uStartI++
		}
	}

	var uPrevX, uPrevY float64
	uFirst := true

	for i := uStartI; i < len(a.userPitch); i += 2 {
		t := a.userPitch[i] / 1000.0
		p := a.userPitch[i+1]

		if p <= 10 {
			uFirst = true
			continue
		}

		x := (t-currTime)*pixelsPerSec + offsetX
		y := freqToY(p)

		if x < -50 {
			continue
		}
		if x > float64(sw) {
			break
		}

		col := color.RGBA{255, 200, 50, 255} // Yellow

		// Hit detection logic (Visual)
		sIdx := int(t * 100)
		if sIdx >= 0 && sIdx < len(a.songPitch) {
			ref := a.songPitch[sIdx]
			if ref > 10 && math.Abs(freqToMidi(p)-freqToMidi(ref)) < 0.7 {
				col = color.RGBA{50, 255, 50, 255} // Green
			}
		}

		if !uFirst {
			ebitenutil.DrawLine(screen, uPrevX, uPrevY, x, y, col)
		}
		uPrevX, uPrevY = x, y
		uFirst = false
	}

	// Current Marker
	if a.micPitch > 10 {
		y := freqToY(a.micPitch)
		ebitenutil.DrawRect(screen, offsetX-5, y-5, 10, 10, color.White)
	}

	// Draw "Now" Line
	ebitenutil.DrawLine(screen, offsetX, 0, offsetX, float64(sh), color.Gray{100})

	ebitenutil.DebugPrintAt(screen, "SPACE:Pause  ←→:±10s  F:Fullscreen  ESC:Exit", 10, sh-20)
}

func (a *App) Layout(w, h int) (int, int) {
	return screenW, screenH
}

func (a *App) loadAndAnalyzeSong(filename string) error {
	var audioFile string

	// For Singing/Instrumental modes, use Python separator
	if a.mode == ModeSinging || a.mode == ModeInstrumental {
		// Check if separated files already exist
		vocalsPath := "separated/vocals.mp3"
		accompPath := "separated/accompaniment.mp3"

		needsSeparation := false
		if a.mode == ModeSinging {
			if _, err := os.Stat(vocalsPath); os.IsNotExist(err) {
				needsSeparation = true
			}
		} else {
			if _, err := os.Stat(accompPath); os.IsNotExist(err) {
				needsSeparation = true
			}
		}

		if needsSeparation {
			log.Println("Running audio separation (this may take a minute)...")
			a.mu.Lock()
			a.message = "Separating audio (may take a minute)..."
			a.mu.Unlock()

			// Read venv path from config file
			pythonCmd := "python3"
			if venvBytes, err := os.ReadFile("venv_path.txt"); err == nil {
				venvPath := string(bytes.TrimSpace(venvBytes))
				if venvPath != "" {
					pythonCmd = venvPath + "/bin/python"
					log.Printf("Using venv Python: %s", pythonCmd)
				}
			}

			// Call Python separator
			cmd := exec.Command(pythonCmd, "separate.py", filename, "separated")
			output, err := cmd.CombinedOutput()
			log.Printf("Separator output: %s", string(output))

			if err != nil {
				return fmt.Errorf("separation failed: %v\nOutput: %s", err, string(output))
			}
		}

		// Use the appropriate separated file
		if a.mode == ModeSinging {
			audioFile = vocalsPath
			log.Println("Using vocals track")
		} else {
			audioFile = accompPath
			log.Println("Using accompaniment track")
		}
	} else {
		// Full Mix mode - use original file
		audioFile = filename
		log.Println("Using full mix")
	}

	// Load the audio file
	f, err := os.Open(audioFile)
	if err != nil {
		return fmt.Errorf("failed to open %s: %v", audioFile, err)
	}
	defer f.Close()

	d, err := mp3.DecodeWithoutResampling(f)
	if err != nil {
		return err
	}

	var pcmData bytes.Buffer
	if _, err := io.Copy(&pcmData, d); err != nil {
		return err
	}
	pcmBytes := pcmData.Bytes()

	// Create Player from audio
	playerRead := bytes.NewReader(pcmBytes)
	a.audioPlayer, err = audioContext.NewPlayer(playerRead)
	if err != nil {
		return err
	}

	// --- Pitch Analysis ---
	stepBytes := int(float64(sampleRate)*0.01) * 4 // 10ms chunks
	totalSamples := len(pcmBytes) / 4

	a.songPitch = make([]float64, 0, totalSamples/stepBytes)
	floatBuf := make([]float32, stepBytes/4)

	startTime := time.Now()
	log.Println("Starting pitch analysis...")

	minF, maxF := 40.0, 2000.0
	if a.mode == ModeSinging {
		minF = 100.0
		maxF = 1200.0
	}

	// Minimum energy threshold to consider as pitched content
	minEnergy := 0.001
	if a.mode == ModeSinging {
		// Stricter energy threshold for vocals (quieter parts = silence)
		minEnergy = 0.005
	}

	for i := 0; i < len(pcmBytes)-stepBytes; i += stepBytes {
		chunk := pcmBytes[i : i+stepBytes]
		for j := 0; j < len(chunk); j += 4 {
			// Read left channel
			s16 := int16(chunk[j]) | int16(chunk[j+1])<<8
			floatBuf[j/4] = float32(s16) / 32768.0
		}

		// Check energy - skip if too quiet (silence or noise)
		energy := calculateEnergy(floatBuf)
		if energy < minEnergy {
			a.songPitch = append(a.songPitch, 0) // Mark as silence
			continue
		}

		p := detectPitch(floatBuf, minF, maxF)

		// For singing mode, also filter out values outside typical vocal range
		if a.mode == ModeSinging && (p < 80 || p > 1000) {
			p = 0 // Filter out non-vocal frequencies
		}

		a.songPitch = append(a.songPitch, p)
	}

	log.Printf("Analysis done in %v", time.Since(startTime))
	return nil
}

// ------ Pitch Detection ------

func detectPitch(samples []float32, minFreq, maxFreq float64) float64 {
	n := len(samples)
	if n == 0 {
		return 0
	}

	minPeriod := int(sampleRate / maxFreq)
	maxPeriod := int(sampleRate / minFreq)
	if minPeriod < 2 {
		minPeriod = 2
	} // Nyquist
	if maxPeriod >= n {
		maxPeriod = n - 1
	}

	bestPeriod := 0
	maxVal := 0.0

	// Optimize: Downsample for speed? For now full resolution
	// Check periods
	for tau := minPeriod; tau < maxPeriod; tau++ {
		// Autocorrelation at lag tau
		// We use Difference Function (YIN step 1ish) or standard ACF?
		// Standard ACF is robust enough for clear tones.
		// Normalized ACF is better.

		cross := 0.0
		// m0 := 0.0 // energy at 0
		// mt := 0.0 // energy at tau

		limit := n - tau
		// Unroll loop slightly for speed? Go compiler handles it ok.
		for i := 0; i < limit; i += 2 { // skip 2 for speed
			cross += float64(samples[i]) * float64(samples[i+tau])
		}

		if cross > maxVal {
			maxVal = cross
			bestPeriod = tau
		}
	}

	if bestPeriod == 0 {
		return 0
	}

	// Refine parabolic interpolation?
	// ... for now pixel precision is okay

	return sampleRate / float64(bestPeriod)
}

func calculateEnergy(samples []float32) float64 {
	e := 0.0
	for _, s := range samples {
		e += float64(s * s)
	}
	return e / float64(len(samples))
}

// ------ Visualization Helpers ------

func drawButton(screen *ebiten.Image, x, y, w, h int, label string, clr color.Color) {
	ebitenutil.DrawRect(screen, float64(x), float64(y), float64(w), float64(h), clr)
	text.Draw(screen, label, basicfont.Face7x13, x+20, y+30, color.White)
}

func inRect(x, y, rx, ry, rw, rh int) bool {
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

func freqToMidi(freq float64) float64 {
	if freq <= 0 {
		return 0
	}
	return 69 + 12*math.Log2(freq/440.0)
}

func freqToNote(freq float64) (string, int) {
	if freq <= 0 {
		return "-", 0
	}
	midi := int(math.Round(freqToMidi(freq)))
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	note := notes[midi%12]
	octave := midi/12 - 1
	return note, octave
}

// ------ Stabilization ------

type Smoother struct {
	buffer []float64
	cursor int
}

func NewSmoother(size int) *Smoother {
	return &Smoother{
		buffer: make([]float64, size),
	}
}

func (s *Smoother) Smooth(val float64) float64 {
	if val <= 0 {
		// Reset smoothing on silence to avoid trailing drop
		for i := range s.buffer {
			s.buffer[i] = 0
		}
		return 0
	}
	s.buffer[s.cursor] = val
	s.cursor = (s.cursor + 1) % len(s.buffer)

	sum := 0.0
	count := 0.0
	for _, v := range s.buffer {
		if v > 0 {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

func (s *Smoother) Reset() {
	for i := range s.buffer {
		s.buffer[i] = 0
	}
}
