package ui

import (
	"fmt"
	"image/color"
	"math"

	"singAssist/internal/config"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
)

var (
	bigFont   font.Face
	smallFont font.Face
)

func init() {
	tt, err := opentype.Parse(gomonobold.TTF)
	if err == nil {
		bigFont, _ = opentype.NewFace(tt, &opentype.FaceOptions{
			Size:    48,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		smallFont, _ = opentype.NewFace(tt, &opentype.FaceOptions{
			Size:    14,
			DPI:     72,
			Hinting: font.HintingFull,
		})
	}
}

/*
DrawButton renders a colored rectangular button with centered text label.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - x, y: int - Top-left corner position
  - w, h: int - Width and height in pixels
  - label: string - Button text
  - clr: color.Color - Button fill color

Called by:
  - DrawStartScreen for menu buttons

Task:
  - Draw filled rectangle with text overlay

Logic:
 1. Draw filled rectangle at position with given color
 2. Draw white text label offset 20px right and 30px down from top-left

Output:
  - None (draws to screen)
*/
func DrawButton(screen *ebiten.Image, x, y, w, h int, label string, clr color.Color) {
	ebitenutil.DrawRect(screen, float64(x), float64(y), float64(w), float64(h), clr)
	text.Draw(screen, label, basicfont.Face7x13, x+20, y+30, color.White)
}

/*
InRect tests if a point is inside a rectangle.

Input:
  - x, y: int - Point coordinates to test
  - rx, ry: int - Rectangle top-left corner
  - rw, rh: int - Rectangle width and height

Called by:
  - App.handleStartScreenInput for button click detection

Task:
  - Simple bounds check for mouse interaction

Logic:
 1. Check x is within [rx, rx+rw)
 2. Check y is within [ry, ry+rh)

Output:
  - bool: true if point is inside rectangle
*/
func InRect(x, y, rx, ry, rw, rh int) bool {
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

/*
FreqToMidi converts frequency in Hz to MIDI note number.

Input:
  - freq: float64 - Frequency in Hz

Called by:
  - FreqToNote for note name conversion
  - PitchVisualizer.FreqToY for Y coordinate calculation
  - PitchVisualizer.DrawUserPitch for hit detection

Task:
  - Convert frequency to continuous MIDI scale for visualization

Logic:
 1. If freq <= 0: return 0 (invalid)
 2. Apply formula: 69 + 12 * log2(freq / 440)
 3. MIDI 69 = A4 = 440Hz

Output:
  - float64: Continuous MIDI note number (can be fractional)
*/
func FreqToMidi(freq float64) float64 {
	if freq <= 0 {
		return 0
	}
	return 69 + 12*math.Log2(freq/440.0)
}

/*
FreqToNote converts frequency to musical note name and octave.

Input:
  - freq: float64 - Frequency in Hz

Called by:
  - App.drawPlayingMode for displaying current notes
  - App.drawNoAudioMode for displaying user pitch

Task:
  - Convert frequency to human-readable note name

Logic:
 1. If freq <= 0: return "-", 0
 2. Convert to MIDI, round to nearest integer
 3. Note name = notes[midi % 12]
 4. Octave = midi / 12 - 1

Output:
  - string: Note name (e.g., "C#", "A")
  - int: Octave number (e.g., 4 for A4)
*/
func FreqToNote(freq float64) (string, int) {
	if freq <= 0 {
		return "-", 0
	}
	midi := int(math.Round(FreqToMidi(freq)))
	notes := []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	note := notes[midi%12]
	octave := midi/12 - 1
	return note, octave
}

/*
DrawStartScreen renders the main menu with mode selection buttons.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sw, sh: int - Screen width and height
  - songName: string - Current song name for title

Called by:
  - App.Draw when state is StateStartScreen

Task:
  - Draw title and four mode selection buttons

Logic:
 1. Fill screen with black
 2. Draw title (with song name if available)
 3. Draw four buttons: Vocals, Instrumental, Full Mix, No Audio
 4. Buttons are centered horizontally, stacked vertically

Output:
  - None (draws to screen)
*/
func DrawStartScreen(screen *ebiten.Image, sw, sh int, songName string) {
	screen.Fill(color.Black)

	title := "SingAssist"
	if songName != "" {
		title = "SingAssist - " + songName
	}
	text.Draw(screen, title, basicfont.Face7x13, sw/2-40, sh/2-160, color.White)

	DrawButton(screen, sw/2-100, sh/2-120, 200, 50, "Vocals Only", color.RGBA{0, 200, 100, 255})
	DrawButton(screen, sw/2-100, sh/2-60, 200, 50, "Instrumental", color.RGBA{100, 100, 200, 255})
	DrawButton(screen, sw/2-100, sh/2, 200, 50, "Full Mix", color.RGBA{200, 100, 100, 255})
	DrawButton(screen, sw/2-100, sh/2+60, 200, 50, "No Audio", color.RGBA{150, 150, 50, 255})
}

/*
DrawCalibrating renders the calibration screen with instructions.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sw, sh: int - Screen width and height

Called by:
  - App.Draw when state is StateCalibrating

Task:
  - Display calibration message

Logic:
 1. Fill screen with black
 2. Draw centered message asking for silence

Output:
  - None (draws to screen)
*/
func DrawCalibrating(screen *ebiten.Image, sw, sh int) {
	screen.Fill(color.Black)
	msg := "Calibrating Silence...\nPlease stay quiet."
	text.Draw(screen, msg, basicfont.Face7x13, sw/2-60, sh/2, color.White)
}

/*
DrawMessage renders a debug/status message at top-left.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - msg: string - Message to display

Called by:
  - App.Draw for error/loading messages

Task:
  - Display status text

Logic:
 1. Use ebitenutil.DebugPrint for simple text at (0,0)

Output:
  - None (draws to screen)
*/
func DrawMessage(screen *ebiten.Image, msg string) {
	ebitenutil.DebugPrint(screen, msg)
}

/*
NoteDisplay contains info for rendering a prominent note indicator.
*/
type NoteDisplay struct {
	Note      string
	Octave    int
	Freq      float64
	IsMatched bool
}

/*
DrawNoteHUD renders the production-grade note display with large notes on left and right.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sw: int - Screen width
  - songNote: NoteDisplay - Current song note info
  - userNote: NoteDisplay - Current user note info

Called by:
  - App.drawPlayingMode

Task:
  - Display prominent note indicators: song on left, user on right

Logic:
 1. Draw semi-transparent background panels
 2. Draw large note text (e.g., "C#4") in gray
 3. Draw smaller frequency below
 4. If notes match, show green highlight on user side

Output:
  - None (draws to screen)
*/
func DrawNoteHUD(screen *ebiten.Image, sw int, songNote, userNote NoteDisplay) {
	gray := color.RGBA{140, 140, 140, 255}
	dimGray := color.RGBA{80, 80, 80, 255}
	green := color.RGBA{80, 220, 80, 255}
	panelBg := color.RGBA{20, 20, 25, 200}

	vector.DrawFilledRect(screen, 15, 15, 130, 80, panelBg, false)
	vector.DrawFilledRect(screen, float32(sw-145), 15, 130, 80, panelBg, false)

	if bigFont != nil {
		songNoteText := songNote.Note
		if songNote.Note != "-" && songNote.Octave > 0 {
			songNoteText = fmt.Sprintf("%s%d", songNote.Note, songNote.Octave)
		}
		text.Draw(screen, songNoteText, bigFont, 25, 65, gray)

		userNoteText := userNote.Note
		if userNote.Note != "-" && userNote.Octave > 0 {
			userNoteText = fmt.Sprintf("%s%d", userNote.Note, userNote.Octave)
		}
		noteColor := gray
		if userNote.IsMatched {
			noteColor = green
		}
		text.Draw(screen, userNoteText, bigFont, sw-135, 65, noteColor)
	}

	if smallFont != nil {
		songFreqText := "---"
		if songNote.Freq > 10 {
			songFreqText = fmt.Sprintf("%.0f Hz", songNote.Freq)
		}
		text.Draw(screen, songFreqText, smallFont, 25, 85, dimGray)

		userFreqText := "---"
		if userNote.Freq > 10 {
			userFreqText = fmt.Sprintf("%.0f Hz", userNote.Freq)
		}
		text.Draw(screen, userFreqText, smallFont, sw-135, 85, dimGray)
	}

	if smallFont != nil {
		text.Draw(screen, "SONG", smallFont, 25, 28, dimGray)
		text.Draw(screen, "YOU", smallFont, sw-65, 28, dimGray)
	}
}

/*
PitchVisualizer handles coordinate transformations and pitch graph rendering.

Fields:
  - OffsetY: Y position of lowest displayed note
  - ScaleY: Pixels per semitone
  - BaseMidi: MIDI note number at bottom of display
  - OffsetX: X position of "now" line
*/
type PitchVisualizer struct {
	OffsetY  float64
	ScaleY   float64
	BaseMidi float64
	OffsetX  float64
}

/*
NewPitchVisualizer creates a visualizer configured for given screen size.

Input:
  - sw, sh: int - Screen width and height

Called by:
  - App.drawPlayingMode multiple times per frame
  - App.drawNoAudioMode for pitch marker

Task:
  - Calculate layout parameters for pitch visualization

Logic:
 1. OffsetY = bottom margin (sh - 50)
 2. ScaleY = available height / 60 semitones
 3. BaseMidi = 30 (approximately F#1, low bass)
 4. OffsetX = 20% from left (position of "now" line)

Output:
  - *PitchVisualizer: Configured for current screen size
*/
func NewPitchVisualizer(sw, sh int) *PitchVisualizer {
	return &PitchVisualizer{
		OffsetY:  float64(sh) - 50,
		ScaleY:   float64(sh-100) / 60.0,
		BaseMidi: 30.0,
		OffsetX:  float64(sw) * 0.2,
	}
}

/*
FreqToY converts frequency to Y screen coordinate.

Input:
  - f: float64 - Frequency in Hz

Called by:
  - DrawSongPitch, DrawUserPitch, DrawCurrentPitch

Task:
  - Map frequency to vertical position (higher freq = higher on screen)

Logic:
 1. If f <= 0: return off-screen (-100)
 2. Convert to MIDI note
 3. Calculate Y = OffsetY - (midi - BaseMidi) * ScaleY

Output:
  - float64: Y coordinate (lower = higher pitch)
*/
func (v *PitchVisualizer) FreqToY(f float64) float64 {
	if f <= 0 {
		return -100
	}
	m := FreqToMidi(f)
	return v.OffsetY - (m-v.BaseMidi)*v.ScaleY
}

/*
DrawSongPitch renders the song's pitch contour as a blue line.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - data: []float64 - Pitch values at 10ms intervals
  - currTime: float64 - Current playback time in seconds
  - sw, sh: int - Screen dimensions

Called by:
  - App.drawPlayingMode

Task:
  - Draw song pitch within visible time window (-3s to +5s from now)

Logic:
 1. Calculate visible index range from currTime ± window
 2. For each pitch sample in range:
    a. Skip if pitch <= 5 (silence), break line continuity
    b. Calculate X from time offset, Y from FreqToY
    c. Draw line segment to previous point, or 3x3 rect if first point
 3. Track previous point for line continuity

Output:
  - None (draws to screen)
*/
func (v *PitchVisualizer) DrawSongPitch(screen *ebiten.Image, data []float64, currTime float64, sw, sh int) {
	col := color.RGBA{100, 150, 255, 255}
	stepSec := 0.01

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
		x := (t-currTime)*config.PixelsPerSec + v.OffsetX
		y := v.FreqToY(p)

		if y < 0 || y > float64(sh) {
			first = true
			continue
		}

		if !first {
			ebitenutil.DrawLine(screen, prevX, prevY, x, y, col)
		} else {
			ebitenutil.DrawRect(screen, x, y, 3, 3, col)
		}

		prevX, prevY = x, y
		first = false
	}
}

/*
DrawUserPitch renders the user's recorded pitch trail with hit detection.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - userPitch: []float64 - Pairs of [timeMs, pitch, timeMs, pitch, ...]
  - songPitch: []float64 - Song pitch data for hit comparison
  - currTime: float64 - Current playback time in seconds
  - sw, sh: int - Screen dimensions

Called by:
  - App.drawPlayingMode

Task:
  - Draw user pitch trail, colored by accuracy (green=hit, yellow=miss)

Logic:
 1. Apply latency compensation to time values
 2. Iterate userPitch in pairs (time, pitch)
 3. Skip silence (pitch <= 10)
 4. Calculate X from time, Y from FreqToY
 5. Skip if off-screen left (<-50), break if off-screen right
 6. Compare pitch to song pitch at same time:
    - Green if within 0.7 semitones
    - Yellow otherwise
 7. Draw line to previous point

Output:
  - None (draws to screen)
*/
func (v *PitchVisualizer) DrawUserPitch(screen *ebiten.Image, userPitch []float64, songPitch []float64, currTime float64, sw, sh int) {
	var prevX, prevY float64
	first := true

	latencyOffset := config.AudioLatencyMs / 1000.0

	for i := 0; i < len(userPitch); i += 2 {
		rawT := userPitch[i] / 1000.0
		t := rawT - latencyOffset
		p := userPitch[i+1]

		if p <= 10 {
			first = true
			continue
		}

		x := (t-currTime)*config.PixelsPerSec + v.OffsetX
		y := v.FreqToY(p)

		if x < -50 {
			continue
		}
		if x > float64(sw) {
			break
		}

		col := color.RGBA{255, 200, 50, 255}

		sIdx := int(t * 100)
		if sIdx >= 0 && sIdx < len(songPitch) {
			ref := songPitch[sIdx]
			if ref > 10 && math.Abs(FreqToMidi(p)-FreqToMidi(ref)) < 0.7 {
				col = color.RGBA{50, 255, 50, 255}
			}
		}

		if !first {
			ebitenutil.DrawLine(screen, prevX, prevY, x, y, col)
		}
		prevX, prevY = x, y
		first = false
	}
}

/*
DrawCurrentPitch renders a white square marker at the current pitch position.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - pitch: float64 - Current pitch in Hz

Called by:
  - App.drawPlayingMode, App.drawNoAudioMode

Task:
  - Show real-time pitch indicator

Logic:
 1. If pitch > 10: draw 10x10 white rectangle centered on (OffsetX, FreqToY(pitch))

Output:
  - None (draws to screen)
*/
func (v *PitchVisualizer) DrawCurrentPitch(screen *ebiten.Image, pitch float64) {
	if pitch > 10 {
		y := v.FreqToY(pitch)
		ebitenutil.DrawRect(screen, v.OffsetX-5, y-5, 10, 10, color.White)
	}
}

/*
DrawNowLine draws the vertical timeline indicator.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sh: int - Screen height

Called by:
  - App.drawPlayingMode

Task:
  - Draw vertical gray line at "now" position

Logic:
 1. Draw vertical line from (OffsetX, 0) to (OffsetX, sh)

Output:
  - None (draws to screen)
*/
func (v *PitchVisualizer) DrawNowLine(screen *ebiten.Image, sh int) {
	ebitenutil.DrawLine(screen, v.OffsetX, 0, v.OffsetX, float64(sh), color.Gray{100})
}

/*
DrawControls renders keyboard shortcut hints at bottom of screen.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sh: int - Screen height

Called by:
  - App.drawPlayingMode

Task:
  - Show available controls to user

Logic:
 1. Draw text at (10, sh-20)

Output:
  - None (draws to screen)
*/
func DrawControls(screen *ebiten.Image, sh int) {
	ebitenutil.DebugPrintAt(screen, "SPACE:Pause  ←→:±10s  F:Fullscreen  ESC:Exit", 10, sh-20)
}
