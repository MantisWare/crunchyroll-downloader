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
var trailingRotationTag = regexp.MustCompile(` \[(?:90cw|90ccw|180)\]$`)

const (
	rotationNone  = "No change"
	rotation90CW  = "90° clockwise"
	rotation180   = "180°"
	rotation90CCW = "90° counterclockwise"
)

var convertRotations = []string{rotationNone, rotation90CW, rotation180, rotation90CCW}

func validRotation(rotation string) bool {
	for _, option := range convertRotations {
		if option == rotation {
			return true
		}
	}
	return false
}

func conversionOutputName(input, videoQuality, rotation string) string {
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	base = trailingRotationTag.ReplaceAllString(base, "")
	base = trailingQualityTag.ReplaceAllString(base, "")
	name := base + " [" + videoQuality + "]"
	if tag := rotationFileTag(rotation); tag != "" {
		name += " [" + tag + "]"
	}
	return name + ".mp4"
}

func rotationFileTag(rotation string) string {
	switch rotation {
	case rotation90CW:
		return "90cw"
	case rotation180:
		return "180"
	case rotation90CCW:
		return "90ccw"
	default:
		return ""
	}
}

func conversionVideoFilter(videoQuality, rotation string) (string, error) {
	height, err := strconv.Atoi(strings.TrimSuffix(videoQuality, "p"))
	if err != nil || height <= 0 {
		return "", fmt.Errorf("invalid video quality %q", videoQuality)
	}
	scale := fmt.Sprintf("scale=-2:%d", height)
	switch rotation {
	case "", rotationNone:
		return scale, nil
	case rotation90CW:
		return scale + ",transpose=1", nil
	case rotation180:
		return scale + ",transpose=1,transpose=1", nil
	case rotation90CCW:
		return scale + ",transpose=2", nil
	default:
		return "", fmt.Errorf("unsupported rotation %q", rotation)
	}
}

func conversionJobsForFiles(paths []string, videoQuality, rotation string) ([]conversionJob, error) {
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

		output := filepath.Join(filepath.Dir(clean), conversionOutputName(clean, videoQuality, rotation))
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

func conversionJobsForFolder(folder, videoQuality, rotation string) ([]conversionJob, error) {
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

	jobs, err := conversionJobsForFiles(paths, videoQuality, rotation)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].Output = filepath.Join(outputDir, conversionOutputName(jobs[i].Input, videoQuality, rotation))
	}
	return jobs, nil
}

func convertVideo(env jobEnv, job conversionJob, videoQuality, audioQuality, rotation string) error {
	filter, err := conversionVideoFilter(videoQuality, rotation)
	if err != nil {
		return err
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
		"-vf", filter,
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", audioQuality,
		"-movflags", "+faststart",
		job.Output,
	}

	cmd := exec.CommandContext(env.context(), "ffmpeg", args...)
	cmd.Stdout = env.writer()
	cmd.Stderr = env.writer()

	if err := cmd.Run(); err != nil {
		_ = os.Remove(job.Output)
		if env.cancelled() {
			return errCancelled
		}
		return fmt.Errorf("FFmpeg conversion: %w", err)
	}
	return nil
}
