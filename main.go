//go:build !gui

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func printHelp() {
	fmt.Print(`Crunchyroll Downloader
  Download anime from Crunchyroll with Widevine DRM decryption.

USAGE
  crunchyroll-downloader [options]

OPTIONS
  -url string          URL of the episode or series to download
  -urls string         Path to a text file with one URL per line
  -etp-rt string       The "etp_rt" cookie value from your Crunchyroll account (required)
  -audio-lang string   Audio language / dub (default "ja-JP")
  -subs-lang string    Subtitles language (default "en-US")
  -video-quality string Video quality: 1080p, 720p, 480p, 360p (default "1080p")
  -audio-quality string Audio quality: 192k, 128k, 96k (default "192k")
  -season int          Season number to download (omit to download all seasons)
  -help                Show this help message

SETUP
  1. etp_rt cookie (required)
     Log in to crunchyroll.com, open Developer Tools, then:
       Firefox:  Storage → Cookies → etp_rt
       Chrome:   Application → Cookies → etp_rt

  2. Widevine CDM (required)
     Place one of the following in ./ or assets/:
       - A .wvd file
       - client_id.bin + private_key.pem

  3. FFmpeg (required)
     Must be installed and available in your PATH.
     https://www.ffmpeg.org/download.html

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
	parsed, err := parseContentURL(url)
	if err != nil {
		fmt.Println(err)
		return
	}

	if parsed.Kind == kindWatch {
		info := getEpisodeInfo(parsed.ID)
		contentID, ok := resolveWatchEpisodeID(parsed.ID, info, *audioLang)
		if !ok {
			return
		}
		downloadEpisode(contentID, videoQuality, audioQuality, subtitlesLang, info)
		return
	}

	seasons := getSeasons(parsed.ID)

	if *seasonNumber != 0 {
		episodes, ok := resolveSeasonEpisodes(seasons, *seasonNumber, *audioLang)
		if !ok {
			if len(seasons) == 0 {
				fmt.Printf("This anime has no season %v!\n", *seasonNumber)
			}
			return
		}
		downloadSeason(videoQuality, audioQuality, subtitlesLang, episodes)
		return
	}

	print("No season number specified, downloading all seasons...\n")

	for _, season := range seasonsForAudio(seasons, *audioLang) {
		episodes, ok := resolveSeasonEpisodes(seasons, season.SeasonNumber, *audioLang)
		if !ok || len(episodes) == 0 {
			continue
		}
		downloadSeason(videoQuality, audioQuality, subtitlesLang, episodes)
	}
}

func main() {
	url := flag.String("url", "", "URL of the episode/season to download")
	urlsFile := flag.String("urls", "", "Path to a text file with one URL per line")

	flag.Usage = func() { printHelp() }
	flag.Parse()

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	if *url == "" && *urlsFile == "" {
		printHelp()
		os.Exit(1)
	}
	if *etpRt == "" {
		if saved := loadAppConfig().EtpRt; saved != "" {
			*etpRt = saved
		}
	}
	if *etpRt == "" {
		fmt.Println("Error: -etp-rt is required.")
		fmt.Println("To get your etp_rt cookie:")
		fmt.Println("  1. Log in to crunchyroll.com")
		fmt.Println("  2. Open Developer Tools")
		fmt.Println("     Firefox:  Storage → Cookies → etp_rt")
		fmt.Println("     Chrome:   Application → Cookies → etp_rt")
		fmt.Println("\nRun with -help for full usage information.")
		os.Exit(1)
	}

	token = GetAccessToken(*etpRt)

	if *urlsFile != "" {
		file, err := os.Open(*urlsFile)
		if err != nil {
			fmt.Printf("Failed to open URLs file: %s\n", err)
			os.Exit(1)
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

		fmt.Printf("Found %d URLs to download\n\n", len(urls))
		for i, u := range urls {
			fmt.Printf("=== [%d/%d] %s ===\n", i+1, len(urls), u)
			processUrl(u)
			fmt.Println()
		}
	} else {
		processUrl(*url)
	}
}
