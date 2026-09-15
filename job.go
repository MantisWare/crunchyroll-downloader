package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

// jobEnv is the per-run environment for a download or conversion. The CLI
// fills it from flags once; each GUI tab builds its own so parallel jobs
// do not share cancel, logs, output folder, or quality settings.
type jobEnv struct {
	ctx           context.Context
	log           io.Writer
	outputDir     string
	videoQuality  string
	audioQuality  string
	subtitlesLang string
	audioLang     string
	dubFallback   bool
}

func (e jobEnv) context() context.Context {
	if e.ctx == nil {
		return context.Background()
	}
	return e.ctx
}

func (e jobEnv) cancelled() bool {
	return e.context().Err() != nil
}

func (e jobEnv) writer() io.Writer {
	if e.log == nil {
		return os.Stdout
	}
	return e.log
}

func (e jobEnv) dir() string {
	if e.outputDir == "" {
		return outputDir
	}
	return e.outputDir
}

func jobPrintf(env jobEnv, format string, args ...any) {
	fmt.Fprintf(env.writer(), format, args...)
}

func jobPrint(env jobEnv, args ...any) {
	fmt.Fprint(env.writer(), args...)
}

func jobPrintln(env jobEnv, args ...any) {
	fmt.Fprintln(env.writer(), args...)
}

func defaultJobEnv() jobEnv {
	return jobEnv{
		ctx:           downloadContext(),
		log:           os.Stdout,
		outputDir:     outputDir,
		videoQuality:  *videoQuality,
		audioQuality:  *audioQuality,
		subtitlesLang: *subtitlesLang,
		audioLang:     *audioLang,
		dubFallback:   *dubFallback,
	}
}
