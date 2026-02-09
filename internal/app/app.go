package app

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"sync"
	"time"

	"singAssist/internal/audio"
	"singAssist/internal/config"
	"singAssist/internal/ui"

	"github.com/hajimehoshi/ebiten/v2"
	eaudio "github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

type GameState int

const (
	StateStartScreen GameState = iota
	StateCalibrating
	StatePlaying
)

/*
App is the main application structure holding all game state.

Fields:
  - state: Current GameState (StartScreen, Calibrating, Playing)
  - mode: Current audio.Mode (Singing, Instrumental, FullMix, NoAudio)
  - songDir: Path to song folder (e.g., "songs/MySong")
  - audioPlayer: Ebiten audio player for playback
  - songPitch: Pre-analyzed pitch data from song (100 samples/sec)
  - userPitch: Recorded user pitch pairs [timeMs, pitch, ...]
  - mic: Microphone handler for real-time input
  - mu: Mutex for thread-safe access to shared state
  - message: Status/error message to display
*/
type App struct {
	state   GameState
	mode    audio.Mode
	songDir string

	audioPlayer *eaudio.Player
	songPitch   []float64

	userPitch []float64

	mic *audio.MicHandler

	mu      sync.Mutex
	message string
}

/*
New creates a new App instance for the given song directory.

Input:
  - songDir: string - Path to song folder (e.g., "songs/MySong")

Called by:
  - main.main after resolving song path

Task:
  - Initialize app with default state

Logic:
 1. Set state to StartScreen
 2. Store songDir
 3. Initialize empty userPitch slice

Output:
  - *App: Ready to be passed to ebiten.RunGame
*/
func New(songDir string) *App {
	return &App{
		state:     StateStartScreen,
		songDir:   songDir,
		userPitch: make([]float64, 0),
	}
}

/*
SongName returns the display name of the current song.

Input:
  - None

Called by:
  - ui.DrawStartScreen for title display

Task:
  - Extract human-readable name from path

Logic:
 1. Return base name of songDir

Output:
  - string: Song folder name (e.g., "MySong")
*/
func (a *App) SongName() string {
	return filepath.Base(a.songDir)
}

/*
Update is called by Ebiten every frame to handle game logic.

Input:
  - None (ebiten.Game interface)

Called by:
  - Ebiten game loop (~60 times per second)

Task:
  - Route input handling based on current state

Logic:
 1. Get current window size
 2. If StartScreen: check for button clicks
 3. If Playing/Calibrating: check for keyboard input

Output:
  - error: nil always (returning error would exit game)
*/
func (a *App) Update() error {
	sw, sh := ebiten.WindowSize()

	if a.state == StateStartScreen {
		a.handleStartScreenInput(sw, sh)
	} else if a.state == StatePlaying || a.state == StateCalibrating {
		a.handlePlayingInput()
	}

	return nil
}

/*
handleStartScreenInput checks for button clicks on the menu screen.

Input:
  - sw, sh: int - Screen width and height

Called by:
  - Update when state is StateStartScreen

Task:
  - Detect clicks on mode selection buttons

Logic:
 1. Check for left mouse button press
 2. Get cursor position
 3. Check if cursor is inside each button's bounds
 4. Call startGame with corresponding mode if clicked

Output:
  - None (calls startGame to change state)
*/
func (a *App) handleStartScreenInput(sw, sh int) {
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()

		if ui.InRect(x, y, sw/2-100, sh/2-120, 200, 50) {
			a.startGame(audio.ModeSinging)
		}
		if ui.InRect(x, y, sw/2-100, sh/2-60, 200, 50) {
			a.startGame(audio.ModeInstrumental)
		}
		if ui.InRect(x, y, sw/2-100, sh/2, 200, 50) {
			a.startGame(audio.ModeFullMix)
		}
		if ui.InRect(x, y, sw/2-100, sh/2+60, 200, 50) {
			a.startGame(audio.ModeNoAudio)
		}
	}
}

/*
handlePlayingInput processes keyboard input during playback.

Input:
  - None

Called by:
  - Update when state is StateCalibrating or StatePlaying

Task:
  - Handle playback controls and navigation

Logic:
 1. F key: toggle fullscreen
 2. Space: toggle play/pause
 3. Left arrow: rewind 10 seconds
 4. Right arrow: forward 10 seconds
 5. Escape: exit to menu

Output:
  - None (modifies app state or audio player)
*/
func (a *App) handlePlayingInput() {
	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		if a.audioPlayer != nil {
			if a.audioPlayer.IsPlaying() {
				a.audioPlayer.Pause()
			} else {
				a.audioPlayer.Play()
			}
		}
	}

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

	if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
		if a.audioPlayer != nil {
			pos := a.audioPlayer.Position()
			a.audioPlayer.SetPosition(pos + 10*time.Second)
		}
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		a.exitToMenu()
	}
}

/*
startGame begins a new playing session with the given mode.

Input:
  - m: audio.Mode - Selected playback mode

Called by:
  - handleStartScreenInput when button is clicked

Task:
  - Clean up previous session
  - Initialize microphone
  - Start calibration

Logic:
 1. Call cleanup to release previous resources
 2. Set mode and state to Calibrating
 3. Reset userPitch slice
 4. Create and start microphone handler
 5. Launch calibrateAndPlay goroutine

Output:
  - None (transitions to calibration state)
*/
func (a *App) startGame(m audio.Mode) {
	a.cleanup()

	a.mode = m
	a.state = StateCalibrating
	a.message = "Calibrating background noise..."
	a.userPitch = make([]float64, 0)

	a.mic = audio.NewMicHandler()
	if err := a.mic.Start(); err != nil {
		log.Printf("Failed to start microphone: %v", err)
		a.message = "Error: Failed to start microphone"
		a.state = StateStartScreen
		return
	}

	go a.calibrateAndPlay()
}

/*
calibrateAndPlay runs calibration then loads song in background goroutine.

Input:
  - None

Called by:
  - startGame (as goroutine)

Task:
  - Calibrate noise threshold
  - Load and analyze song
  - Start playback

Logic:
 1. Run mic.Calibrate for 2 seconds
 2. Update state to Playing
 3. Call audio.LoadAndAnalyzeSong
 4. If error: display error message, return
 5. Store player and songPitch
 6. Start playback
 7. Launch micLoop goroutine

Output:
  - None (updates app state, starts playback)
*/
func (a *App) calibrateAndPlay() {
	a.mic.Calibrate(2 * time.Second)

	a.mu.Lock()
	a.state = StatePlaying
	a.message = "Loading Song..."
	a.mu.Unlock()

	result, err := audio.LoadAndAnalyzeSong(a.songDir, a.mode, func(msg string) {
		a.mu.Lock()
		a.message = msg
		a.mu.Unlock()
	})

	if err != nil {
		a.mu.Lock()
		a.message = "Error: " + err.Error()
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	a.audioPlayer = result.Player
	a.songPitch = result.SongPitch
	a.message = ""
	if a.audioPlayer != nil {
		a.audioPlayer.Play()
	}
	a.mu.Unlock()

	go a.micLoop()
}

/*
micLoop continuously reads microphone and records user pitch.

Input:
  - None

Called by:
  - calibrateAndPlay (as goroutine)

Task:
  - Read microphone input
  - Detect pitch
  - Record timestamped pitch data

Logic:
 1. Loop until mic is nil or Done
 2. Read microphone buffer
 3. If not Playing state, continue
 4. Detect pitch using current mode settings
 5. Lock mutex
 6. If playing: append (time, pitch) to userPitch
 7. Call pruneUserPitch to limit memory usage
 8. Unlock mutex

Output:
  - None (appends to userPitch slice)
*/
func (a *App) micLoop() {
	for {
		if a.mic == nil || a.mic.IsDone() {
			return
		}

		if err := a.mic.Read(); err != nil {
			return
		}

		if a.state != StatePlaying {
			continue
		}

		pitch := a.mic.DetectPitchFromMic(a.mode)

		a.mu.Lock()
		if a.audioPlayer != nil && a.audioPlayer.IsPlaying() {
			pos := a.audioPlayer.Position()
			a.userPitch = append(a.userPitch, float64(pos.Milliseconds()), pitch)
			a.pruneUserPitch(pos.Milliseconds())
		}
		a.mu.Unlock()
	}
}

/*
pruneUserPitch removes old pitch data to limit memory usage.

Input:
  - currentMs: int64 - Current playback position in milliseconds

Called by:
  - micLoop after appending new pitch data

Task:
  - Remove pitch data older than MaxUserPitchHistory seconds

Logic:
 1. Calculate minimum time threshold
 2. If threshold <= 0, nothing to prune
 3. Find first index with time >= threshold
 4. Create new slice containing only recent data
 5. Replace userPitch with new slice (allows GC of old data)

Output:
  - None (modifies userPitch slice in place)
*/
func (a *App) pruneUserPitch(currentMs int64) {
	if len(a.userPitch) == 0 {
		return
	}

	minMs := currentMs - int64(config.MaxUserPitchHistory*1000)
	if minMs <= 0 {
		return
	}

	cutIdx := 0
	for i := 0; i < len(a.userPitch); i += 2 {
		if a.userPitch[i] >= float64(minMs) {
			cutIdx = i
			break
		}
		cutIdx = i + 2
	}

	if cutIdx > 0 && cutIdx < len(a.userPitch) {
		newPitch := make([]float64, len(a.userPitch)-cutIdx)
		copy(newPitch, a.userPitch[cutIdx:])
		a.userPitch = newPitch
	}
}

/*
exitToMenu returns to the start screen.

Input:
  - None

Called by:
  - handlePlayingInput when ESC is pressed

Task:
  - Exit fullscreen
  - Clean up resources
  - Return to menu

Logic:
 1. Disable fullscreen
 2. Call cleanup
 3. Set state to StartScreen

Output:
  - None (transitions to start screen)
*/
func (a *App) exitToMenu() {
	ebiten.SetFullscreen(false)
	a.cleanup()
	a.state = StateStartScreen
}

/*
cleanup releases all resources from the current session.

Input:
  - None

Called by:
  - exitToMenu when returning to menu
  - startGame before starting new session

Task:
  - Stop microphone
  - Close audio player
  - Clear data structures

Logic:
 1. Stop and nil microphone handler
 2. Pause, close, and nil audio player
 3. Nil songPitch slice
 4. Reset userPitch to empty slice
 5. Clear message

Output:
  - None (releases resources)
*/
func (a *App) cleanup() {
	if a.mic != nil {
		a.mic.Stop()
		a.mic = nil
	}

	if a.audioPlayer != nil {
		a.audioPlayer.Pause()
		a.audioPlayer.Close()
		a.audioPlayer = nil
	}

	a.songPitch = nil
	a.userPitch = make([]float64, 0)
	a.message = ""
}

/*
Draw is called by Ebiten every frame to render the screen.

Input:
  - screen: *ebiten.Image - Target drawing surface

Called by:
  - Ebiten game loop (~60 times per second)

Task:
  - Route rendering based on current state

Logic:
 1. Get window size
 2. If StartScreen: call ui.DrawStartScreen
 3. If Calibrating: call ui.DrawCalibrating
 4. Lock mutex for thread-safe data access
 5. Fill screen black
 6. If message set: display it
 7. If NoAudio mode: call drawNoAudioMode
 8. If not playing: return
 9. Call drawPlayingMode

Output:
  - None (draws to screen)
*/
func (a *App) Draw(screen *ebiten.Image) {
	sw, sh := ebiten.WindowSize()

	if a.state == StateStartScreen {
		ui.DrawStartScreen(screen, sw, sh, a.SongName())
		return
	}

	if a.state == StateCalibrating {
		ui.DrawCalibrating(screen, sw, sh)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	screen.Fill(color.Black)

	if a.message != "" {
		ui.DrawMessage(screen, a.message)
	}

	if a.mode == audio.ModeNoAudio {
		a.drawNoAudioMode(screen, sw, sh)
		return
	}

	if a.audioPlayer == nil || !a.audioPlayer.IsPlaying() {
		return
	}

	a.drawPlayingMode(screen, sw, sh)
}

/*
drawNoAudioMode renders the practice mode without audio playback.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sw, sh: int - Screen dimensions

Called by:
  - Draw when mode is ModeNoAudio

Task:
  - Display current pitch with visual feedback

Logic:
 1. Get current mic pitch
 2. Convert to note name
 3. Display pitch info text
 4. If pitch detected: draw pitch marker
 5. Show exit hint

Output:
  - None (draws to screen)
*/
func (a *App) drawNoAudioMode(screen *ebiten.Image, sw, sh int) {
	pitch := 0.0
	if a.mic != nil {
		pitch = a.mic.Pitch
	}
	playNote, _ := ui.FreqToNote(pitch)
	stats := fmt.Sprintf("YOUR PITCH: %-4s (%.0f Hz)\n\nNo audio playback - practice mode", playNote, pitch)
	ebitenutil.DebugPrintAt(screen, stats, 10, 10)

	if pitch > 10 {
		vis := ui.NewPitchVisualizer(sw, sh)
		vis.DrawCurrentPitch(screen, pitch)
	}

	ebitenutil.DebugPrintAt(screen, "ESC: Exit", 10, sh-20)
}

/*
drawPlayingMode renders the main playing interface with pitch visualization.

Input:
  - screen: *ebiten.Image - Target drawing surface
  - sw, sh: int - Screen dimensions

Called by:
  - Draw when state is StatePlaying and audio is playing

Task:
  - Display pitch comparison and scored visualization

Logic:
 1. Get current playback time
 2. Get current mic pitch
 3. Convert user and song pitches to note names
 4. Display pitch comparison stats
 5. Create PitchVisualizer
 6. Draw song pitch line
 7. Draw user pitch trail with hit detection
 8. Draw current pitch marker
 9. Draw "now" line
 10. Draw control hints

Output:
  - None (draws to screen)
*/
func (a *App) drawPlayingMode(screen *ebiten.Image, sw, sh int) {
	currTime := a.audioPlayer.Position().Seconds()

	pitch := 0.0
	if a.mic != nil {
		pitch = a.mic.Pitch
	}

	userNote, userOctave := ui.FreqToNote(pitch)
	songNoteStr := "-"
	songOctave := 0
	songFreq := 0.0

	sIdx := int(currTime * 100)
	if sIdx >= 0 && sIdx < len(a.songPitch) {
		songFreq = a.songPitch[sIdx]
		if songFreq > 10 {
			songNoteStr, songOctave = ui.FreqToNote(songFreq)
		}
	}

	isMatched := false
	if pitch > 10 && songFreq > 10 {
		diff := math.Abs(ui.FreqToMidi(pitch) - ui.FreqToMidi(songFreq))
		isMatched = diff < 0.7
	}

	songDisplay := ui.NoteDisplay{
		Note:   songNoteStr,
		Octave: songOctave,
		Freq:   songFreq,
	}
	userDisplay := ui.NoteDisplay{
		Note:      userNote,
		Octave:    userOctave,
		Freq:      pitch,
		IsMatched: isMatched,
	}
	ui.DrawNoteHUD(screen, sw, songDisplay, userDisplay)

	vis := ui.NewPitchVisualizer(sw, sh)
	vis.DrawSongPitch(screen, a.songPitch, currTime, sw, sh)
	vis.DrawUserPitch(screen, a.userPitch, a.songPitch, currTime, sw, sh)
	vis.DrawCurrentPitch(screen, pitch)
	vis.DrawNowLine(screen, sh)
	ui.DrawControls(screen, sh)
}

/*
Layout returns the logical screen dimensions for Ebiten.

Input:
  - w, h: int - Actual window size (unused)

Called by:
  - Ebiten game loop

Task:
  - Provide fixed logical resolution

Logic:
 1. Return config.ScreenW x config.ScreenH

Output:
  - int, int: Logical width and height (1000 x 600)
*/
func (a *App) Layout(w, h int) (int, int) {
	return config.ScreenW, config.ScreenH
}
