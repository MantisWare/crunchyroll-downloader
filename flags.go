package main

import "flag"

var (
	audioLang     = flag.String("audio-lang", "ja-JP", "Audio language")
	subtitlesLang = flag.String("subs-lang", "en-US", "Subtitles language")
	videoQuality  = flag.String("video-quality", "1080p", "Video quality")
	audioQuality  = flag.String("audio-quality", "192k", "Audio quality")
	seasonNumber  = flag.Int("season", 0, "Season number. Not used if an episode link is entered")
	etpRt         = flag.String("etp-rt", "", "The \"etp_rt\" cookie value of your account")
	browserLogin  = flag.Bool("browser-login", false, "Open a browser to sign in and capture the etp_rt cookie")
	showHelp      = flag.Bool("help", false, "Show detailed help and usage information")
	outputDir     = "."
)
