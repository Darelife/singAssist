package main

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"sync"

	"github.com/gordonklaus/portaudio"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	sampleRate = 44100
	bufferSize = 2048

	screenW  = 900
	screenH  = 500
	historyN = 300
)

var (
	freqHistory []float64
	mu          sync.Mutex
)

type App struct{}

func (a *App) Update() error {
	return nil
}

func (a *App) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)

	if len(freqHistory) > 0 {
		current := freqHistory[len(freqHistory)-1]
		note, octave := freqToNote(current)

		text := fmt.Sprintf("%.1f Hz   %s%d", current, note, octave)
		ebitenutil.DebugPrintAt(screen, text, 10, 10)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(freqHistory) < 2 {
		return
	}

	current := freqHistory[len(freqHistory)-1]
	centerY := float64(screenH) / 2
	scale := 0.8 // pixels per Hz (tune this)

	for i := 1; i < len(freqHistory); i++ {
		x1 := float64(i-1) * screenW / historyN
		x2 := float64(i) * screenW / historyN

		y1 := centerY - (freqHistory[i-1]-current)*scale
		y2 := centerY - (freqHistory[i]-current)*scale

		col := color.RGBA{0, 200, 255, 255} // cyan
		if freqHistory[i] > current {
			col = color.RGBA{255, 80, 80, 255} // red for higher
		}

		ebitenutil.DrawLine(screen, x1, y1, x2, y2, col)
	}
}

func freqToNote(freq float64) (string, int) {
	if freq <= 0 {
		return "", 0
	}

	midi := int(math.Round(69 + 12*math.Log2(freq/440.0)))
	noteNames := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

	note := noteNames[midi%12]
	octave := midi/12 - 1

	return note, octave
}

func (a *App) Layout(_, _ int) (int, int) {
	return screenW, screenH
}

func main() {
	freqHistory = make([]float64, 0, historyN)

	go audioLoop()

	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("SingAssist – Live Pitch Graph")

	if err := ebiten.RunGame(&App{}); err != nil {
		log.Fatal(err)
	}
}

func audioLoop() {
	portaudio.Initialize()
	defer portaudio.Terminate()

	in := make([]float32, bufferSize)

	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(in), in)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	stream.Start()
	defer stream.Stop()

	fft := fourier.NewFFT(bufferSize)

	for {
		stream.Read()
		f := detectFrequency(in, fft)

		if f > 50 && f < 2000 {
			mu.Lock()
			if len(freqHistory) >= historyN {
				freqHistory = freqHistory[1:]
			}
			freqHistory = append(freqHistory, smooth(f))
			mu.Unlock()
		}
	}
}

var last float64

func smooth(v float64) float64 {
	alpha := 0.03
	last = alpha*v + (1-alpha)*last
	return last
}

func detectFrequency(samples []float32, fft *fourier.FFT) float64 {
	data := make([]float64, len(samples))
	for i, v := range samples {
		data[i] = float64(v)
	}

	coeffs := fft.Coefficients(nil, data)

	maxMag := 0.0
	maxIdx := 0
	for i := 1; i < len(coeffs)/2; i++ {
		mag := math.Hypot(real(coeffs[i]), imag(coeffs[i]))
		if mag > maxMag {
			maxMag = mag
			maxIdx = i
		}
	}

	return float64(maxIdx) * sampleRate / float64(len(samples))
}
