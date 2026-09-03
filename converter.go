package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var supportedVideoExtensions = map[string]bool{
	".avi":  true,
	".flv":  true,
	".m4v":  true,
	".mkv":  true,
	".mov":  true,
	".mp4":  true,
	".webm": true,
	".wmv":  true,
}

type conversionJob struct {
	Input  string
	Output string
}

func isSupportedVideo(path string) bool {
	return supportedVideoExtensions[strings.ToLower(filepath.Ext(path))]
}

var trailingQualityTag = regexp.MustCompile(` \[\d+p\]$`)

func conversionOutputName(input, videoQuality string) string {
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	base = trailingQualityTag.ReplaceAllString(base, "")
	return base + " [" + videoQuality + "].mp4"
}

func conversionJobsForFiles(paths []string, videoQuality string) ([]conversionJob, error) {
	jobs := make([]conversionJob, 0, len(paths))
	seen := make(map[string]bool)

	for _, path := range paths {
		clean := filepath.Clean(path)
		if seen[clean] {
			continue
		}
		seen[clean] = true

		info, err := os.Stat(clean)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", clean, err)
		}
		if info.IsDir() || !isSupportedVideo(clean) {
			continue
		}

		output := filepath.Join(filepath.Dir(clean), conversionOutputName(clean, videoQuality))
		if output == clean {
			base := strings.TrimSuffix(filepath.Base(output), filepath.Ext(output))
			output = filepath.Join(filepath.Dir(clean), base+" converted.mp4")
		}

		jobs = append(jobs, conversionJob{Input: clean, Output: output})
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Input < jobs[j].Input
	})
	return jobs, nil
}

func conversionJobsForFolder(folder, videoQuality string) ([]conversionJob, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("reading folder %s: %w", folder, err)
	}

	outputDir := filepath.Join(folder, "converted_"+videoQuality)

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		if isSupportedVideo(path) {
			paths = append(paths, path)
		}
	}

	jobs, err := conversionJobsForFiles(paths, videoQuality)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].Output = filepath.Join(outputDir, conversionOutputName(jobs[i].Input, videoQuality))
	}
	return jobs, nil
}

func convertVideo(job conversionJob, videoQuality, audioQuality string) error {
	height, err := strconv.Atoi(strings.TrimSuffix(videoQuality, "p"))
	if err != nil || height <= 0 {
		return fmt.Errorf("invalid video quality %q", videoQuality)
	}
	if _, err := strconv.Atoi(strings.TrimSuffix(audioQuality, "k")); err != nil {
		return fmt.Errorf("invalid audio quality %q", audioQuality)
	}

	if filepath.Clean(job.Input) == filepath.Clean(job.Output) {
		return fmt.Errorf("input and output paths are the same")
	}
	if err := os.MkdirAll(filepath.Dir(job.Output), 0755); err != nil {
		return fmt.Errorf("creating output folder: %w", err)
	}

	args := []string{
		"-y",
		"-nostdin",
		"-i", job.Input,
		"-vf", fmt.Sprintf("scale=-2:%d", height),
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", audioQuality,
		"-movflags", "+faststart",
		job.Output,
	}

	cmd := exec.CommandContext(downloadContext(), "ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	if err := cmd.Run(); err != nil {
		_ = os.Remove(job.Output)
		if isCancelled() {
			return errCancelled
		}
		return fmt.Errorf("FFmpeg conversion: %w", err)
	}
	return nil
}
