package youtube

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"singAssist/internal/config"
)

/*
Download downloads an audio file from YouTube using yt-dlp.

Input:
  - query: string - YouTube search query (e.g., "Never Gonna Give You Up")

Called by:
  - main.main when user provides -yt flag

Task:
  - Search YouTube for the query
  - Download best quality audio and convert to MP3
  - Save to songs/<sanitized_name>/song.mp3

Logic:
 1. Sanitize query to create valid folder name
 2. Create song directory using config.EnsureSongDir
 3. Check if song already exists, skip download if so
 4. Execute yt-dlp with: ytsearch1:<query>, extract audio, mp3 format, best quality
 5. Verify downloaded file exists

Output:
  - string: Song directory path (e.g., "songs/Never_Gonna_Give_You_Up")
  - error: nil on success, wrapped error with details on failure
*/
func Download(query string) (string, error) {
	songName := sanitizeName(query)

	songDir, err := config.EnsureSongDir(songName)
	if err != nil {
		return "", fmt.Errorf("failed to create song directory: %w", err)
	}

	paths := config.GetSongPaths(songDir)

	if _, err := os.Stat(paths.SongFile); err == nil {
		fmt.Printf("Song already exists: %s\n", paths.SongFile)
		return songDir, nil
	}

	cmd := exec.Command("yt-dlp",
		fmt.Sprintf("ytsearch1:%s", query),
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
		"-o", paths.SongFile,
	)

	fmt.Printf("Downloading: %s\n", query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp failed: %w\nOutput: %s", err, string(output))
	}

	if _, err := os.Stat(paths.SongFile); os.IsNotExist(err) {
		return "", fmt.Errorf("download failed - file not created")
	}

	fmt.Printf("Downloaded to: %s\n", paths.SongFile)
	return songDir, nil
}

/*
sanitizeName converts a search query into a valid filesystem folder name.

Input:
  - query: string - Raw search query with potential special characters

Called by:
  - Download when creating folder name from YouTube query

Task:
  - Remove special characters
  - Replace spaces with underscores
  - Limit length to 50 characters

Logic:
 1. Remove all non-alphanumeric characters except spaces using regex
 2. Trim leading/trailing whitespace
 3. Replace remaining spaces with underscores
 4. Truncate to 50 characters if longer
 5. Return "downloaded_song" if result is empty

Output:
  - string: Sanitized folder name safe for filesystem use
*/
func sanitizeName(query string) string {
	reg := regexp.MustCompile(`[^a-zA-Z0-9\s]+`)
	name := reg.ReplaceAllString(query, "")

	name = strings.ReplaceAll(strings.TrimSpace(name), " ", "_")

	if len(name) > 50 {
		name = name[:50]
	}

	if name == "" {
		name = "downloaded_song"
	}

	return name
}

/*
ImportSong copies an existing MP3 file into the songs folder structure.

Input:
  - srcPath: string - Path to existing MP3 file (e.g., "Kasoor.mp3")

Called by:
  - main.main when user provides an MP3 file path instead of song folder

Task:
  - Extract song name from filename
  - Create song directory
  - Copy file as song.mp3

Logic:
 1. Extract base filename and remove .mp3 extension to get song name
 2. Create directory using config.EnsureSongDir
 3. If song.mp3 already exists in destination, return existing path
 4. Read source file completely into memory
 5. Write to destination as song.mp3
 6. Print confirmation message

Output:
  - string: Song directory path (e.g., "songs/Kasoor")
  - error: nil on success, wrapped error on read/write failure
*/
func ImportSong(srcPath string) (string, error) {
	baseName := filepath.Base(srcPath)
	ext := filepath.Ext(baseName)
	songName := strings.TrimSuffix(baseName, ext)

	songDir, err := config.EnsureSongDir(songName)
	if err != nil {
		return "", err
	}

	paths := config.GetSongPaths(songDir)

	if _, err := os.Stat(paths.SongFile); err == nil {
		return songDir, nil
	}

	input, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to read source: %w", err)
	}

	if err := os.WriteFile(paths.SongFile, input, 0644); err != nil {
		return "", fmt.Errorf("failed to write song: %w", err)
	}

	fmt.Printf("Imported: %s -> %s\n", srcPath, paths.SongFile)
	return songDir, nil
}
