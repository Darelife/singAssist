package audio

import (
	"math"
	"testing"
)

func sineWave(freq float64, n int, sampleRate int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.8 * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
	}
	return out
}

func TestYinDiffFFTRecoversKnownPitch(t *testing.T) {
	const sampleRate = 44100
	const freq = 220.0
	samples := sineWave(freq, 2048, sampleRate)

	sr := float64(sampleRate)
	minPeriod := int(sr / 1100.0)
	maxPeriod := int(sr / 85.0)

	diff := yinDiffFFT(samples, maxPeriod)
	cmndf := cmndfFromDiff(diff)
	cands := pyinCandidates(cmndf, minPeriod, maxPeriod, sampleRate)

	if len(cands) == 0 {
		t.Fatal("expected at least one pitch candidate for a clean sine wave")
	}

	best := cands[0]
	for _, c := range cands[1:] {
		if c.Prob > best.Prob {
			best = c
		}
	}

	if math.Abs(best.Freq-freq) > 2.0 {
		t.Fatalf("expected best candidate near %.1fHz, got %.1fHz (prob %.2f)", freq, best.Freq, best.Prob)
	}
}

func TestOnlinePitchTrackerSuppressesOctaveJump(t *testing.T) {
	tracker := NewOnlinePitchTracker()

	// Five stable frames at 220Hz to establish a path.
	for i := 0; i < 5; i++ {
		f := tracker.Step([]PitchCandidate{{Freq: 220, Prob: 0.9}, {Freq: 440, Prob: 0.3}})
		if math.Abs(f-220) > 1 {
			t.Fatalf("frame %d: expected ~220Hz, got %.1f", i, f)
		}
	}

	// A single frame where the octave-doubled candidate briefly has
	// higher raw probability than the true pitch. Viterbi's transition
	// cost should keep the tracker on 220Hz because jumping to 440Hz
	// costs 12 semitones of transition penalty.
	f := tracker.Step([]PitchCandidate{{Freq: 220, Prob: 0.4}, {Freq: 440, Prob: 0.6}})
	if math.Abs(f-220) > 1 {
		t.Fatalf("expected octave jump to be suppressed, stayed near 220Hz, got %.1f", f)
	}
}

func TestFFTMatchesDirectAutocorrelation(t *testing.T) {
	const sampleRate = 44100
	samples := sineWave(330.0, 512, sampleRate)
	maxLag := 200

	got := yinDiffFFT(samples, maxLag)

	n := len(samples)
	for tau := 1; tau < maxLag; tau++ {
		want := 0.0
		limit := n - tau
		for i := 0; i < limit; i++ {
			d := float64(samples[i]) - float64(samples[i+tau])
			want += d * d
		}
		if math.Abs(got[tau]-want) > 1e-6*math.Max(1, want) {
			t.Fatalf("tau=%d: fft diff=%.6f direct diff=%.6f", tau, got[tau], want)
		}
	}
}
