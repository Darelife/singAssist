package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	SampleRate          = 44100
	BufferSize          = 2048
	ScreenW             = 1000
	ScreenH             = 600
	PixelsPerSec        = 150.0
	MaxUserPitchHistory = 30.0
	SongsDir            = "songs"
	AudioLatencyMs      = 150.0
)

/*
GetPythonPath returns the absolute path to the Python executable from the virtual environment.

Input:
  - None (reads from venv_path.txt file in current directory)

Called by:
  - audio.LoadAndAnalyzeSong when running Python separator script

Task:
  - Read venv path from venv_path.txt
  - Resolve relative paths to absolute paths
  - Verify Python executable exists

Logic:
 1. Read venv_path.txt file
 2. If file doesn't exist or is empty, return default "python3"
 3. If path is relative, join with current working directory
 4. Construct full path to bin/python
 5. Verify file exists, return path or fallback to "python3"

Output:
  - string: Absolute path to Python executable, or "python3" if not found
*/
func GetPythonPath() string {
	venvBytes, err := os.ReadFile("venv_path.txt")
	if err != nil {
		return "python3"
	}

	venvPath := strings.TrimSpace(string(venvBytes))
	if venvPath == "" {
		return "python3"
	}

	if !filepath.IsAbs(venvPath) {
		cwd, err := os.Getwd()
		if err == nil {
			venvPath = filepath.Join(cwd, venvPath)
		}
	}

	pythonPath := filepath.Join(venvPath, "bin", "python")
	if _, err := os.Stat(pythonPath); err == nil {
		return pythonPath
	}

	return "python3"
}

/*
SongPaths holds all file paths for a song folder.

Fields:
  - Dir: Base directory path (e.g., "songs/MySong")
  - SongFile: Path to original audio (e.g., "songs/MySong/song.mp3")
  - VocalsFile: Path to separated vocals (e.g., "songs/MySong/vocals.mp3")
  - AccompFile: Path to separated accompaniment (e.g., "songs/MySong/accompaniment.mp3")
*/
type SongPaths struct {
	Dir        string
	SongFile   string
	VocalsFile string
	AccompFile string
}

/*
GetSongPaths constructs all file paths for a given song directory.

Input:
  - songDir: string - Base directory path for the song (e.g., "songs/MySong")

Called by:
  - youtube.Download when saving downloaded song
  - youtube.ImportSong when importing MP3 file
  - audio.LoadAndAnalyzeSong when loading song files
  - main.main when verifying song exists

Task:
  - Construct standardized paths for all song files

Logic:
 1. Use songDir as base directory
 2. Join with standard filenames: song.mp3, vocals.mp3, accompaniment.mp3

Output:
  - SongPaths struct with all path fields populated
*/
func GetSongPaths(songDir string) SongPaths {
	return SongPaths{
		Dir:        songDir,
		SongFile:   filepath.Join(songDir, "song.mp3"),
		VocalsFile: filepath.Join(songDir, "vocals.mp3"),
		AccompFile: filepath.Join(songDir, "accompaniment.mp3"),
	}
}

/*
EnsureSongDir creates a song directory inside the songs folder.

Input:
  - songName: string - Name of the song (used as folder name)

Called by:
  - youtube.Download when creating directory for downloaded song
  - youtube.ImportSong when creating directory for imported song

Task:
  - Create nested directory structure songs/<songName>

Logic:
 1. Join SongsDir constant with songName
 2. Create directory with MkdirAll (creates parents if needed)

Output:
  - string: Full path to created directory
  - error: nil on success, filesystem error on failure
*/
func EnsureSongDir(songName string) (string, error) {
	dir := filepath.Join(SongsDir, songName)
	err := os.MkdirAll(dir, 0755)
	return dir, err
}
