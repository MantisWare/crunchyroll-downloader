package main

import (
	"fmt"
	"os"
	"os/exec"
)

// mergeEverything merges audio, video and subtitles in a single MKV container
func mergeEverything(videoFile, audioFile, subsFile, outputFile string, subtitlesLang *string, info EpisodeInfo) {
	args := []string{
		"-i", videoFile,
		"-i", audioFile,
	}

	if subsFile != "" {
		args = append(args,
			"-i", subsFile,
			"-c:s", "copy",
			"-metadata:s:s:0", fmt.Sprintf("title=%s", languageNames[*subtitlesLang]),
		)
	}

	args = append(args,
		"-c:v", "copy", "-c:a", "copy",
		"-metadata:g", "title="+fmt.Sprintf("S%02vE%02v - %s", info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, info.Title),
		"-metadata:g", "show="+info.EpisodeMetadata.SeriesTitle,
		"-metadata:g", "track="+fmt.Sprintf("%v", info.EpisodeMetadata.EpisodeNumber),
		"-metadata:g", "season_number="+fmt.Sprintf("%v", info.EpisodeMetadata.EpisodeNumber),
		outputFile,
	)

	cmd := exec.Command("ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	// Remove stuff
	_ = os.Remove(videoFile)
	_ = os.Remove(audioFile)
	_ = os.Remove(subsFile)

	fmt.Printf("\nDownload finished! Output file: %s\n\n", outputFile)
}
