package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"singAssist/internal/app"
	"singAssist/internal/config"
	"singAssist/internal/youtube"

	"github.com/gordonklaus/portaudio"
	"github.com/hajimehoshi/ebiten/v2"
)

/*
main is the application entry point.

Input:
  - Command line args: [-yt "query"] or <song_folder> or <song.mp3>

Task:
  - Parse CLI arguments
  - Initialize audio system
  - Resolve song path
  - Launch game

Logic:
 1. Parse -yt flag for YouTube download
 2. Initialize PortAudio (required for microphone)
 3. If -yt flag: call youtube.Download
 4. Else: use positional argument as song path
 5. If no args: print usage and exit
 6. Verify song.mp3 exists in songDir
 7. If path is .mp3 file: import to songs folder
 8. Create app.New with songDir
 9. Configure Ebiten window
 10. Run game loop

Output:
  - Exit 0 on normal exit, Exit 1 on error
*/
func main() {
	ytQuery := flag.String("yt", "", "YouTube search query to download and play")
	flag.Parse()

	if err := portaudio.Initialize(); err != nil {
		log.Fatal("Failed to initialize PortAudio:", err)
	}
	defer portaudio.Terminate()

	var songDir string

	if *ytQuery != "" {
		fmt.Printf("Downloading from YouTube: %s\n", *ytQuery)
		dir, err := youtube.Download(*ytQuery)
		if err != nil {
			log.Fatal("YouTube download failed:", err)
		}
		songDir = dir
	} else {
		args := flag.Args()
		if len(args) > 0 {
			songDir = args[0]
		} else {
			printUsage()
			os.Exit(1)
		}
	}

	paths := config.GetSongPaths(songDir)
	if _, err := os.Stat(paths.SongFile); os.IsNotExist(err) {
		if filepath.Ext(songDir) == ".mp3" {
			if _, err := os.Stat(songDir); err == nil {
				fmt.Printf("Importing MP3 file: %s\n", songDir)
				dir, err := youtube.ImportSong(songDir)
				if err != nil {
					log.Fatalf("Failed to import song: %v", err)
				}
				songDir = dir
			} else {
				log.Fatalf("File not found: %s", songDir)
			}
		} else {
			log.Fatalf("Song not found: %s\nExpected: %s", songDir, paths.SongFile)
		}
	}

	application := app.New(songDir)

	ebiten.SetWindowSize(config.ScreenW, config.ScreenH)
	ebiten.SetWindowTitle("SingAssist - " + filepath.Base(songDir))
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	if err := ebiten.RunGame(application); err != nil {
		log.Fatal(err)
	}
}

/*
printUsage displays command line help and available songs.

Input:
  - None

Called by:
  - main when no song argument provided

Task:
  - Show usage instructions
  - List available songs

Logic:
 1. Print usage header
 2. Print song folder structure explanation
 3. Print example commands
 4. Scan songs directory for existing songs
 5. List found songs

Output:
  - None (prints to stdout)
*/
func printUsage() {
	fmt.Println("SingAssist - Vocal Practice Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  singAssist <song_folder>           Play from a song folder")
	fmt.Println("  singAssist <song.mp3>              Import and play an MP3 file")
	fmt.Println("  singAssist -yt \"song name\"         Download from YouTube and play")
	fmt.Println()
	fmt.Println("Song Folder Structure:")
	fmt.Println("  songs/<song_name>/")
	fmt.Println("    ├── song.mp3           (original audio)")
	fmt.Println("    ├── vocals.mp3         (separated, created on demand)")
	fmt.Println("    └── accompaniment.mp3  (separated, created on demand)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  singAssist songs/Kasoor")
	fmt.Println("  singAssist Kasoor.mp3")
	fmt.Println("  singAssist -yt \"Never Gonna Give You Up\"")
	fmt.Println()

	if entries, err := os.ReadDir(config.SongsDir); err == nil && len(entries) > 0 {
		fmt.Println("Available songs:")
		for _, e := range entries {
			if e.IsDir() {
				songPath := filepath.Join(config.SongsDir, e.Name(), "song.mp3")
				if _, err := os.Stat(songPath); err == nil {
					fmt.Printf("  - songs/%s\n", e.Name())
				}
			}
		}
	}
}
