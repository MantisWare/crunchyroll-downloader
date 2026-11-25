//go:build !gui

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

func printHelp() {
	fmt.Print(`Crunchyroll Downloader
  Download anime from Crunchyroll with Widevine DRM decryption.

USAGE
  crunchyroll-downloader [options]

OPTIONS
  -url string          Crunchyroll episode/series URL, or a video page URL
  -urls string         Path to a text file with one URL per line
  -etp-rt string       The "etp_rt" cookie value from your Crunchyroll account (required for Crunchyroll)
  -browser-login       Open a browser to sign in and save the required cookie
  -audio-lang string   Audio language / dub (default "ja-JP")
  -dub-fallback        Download the default dub when the selected one is missing
  -subs-lang string    Subtitles language (default "en-US")
  -video-quality string Video quality: 1080p, 720p, 480p, 360p (default "1080p")
  -audio-quality string Audio quality: 192k, 128k, 96k (default "192k")
  -season int          Season number to download (omit to download all seasons)
  -help                Show this help message

SETUP
  1. Crunchyroll account (required)
     Add -browser-login to open a supported browser and sign in automatically.

     Manual fallback: log in to crunchyroll.com, open Developer Tools, then:
       Firefox:  Storage → Cookies → etp_rt
       Chrome:   Application → Cookies → etp_rt

  2. Widevine CDM (required)
     Place one of the following in ./ or assets/:
       - A .wvd file
       - client_id.bin + private_key.pem

  3. FFmpeg (required)
     Must be installed and available in your PATH.
     https://www.ffmpeg.org/download.html

  4. Video downloads also need yt-dlp 2026.08.19 or newer, and Deno, on PATH.
     A video page does not use the Crunchyroll cookie. Sign in from the Video tab when a site asks for an account.

EXAMPLES
  Download a single episode (Japanese audio, English subtitles):
    crunchyroll-downloader \
      -url https://www.crunchyroll.com/watch/GE00198973JAJP/dawn-and-confusion \
      -etp-rt YOUR_TOKEN

  Download a season with English dub:
    crunchyroll-downloader \
      -url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise \
      -season 1 -audio-lang en-US -etp-rt YOUR_TOKEN

  Download all seasons of a series:
    crunchyroll-downloader \
      -url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise \
      -etp-rt YOUR_TOKEN

  Batch download from a file:
    crunchyroll-downloader -urls list.txt -audio-lang pt-BR -etp-rt YOUR_TOKEN

  Download a video (YouTube or another page):
    crunchyroll-downloader \
      -url https://www.youtube.com/watch?v=jNQXAC9IVRw \
      -video-quality 720p

LANGUAGES
`)

	codes := make([]string, 0, len(languageNames))
	for code := range languageNames {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	fmt.Println("  Code       Language")
	fmt.Println("  ─────────  ────────────────────────────")
	for _, code := range codes {
		fmt.Printf("  %-9s  %s\n", code, languageNames[code])
	}
	fmt.Println()

	fmt.Print(`  Available dubs/subs vary per title on Crunchyroll.

MORE INFO
  https://github.com/MantisWare/crunchyroll-downloader
`)
}

func processUrl(url string) {
	if isYouTubeHost(url) || isOtherVideoPage(url) {
		if isYouTubeHost(url) {
			if _, err := parseYouTubeURL(url); err != nil {
				fmt.Println(err)
				return
			}
		}
		if err := downloadYouTubeURL(defaultJobEnv(), url); err != nil {
			fmt.Printf("Error: %s\n", err)
		}
		return
	}

	parsed, err := parseContentURL(url)
	if err != nil {
		fmt.Println(err)
		return
	}

	if parsed.Kind == kindWatch {
		info := getEpisodeInfo(parsed.ID, *audioLang)
		contentID, audioLocale, ok := resolveWatchEpisodeID(parsed.ID, info, *audioLang, *dubFallback)
		if !ok {
			return
		}
		info.EpisodeMetadata.AudioLocale = audioLocale
		downloadEpisode(defaultJobEnv(), contentID, info)
		return
	}

	seasons := getSeasons(parsed.ID, *audioLang)

	if *seasonNumber != 0 {
		episodes, ok := resolveSeasonEpisodes(seasons, *seasonNumber, *audioLang)
		if !ok {
			if len(seasons) == 0 {
				fmt.Printf("This anime has no season %v!\n", *seasonNumber)
			}
			return
		}
		downloadSeason(defaultJobEnv(), episodes)
		return
	}

	print("No season number specified, downloading all seasons...\n")

	for _, season := range seasonsForAudio(seasons, *audioLang) {
		episodes, ok := resolveSeasonEpisodes(seasons, season.SeasonNumber, *audioLang)
		if !ok || len(episodes) == 0 {
			continue
		}
		downloadSeason(defaultJobEnv(), episodes)
	}
}

func main() {
	url := flag.String("url", "", "Crunchyroll or video page URL to download")
	urlsFile := flag.String("urls", "", "Path to a text file with one URL per line")

	flag.Usage = func() { printHelp() }
	flag.Parse()

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	if *url == "" && *urlsFile == "" && !*browserLogin {
		printHelp()
		os.Exit(1)
	}
	targets, err := collectCLIURLs(*url, *urlsFile)
	if err != nil {
		fmt.Printf("Failed to open URLs file: %s\n", err)
		os.Exit(1)
	}

	cfg := loadAppConfig()
	if *browserLogin {
		fmt.Println("Opening Crunchyroll sign-in in a temporary browser window...")
		fmt.Println("Complete the login there; this process will continue automatically.")

		signalCtx, stopSignals := signal.NotifyContext(
			context.Background(),
			os.Interrupt,
			syscall.SIGTERM,
		)
		loginCtx, cancelLogin := context.WithTimeout(signalCtx, browserLoginTimeout)
		cookie, loginErr := loginWithBrowser(loginCtx)
		cancelLogin()
		stopSignals()
		if loginErr != nil {
			fmt.Printf("Error: browser sign-in failed: %s\n", loginErr)
			os.Exit(1)
		}

		*etpRt = cookie
	}
	if *browserLogin || urlsRequireCrunchyrollAuth(targets) {
		if authErr := prepareCrunchyrollAuth(cfg); authErr != nil {
			fmt.Println(authErr.Error())
			os.Exit(1)
		}
	}

	if *url == "" && *urlsFile == "" {
		return
	}

	if *urlsFile != "" {
		fmt.Printf("Found %d URLs to download\n\n", len(targets))
		for i, u := range targets {
			fmt.Printf("=== [%d/%d] %s ===\n", i+1, len(targets), u)
			processUrl(u)
			fmt.Println()
		}
		return
	}
	processUrl(*url)
}

func collectCLIURLs(single, filePath string) ([]string, error) {
	if filePath != "" {
		return readURLFile(filePath)
	}
	single = strings.TrimSpace(single)
	if single == "" {
		return nil, nil
	}
	return []string{single}, nil
}

func readURLFile(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var urls []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && strings.HasPrefix(line, "http") {
			urls = append(urls, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return urls, nil
}

func prepareCrunchyrollAuth(cfg appConfig) error {
	if *etpRt == "" {
		if saved := cfg.EtpRt; saved != "" {
			*etpRt = saved
		}
	}
	if *etpRt == "" {
		return fmt.Errorf("Error: -etp-rt is required.\nTo get your etp_rt cookie:\n  1. Re-run with -browser-login, or log in to crunchyroll.com\n  2. For manual setup, open Developer Tools\n     Firefox:  Storage → Cookies → etp_rt\n     Chrome:   Application → Cookies → etp_rt\n\nRun with -help for full usage information.")
	}
	if err := RefreshAccessToken(*etpRt); err != nil {
		return fmt.Errorf("Error: failed to sign in with the etp_rt cookie: %s", err)
	}
	if *browserLogin {
		cfg.EtpRt = *etpRt
		saveAppConfig(cfg)
		fmt.Println("Signed in successfully. The session cookie was saved.")
	}
	return nil
}
