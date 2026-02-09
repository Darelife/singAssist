package audio

import (
	"fmt"
	"time"

	"singAssist/internal/config"

	"github.com/gordonklaus/portaudio"
)

/*
Smoother provides moving average smoothing for pitch values to reduce jitter.

Fields:
  - buffer: Circular buffer of recent pitch values
  - cursor: Current write position in buffer
*/
type Smoother struct {
	buffer []float64
	cursor int
}

/*
NewSmoother creates a new pitch smoother with given window size.

Input:
  - size: int - Number of samples to average (e.g., 5)

Called by:
  - NewMicHandler when initializing microphone

Task:
  - Initialize circular buffer for smoothing

Logic:
 1. Allocate buffer of specified size
 2. Initialize cursor to 0

Output:
  - *Smoother: Ready-to-use smoother instance
*/
func NewSmoother(size int) *Smoother {
	return &Smoother{
		buffer: make([]float64, size),
	}
}

/*
Smooth applies moving average smoothing to a pitch value.

Input:
  - val: float64 - Raw pitch value in Hz (0 or negative = silence)

Called by:
  - MicHandler.DetectPitchFromMic after raw pitch detection

Task:
  - Smooth out pitch jitter while preserving silence gaps

Logic:
 1. If input is <= 0: clear buffer entirely, return 0 (prevents trailing)
 2. Store value in circular buffer, advance cursor
 3. Calculate mean of all non-zero values in buffer
 4. Return smoothed value or 0 if no valid samples

Output:
  - float64: Smoothed pitch value in Hz
*/
func (s *Smoother) Smooth(val float64) float64 {
	if val <= 0 {
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

/*
Reset clears all values in the smoother buffer.

Input:
  - None

Called by:
  - App.startGame when beginning new session

Task:
  - Reset smoother state for fresh start

Logic:
 1. Set all buffer values to 0

Output:
  - None
*/
func (s *Smoother) Reset() {
	for i := range s.buffer {
		s.buffer[i] = 0
	}
}

/*
MicHandler manages microphone input capture and pitch detection.

Fields:
  - Stream: PortAudio stream handle
  - Buffer: Audio sample buffer (float32, mono)
  - Done: Channel to signal goroutine shutdown
  - Smoother: Pitch smoothing instance
  - Pitch: Current detected pitch (updated by DetectPitchFromMic)
  - Threshold: Noise gate threshold (set by Calibrate)
*/
type MicHandler struct {
	Stream    *portaudio.Stream
	Buffer    []float32
	Done      chan struct{}
	Smoother  *Smoother
	Pitch     float64
	Threshold float64
}

/*
NewMicHandler creates a new microphone handler with default settings.

Input:
  - None

Called by:
  - App.startGame when starting new session

Task:
  - Initialize microphone handler with buffer and smoother

Logic:
 1. Allocate buffer of config.BufferSize samples
 2. Create smoother with window of 5

Output:
  - *MicHandler: Handler ready for Start() call
*/
func NewMicHandler() *MicHandler {
	return &MicHandler{
		Buffer:   make([]float32, config.BufferSize),
		Smoother: NewSmoother(5),
	}
}

/*
Start initializes PortAudio stream and begins microphone capture.

Input:
  - None

Called by:
  - App.startGame after cleanup

Task:
  - Open default microphone stream
  - Start audio capture with retry logic

Logic:
 1. Try up to 3 times with exponential backoff
 2. Open PortAudio default stream (1 input channel, mono, SampleRate Hz)
 3. Start stream capture
 4. Initialize Done channel for shutdown signaling

Output:
  - error: nil on success, PortAudio error after all retries
*/
func (m *MicHandler) Start() error {
	var err error
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}

		m.Stream, err = portaudio.OpenDefaultStream(1, 0, config.SampleRate, len(m.Buffer), m.Buffer)
		if err != nil {
			continue
		}

		if err = m.Stream.Start(); err != nil {
			m.Stream.Close()
			m.Stream = nil
			continue
		}

		m.Done = make(chan struct{})
		return nil
	}

	return fmt.Errorf("failed to start microphone after %d attempts: %v", maxRetries, err)
}

/*
Stop safely shuts down microphone capture.

Input:
  - None

Called by:
  - App.cleanup when exiting to menu or closing app

Task:
  - Signal goroutine to stop
  - Close PortAudio stream

Logic:
 1. Close Done channel (signals IsDone to return true)
 2. Wait 50ms for goroutine to notice
 3. Stop and close PortAudio stream
 4. Nil out references

Output:
  - None
*/
func (m *MicHandler) Stop() {
	if m.Done != nil {
		close(m.Done)
		time.Sleep(50 * time.Millisecond)
	}
	if m.Stream != nil {
		m.Stream.Stop()
		m.Stream.Close()
		m.Stream = nil
	}
	m.Done = nil
}

/*
Read fills the buffer with samples from microphone.

Input:
  - None (reads into m.Buffer)

Called by:
  - App.micLoop on each iteration
  - MicHandler.Calibrate during noise calibration

Task:
  - Block until buffer is filled with audio samples

Logic:
 1. If stream is nil, return nil (no-op)
 2. Call PortAudio Read to fill buffer

Output:
  - error: nil on success, PortAudio error on failure
*/
func (m *MicHandler) Read() error {
	if m.Stream == nil {
		return nil
	}
	return m.Stream.Read()
}

/*
IsDone checks if the handler should stop processing.

Input:
  - None

Called by:
  - App.micLoop to check for shutdown signal

Task:
  - Non-blocking check for Done channel closure

Logic:
 1. Use select with default case for non-blocking check
 2. Return true if Done channel is closed/readable

Output:
  - bool: true if shutdown requested, false otherwise
*/
func (m *MicHandler) IsDone() bool {
	select {
	case <-m.Done:
		return true
	default:
		return false
	}
}

/*
Calibrate measures background noise level to set gate threshold.

Input:
  - duration: time.Duration - How long to measure (e.g., 2 seconds)

Called by:
  - App.calibrateAndPlay at start of session

Task:
  - Measure ambient noise to set noise gate threshold

Logic:
 1. Record energy samples for specified duration
 2. Find maximum energy observed
 3. Set threshold to 1.5x max (safety margin)

Output:
  - float64: Calculated noise threshold
*/
func (m *MicHandler) Calibrate(duration time.Duration) float64 {
	var energies []float64
	endTime := time.Now().Add(duration)

	for time.Now().Before(endTime) {
		if err := m.Read(); err != nil {
			break
		}
		energies = append(energies, CalculateEnergy(m.Buffer))
	}

	maxE := 0.0
	for _, e := range energies {
		if e > maxE {
			maxE = e
		}
	}
	m.Threshold = maxE * 1.5
	return m.Threshold
}

/*
DetectPitchFromMic processes current buffer and detects pitch.

Input:
  - mode: Mode - Current playback mode (affects frequency range)

Called by:
  - App.micLoop on each iteration during playback

Task:
  - Gate noise below threshold
  - Detect and smooth pitch from microphone buffer

Logic:
 1. Calculate energy of current buffer
 2. If below threshold: set Pitch to 0, return 0
 3. Set frequency range based on mode (narrower for singing)
 4. Run DetectPitch on buffer
 5. Apply smoothing
 6. Store in m.Pitch and return

Output:
  - float64: Detected pitch in Hz (0 if below threshold)
*/
func (m *MicHandler) DetectPitchFromMic(mode Mode) float64 {
	energy := CalculateEnergy(m.Buffer)
	if energy < m.Threshold {
		m.Pitch = 0
		return 0
	}

	minF, maxF := 40.0, 2000.0
	if mode == ModeSinging {
		minF, maxF = 85.0, 1100.0
	}

	rawPitch := DetectPitch(m.Buffer, minF, maxF)
	m.Pitch = m.Smoother.Smooth(rawPitch)
	return m.Pitch
}
