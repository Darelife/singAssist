package audio

import "math"

/*
PitchCandidate is one hypothesis for the fundamental frequency of a frame,
weighted by how many YIN thresholds agreed on it.

Fields:
  - Freq: candidate frequency in Hz
  - Prob: fraction of threshold sweep that selected this period, in (0, 1]
*/
type PitchCandidate struct {
	Freq float64
	Prob float64
}

/*
pyinThresholdSweep is the fixed set of absolute thresholds swept across the
CMNDF curve to generate weighted candidates, standing in for pYIN's
Beta-distributed prior over the YIN threshold parameter (Mauch & Dixon,
2014). A lower threshold is stricter and more often locks onto the true
fundamental; a higher threshold is more permissive and more often catches
a subharmonic. Aggregating across the sweep turns the single greedy pick
of classic YIN into a probability distribution over candidates.
*/
var pyinThresholdSweep = buildThresholdSweep(0.05, 0.20, 16)

/*
buildThresholdSweep generates `count` evenly spaced thresholds in [lo, hi].

Called by:
  - package init (pyinThresholdSweep)

Output:
  - []float64: ascending thresholds, len == count
*/
func buildThresholdSweep(lo, hi float64, count int) []float64 {
	out := make([]float64, count)
	if count == 1 {
		out[0] = lo
		return out
	}
	step := (hi - lo) / float64(count-1)
	for i := range out {
		out[i] = lo + float64(i)*step
	}
	return out
}

/*
yinDiffFFT computes the YIN difference function d(tau) for tau in
[0, maxLag) using an FFT-accelerated linear autocorrelation instead of the
O(n*maxLag) direct double sum, so a full threshold sweep stays cheap
enough for real-time mic frames.

Input:
  - samples: []float32 - windowed audio samples
  - maxLag: int - largest lag (in samples) to evaluate

Called by:
  - analyzePitch (song) and MicHandler.DetectPitchFromMic (live)

Logic:
 1. Compute the linear autocorrelation r(tau) of samples via
    Wiener-Khinchin: zero-pad to a power of two, FFT, take |X|^2 (the
    power spectrum), inverse FFT; the real part is r(tau).
 2. Combine r(tau) with cumulative sums of squared samples to get the
    classic difference-function identity:
    d(tau) = sum_{i<n-tau} x[i]^2 + sum_{i<n-tau} x[i+tau]^2 - 2*r(tau)

Output:
  - []float64: d(tau) for tau = 0..maxLag-1 (d[0] is always 0)
*/
func yinDiffFFT(samples []float32, maxLag int) []float64 {
	n := len(samples)
	if maxLag >= n {
		maxLag = n - 1
	}
	if maxLag < 1 {
		return make([]float64, 1)
	}

	size := nextPow2(2 * n)
	buf := make([]complex128, size)
	for i, s := range samples {
		buf[i] = complex(float64(s), 0)
	}

	fft(buf, false)
	for i := range buf {
		p := buf[i] * complex(real(buf[i]), -imag(buf[i]))
		buf[i] = p
	}
	fft(buf, true)

	prefixSq := make([]float64, n+1)
	for i, s := range samples {
		prefixSq[i+1] = prefixSq[i] + float64(s)*float64(s)
	}

	diff := make([]float64, maxLag)
	for tau := 1; tau < maxLag; tau++ {
		r := real(buf[tau])
		e1 := prefixSq[n-tau]
		e2 := prefixSq[n] - prefixSq[tau]
		d := e1 + e2 - 2*r
		if d < 0 {
			d = 0
		}
		diff[tau] = d
	}
	return diff
}

/*
cmndfFromDiff normalizes a YIN difference function into the cumulative
mean normalized difference function used for thresholding.

Input:
  - diff: []float64 - output of yinDiffFFT

Called by:
  - analyzePitch, MicHandler.DetectPitchFromMic

Output:
  - []float64: CMNDF values, same length as diff (index 0 unused)
*/
func cmndfFromDiff(diff []float64) []float64 {
	cmndf := make([]float64, len(diff))
	running := 0.0
	for tau := 1; tau < len(diff); tau++ {
		running += diff[tau]
		if running == 0 {
			cmndf[tau] = 1.0
		} else {
			cmndf[tau] = diff[tau] * float64(tau) / running
		}
	}
	return cmndf
}

/*
pyinCandidates sweeps pyinThresholdSweep over a CMNDF curve and aggregates
the resulting period picks into weighted pitch candidates.

Input:
  - cmndf: []float64 - CMNDF curve from cmndfFromDiff
  - minPeriod, maxPeriod: int - valid period range (samples)
  - sampleRate: int - audio sample rate, for period -> frequency

Called by:
  - analyzePitch, MicHandler.DetectPitchFromMic

Logic:
 1. For each threshold, scan for the first CMNDF dip below it (classic
    YIN's absolute-threshold rule), then ride the slope down to the
    local minimum, exactly as single-threshold YIN does.
 2. Tally how many thresholds landed on each period.
 3. Convert each distinct period into a candidate frequency with
    probability = (# thresholds selecting it) / (# thresholds swept).
 4. If no threshold found a dip (very weak periodicity), fall back to
    the single global CMNDF minimum with probability 1, matching plain
    YIN's fallback behavior.

Output:
  - []PitchCandidate: weighted candidates, empty if no periodicity found
*/
func pyinCandidates(cmndf []float64, minPeriod, maxPeriod, sampleRate int) []PitchCandidate {
	if maxPeriod > len(cmndf) {
		maxPeriod = len(cmndf)
	}
	if minPeriod < 1 {
		minPeriod = 1
	}
	if minPeriod >= maxPeriod {
		return nil
	}

	counts := make(map[int]int)
	for _, threshold := range pyinThresholdSweep {
		period := 0
		for tau := minPeriod; tau < maxPeriod; tau++ {
			if cmndf[tau] < threshold {
				period = tau
				for t := tau + 1; t < maxPeriod; t++ {
					if cmndf[t] < cmndf[period] {
						period = t
					} else {
						break
					}
				}
				break
			}
		}
		if period > 0 {
			counts[period]++
		}
	}

	if len(counts) == 0 {
		best := 0
		minVal := math.MaxFloat64
		for tau := minPeriod; tau < maxPeriod; tau++ {
			if cmndf[tau] < minVal {
				minVal = cmndf[tau]
				best = tau
			}
		}
		if best == 0 {
			return nil
		}
		return []PitchCandidate{{Freq: float64(sampleRate) / float64(best), Prob: 1.0}}
	}

	total := float64(len(pyinThresholdSweep))
	cands := make([]PitchCandidate, 0, len(counts))
	for period, c := range counts {
		cands = append(cands, PitchCandidate{
			Freq: float64(sampleRate) / float64(period),
			Prob: float64(c) / total,
		})
	}
	return cands
}

/*
freqToMidiLocal converts a frequency to a continuous MIDI note number.
Duplicated from the ui package (rather than imported) to keep the audio
package free of a dependency on the rendering layer.

Input:
  - f: float64 - frequency in Hz

Called by:
  - OnlinePitchTracker.Step, viterbiDecodeSegment

Output:
  - float64: continuous MIDI note number, 0 if f <= 0
*/
func freqToMidiLocal(f float64) float64 {
	if f <= 0 {
		return 0
	}
	return 69 + 12*math.Log2(f/440.0)
}

/*
pathState is one candidate's best cumulative Viterbi path cost, used by
OnlinePitchTracker for greedy real-time decoding.
*/
type pathState struct {
	freq float64
	cost float64
}

/*
OnlinePitchTracker performs forward-only (no backtrace) Viterbi decoding
across mic frames in real time.

Fields:
  - states: best cumulative-cost path ending at each candidate from the
    previous frame
*/
type OnlinePitchTracker struct {
	states []pathState
}

/*
NewOnlinePitchTracker creates a tracker with no prior path history.

Output:
  - *OnlinePitchTracker: ready for Step calls
*/
func NewOnlinePitchTracker() *OnlinePitchTracker {
	return &OnlinePitchTracker{}
}

/*
Reset clears path history, used on silence so an old pitch doesn't bias
the next voiced phrase's decode.

Called by:
  - MicHandler.DetectPitchFromMic when the frame is below the noise gate
*/
func (t *OnlinePitchTracker) Reset() {
	t.states = nil
}

/*
Step decodes one frame's pitch from its weighted candidates, suppressing
octave jumps and jitter by picking whichever candidate is cheapest to
reach from the previous frame's best path rather than the single highest-
probability dip.

Input:
  - cands: []PitchCandidate - this frame's weighted candidates

Called by:
  - MicHandler.DetectPitchFromMic

Logic:
 1. For each candidate, emission cost = -log(probability).
 2. Transition cost from a previous-frame candidate = distance in
    semitones between the two frequencies. An octave error is ~12
    semitones away, so unless the correct-octave candidate is entirely
    absent, it wins on total cost even when its raw probability is
    lower than the erroneous octave-doubled/halved dip.
 3. Each candidate's total cost = emission + min over previous states of
    (previous cost + transition).
 4. Output the frequency of the current frame's lowest-cost candidate.
 5. If there is no previous state (first voiced frame after silence),
    transition cost is 0 for all candidates.

Output:
  - float64: decoded pitch in Hz, 0 if cands is empty
*/
func (t *OnlinePitchTracker) Step(cands []PitchCandidate) float64 {
	if len(cands) == 0 {
		t.states = nil
		return 0
	}

	next := make([]pathState, len(cands))
	for i, c := range cands {
		emission := -math.Log(c.Prob + 1e-6)

		best := 0.0
		if len(t.states) > 0 {
			best = math.MaxFloat64
			targetMidi := freqToMidiLocal(c.Freq)
			for _, s := range t.states {
				trans := math.Abs(targetMidi - freqToMidiLocal(s.freq))
				total := s.cost + trans
				if total < best {
					best = total
				}
			}
		}
		next[i] = pathState{freq: c.Freq, cost: best + emission}
	}

	t.states = next

	bestIdx := 0
	for i := range next {
		if next[i].cost < next[bestIdx].cost {
			bestIdx = i
		}
	}
	return next[bestIdx].freq
}

/*
viterbiDecodeSegment runs full (offline) Viterbi decoding with backtrace
over a contiguous run of voiced frames, used for song analysis where the
whole segment is already buffered.

Input:
  - candsPerFrame: [][]PitchCandidate - one candidate list per frame, in
    time order, with no silent frames inside the segment

Called by:
  - analyzePitch, once per voiced segment (silence resets the segment)

Logic:
 1. Standard Viterbi: dp[i][j] = cost of the best path ending at
    candidate j of frame i.
 2. Transition cost = semitone distance, same as OnlinePitchTracker, but
    here the whole segment is visible so the true global-minimum path
    can be recovered by backtracking instead of greedily committing per
    frame.
 3. Backtrack from the lowest-cost final-frame candidate to recover the
    full smoothed pitch sequence.

Output:
  - []float64: one decoded frequency per input frame
*/
func viterbiDecodeSegment(candsPerFrame [][]PitchCandidate) []float64 {
	n := len(candsPerFrame)
	if n == 0 {
		return nil
	}

	type node struct {
		cost float64
		prev int
	}

	dp := make([][]node, n)
	for i, cands := range candsPerFrame {
		dp[i] = make([]node, len(cands))
		for j, c := range cands {
			emission := -math.Log(c.Prob + 1e-6)
			if i == 0 {
				dp[i][j] = node{cost: emission, prev: -1}
				continue
			}

			targetMidi := freqToMidiLocal(c.Freq)
			best := math.MaxFloat64
			bestPrev := 0
			for k, pc := range candsPerFrame[i-1] {
				trans := math.Abs(targetMidi - freqToMidiLocal(pc.Freq))
				total := dp[i-1][k].cost + trans
				if total < best {
					best = total
					bestPrev = k
				}
			}
			dp[i][j] = node{cost: best + emission, prev: bestPrev}
		}
	}

	bestJ := 0
	for j := range dp[n-1] {
		if dp[n-1][j].cost < dp[n-1][bestJ].cost {
			bestJ = j
		}
	}

	out := make([]float64, n)
	j := bestJ
	for i := n - 1; i >= 0; i-- {
		out[i] = candsPerFrame[i][j].Freq
		if dp[i][j].prev < 0 {
			break
		}
		j = dp[i][j].prev
	}
	return out
}
