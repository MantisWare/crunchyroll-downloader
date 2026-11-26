package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseYouTubeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		kind       youtubeKind
		videoID    string
		playlistID string
		ok         bool
	}{
		{
			name:    "watch",
			raw:     "https://www.youtube.com/watch?v=jNQXAC9IVRw",
			kind:    youtubeVideo,
			videoID: "jNQXAC9IVRw",
			ok:      true,
		},
		{
			name:    "watch with playlist query stays a video",
			raw:     "https://www.youtube.com/watch?v=jNQXAC9IVRw&list=PLxxxxxxxxxxxxxxxxxxxxxx",
			kind:    youtubeVideo,
			videoID: "jNQXAC9IVRw",
			ok:      true,
		},
		{
			name:    "shorts",
			raw:     "https://m.youtube.com/shorts/jNQXAC9IVRw",
			kind:    youtubeVideo,
			videoID: "jNQXAC9IVRw",
			ok:      true,
		},
		{
			name:    "youtu.be",
			raw:     "http://youtu.be/jNQXAC9IVRw",
			kind:    youtubeVideo,
			videoID: "jNQXAC9IVRw",
			ok:      true,
		},
		{
			name:    "music watch",
			raw:     "https://music.youtube.com/watch?v=jNQXAC9IVRw",
			kind:    youtubeVideo,
			videoID: "jNQXAC9IVRw",
			ok:      true,
		},
		{
			name:       "playlist",
			raw:        "https://www.youtube.com/playlist?list=PLabcdefghijklmnop",
			kind:       youtubePlaylist,
			playlistID: "PLabcdefghijklmnop",
			ok:         true,
		},
		{name: "playlist missing list", raw: "https://www.youtube.com/playlist", ok: false},
		{name: "channel", raw: "https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx", ok: false},
		{name: "live", raw: "https://www.youtube.com/live/jNQXAC9IVRw", ok: false},
		{name: "embed", raw: "https://www.youtube.com/embed/jNQXAC9IVRw", ok: false},
		{name: "crunchyroll", raw: "https://www.crunchyroll.com/watch/GE00198973JAJP/dawn", ok: false},
		{name: "junk", raw: "not a url", ok: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseYouTubeURL(test.raw)
			if test.ok {
				if err != nil {
					t.Fatalf("parseYouTubeURL() error = %v", err)
				}
				if got.Kind != test.kind || got.VideoID != test.videoID || got.PlaylistID != test.playlistID {
					t.Fatalf("parseYouTubeURL() = %+v", got)
				}
				return
			}
			if err == nil {
				t.Fatal("parseYouTubeURL() succeeded")
			}
		})
	}
}

func TestClassifyVideoURL(t *testing.T) {
	t.Parallel()

	page := "https://afriforumtv.co.za/user/watch/f2ef181c-25a0-41ed-9618-191603dcf123?episodeId=58781465-1f2c-40dc-8f6f-bc7925082559"
	got, err := classifyVideoURL(page)
	if err != nil {
		t.Fatalf("classifyVideoURL(afriforum) error = %v", err)
	}
	if got.Kind != youtubePage {
		t.Fatalf("classifyVideoURL(afriforum) kind = %v", got.Kind)
	}

	watch, err := classifyVideoURL("https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil || watch.Kind != youtubeVideo || watch.VideoID != "jNQXAC9IVRw" {
		t.Fatalf("classifyVideoURL(youtube) = %+v, %v", watch, err)
	}
	if _, err := classifyVideoURL("https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx"); err == nil {
		t.Fatal("youtube channel was accepted")
	}
	if _, err := classifyVideoURL("https://www.crunchyroll.com/watch/GE00198973JAJP/dawn"); err == nil {
		t.Fatal("crunchyroll page was accepted on the video tab")
	}
	if _, err := classifyVideoURL("not a url"); err == nil {
		t.Fatal("junk URL was accepted")
	}
}

func TestChoosePageMedia(t *testing.T) {
	t.Parallel()

	chosen, err := choosePageMedia([]pageMediaCandidate{
		{URL: "blob:https://example.com/1", FromVideo: true},
		{URL: "https://cdn.example/ad.mp4", File: true},
		{URL: "https://cdn.example/first.m3u8", Manifest: true},
		{URL: "https://cdn.example/main.m3u8", Manifest: true},
	})
	if err != nil || chosen != "https://cdn.example/main.m3u8" {
		t.Fatalf("choosePageMedia(manifest) = %q, %v", chosen, err)
	}

	chosen, err = choosePageMedia([]pageMediaCandidate{
		{URL: "https://cdn.example/main.m3u8", Manifest: true},
		{URL: "https://cdn.example/play.m3u8", FromVideo: true},
	})
	if err != nil || chosen != "https://cdn.example/play.m3u8" {
		t.Fatalf("choosePageMedia(video) = %q, %v", chosen, err)
	}

	if _, err := choosePageMedia(nil); !errors.Is(err, errNoPageVideo) {
		t.Fatalf("empty page error = %v", err)
	}
}

func TestChooseEmbeddedEpisode(t *testing.T) {
	t.Parallel()

	first := "https://cdn.example/bcdn_token=one&expires=1/11111111-1111-1111-1111-111111111111/playlist.m3u8"
	second := "https://cdn.example/bcdn_token=two&expires=1/22222222-2222-2222-2222-222222222222/playlist.m3u8"
	candidates := []pageMediaCandidate{
		{URL: first, Manifest: true},
		{URL: second, Manifest: true},
	}
	embeds := []string{"https://iframe.mediadelivery.net/embed/23748/11111111-1111-1111-1111-111111111111"}
	chosen, err := selectPageMedia(candidates, embeds)
	if err != nil || chosen != first {
		t.Fatalf("selectPageMedia(embed) = %q, %v", chosen, err)
	}
	if pageMediaSettled(candidates, nil) {
		t.Fatal("several playlists settled before the player frame appeared")
	}
	if !pageMediaSettled(candidates, embeds) {
		t.Fatal("player frame did not settle the matching playlist")
	}
	if _, err := selectPageMedia(candidates, nil); !errors.Is(err, errManyPageVideos) {
		t.Fatalf("several playlists error = %v", err)
	}
}

func TestExtractMediaURLs(t *testing.T) {
	t.Parallel()

	body := `{"video":{"url":"https:\/\/cdn.example\/bcdn_token=abc\u0026expires=9\/11111111-1111-1111-1111-111111111111\/playlist.m3u8"}}`
	urls := extractMediaURLs(body)
	if len(urls) != 1 || !strings.Contains(urls[0], "11111111-1111-1111-1111-111111111111/playlist.m3u8") || !strings.Contains(urls[0], "bcdn_token=abc&expires=9") {
		t.Fatalf("extractMediaURLs = %#v", urls)
	}
	if !isPlayerEmbed("https://iframe.mediadelivery.net/embed/23748/11111111-1111-1111-1111-111111111111") {
		t.Fatal("mediadelivery frame was ignored")
	}
	if isPlayerEmbed("https://www.googletagmanager.com/ns.html") {
		t.Fatal("analytics frame was treated as a player")
	}
}

func TestManifestBodyEncrypted(t *testing.T) {
	t.Parallel()

	if !manifestBodyEncrypted(`schemeIdUri="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"`) {
		t.Fatal("widevine manifest was treated as clear")
	}
	if manifestBodyEncrypted("#EXT-X-KEY:METHOD=AES-128,URI=\"https://example/key\"") {
		t.Fatal("AES-128 HLS was treated as DRM")
	}
}

func TestYouTubeCookiesShareOneFile(t *testing.T) {
	t.Parallel()

	watch, err := videoCookiePath("https://www.youtube.com/watch?v=cjH3OB0tfQs")
	if err != nil {
		t.Fatalf("videoCookiePath(watch) error = %v", err)
	}
	short, err := videoCookiePath("https://youtu.be/cjH3OB0tfQs")
	if err != nil {
		t.Fatalf("videoCookiePath(short) error = %v", err)
	}
	mobile, err := videoCookiePath("https://m.youtube.com/watch?v=cjH3OB0tfQs")
	if err != nil {
		t.Fatalf("videoCookiePath(mobile) error = %v", err)
	}
	if watch != short || short != mobile || !strings.HasSuffix(watch, "youtube.com.txt") {
		t.Fatalf("cookie files = %q, %q, %q", watch, short, mobile)
	}
}

func TestAgeConfirmationMessage(t *testing.T) {
	t.Parallel()

	detail := strings.Join([]string{
		"ERROR: [youtube] cjH3OB0tfQs: Sign in to confirm your age. Use --cookies-from-browser or --cookies for the authentication.",
		"WARNING: Your yt-dlp version (2026.08.19) is older than 90 days!",
	}, "\n")
	got := presentVideoError(detail, "https://www.youtube.com/watch?v=cjH3OB0tfQs")
	if strings.Contains(got, "cookies-from-browser") || strings.Contains(got, "90 days") {
		t.Fatalf("presentVideoError() = %q", got)
	}
	if !strings.Contains(got, "Use Sign in, then try again.") {
		t.Fatalf("presentVideoError() = %q", got)
	}
}

func TestUnavailableVideoIsNotALoginError(t *testing.T) {
	t.Parallel()

	detail := strings.Join([]string{
		"ERROR: [youtube] e2sIUGO825w: This video is not available",
		"input = NChallengeInput(player_url='https://www.youtube.com/s/player/base.js', challenges=['abc'])",
		"WARNING: [youtube] e2sIUGO825w: n challenge solving failed: Some formats may be missing.",
	}, "\n")
	got := presentVideoError(detail, "https://www.youtube.com/watch?v=e2sIUGO825w")
	if strings.Contains(got, "Sign in") || strings.Contains(got, "not available") {
		t.Fatalf("presentVideoError() = %q", got)
	}
	if !strings.Contains(got, "Update yt-dlp") {
		t.Fatalf("presentVideoError() = %q", got)
	}

	plain := presentVideoError("ERROR: [youtube] e2sIUGO825w: This video is not available", "https://www.youtube.com/watch?v=e2sIUGO825w")
	if strings.Contains(plain, "Sign in") {
		t.Fatalf("unavailable video asked for sign-in: %q", plain)
	}
}

func TestVideoCookieHost(t *testing.T) {
	t.Parallel()

	host, err := videoCookieHost("https://www.vimeo.com/123")
	if err != nil || host != "vimeo.com" {
		t.Fatalf("videoCookieHost(www) = %q, %v", host, err)
	}
	host, err = videoCookieHost("https://m.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil || host != "m.youtube.com" {
		t.Fatalf("videoCookieHost(subdomain) = %q, %v", host, err)
	}
}

func TestShouldSearchPage(t *testing.T) {
	t.Parallel()

	cert := "ERROR: [generic] watch: Unable to download webpage: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate in certificate chain"
	if !shouldSearchPage(cert) {
		t.Fatal("certificate failure did not start a page search")
	}
	if !shouldSearchPage("ERROR: Unsupported URL: https://example.com/watch") {
		t.Fatal("unsupported URL did not start a page search")
	}
	if shouldSearchPage("ERROR: Private video. Sign in if you've been granted access") {
		t.Fatal("private video started a page search")
	}
}

func TestYtdlpCertEnv(t *testing.T) {
	t.Parallel()

	if got := ytdlpTLSArgs(""); got != nil {
		t.Fatalf("ytdlpTLSArgs(empty) = %v", got)
	}
	if got, want := joinArgs(ytdlpTLSArgs("/tmp/macos-trust.pem")), "--compat-options no-certifi"; got != want {
		t.Fatalf("ytdlpTLSArgs = %q", got)
	}

	env := withCertEnv([]string{"SSL_CERT_FILE=/old", "PATH=/usr/bin"}, "/new/macos-trust.pem")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SSL_CERT_FILE=/old") {
		t.Fatalf("old CA bundle kept: %q", joined)
	}
	if !strings.Contains(joined, "SSL_CERT_FILE=/new/macos-trust.pem") || !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatalf("cert env = %q", joined)
	}
}

func TestTrustedCertBundle(t *testing.T) {
	got := trustedCertBundle()
	if runtime.GOOS != "darwin" {
		if got != "" {
			t.Fatalf("trustedCertBundle() = %q", got)
		}
		return
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("trustedCertBundle() file: %v", err)
	}
	if !strings.Contains(string(body), "BEGIN CERTIFICATE") {
		t.Fatal("macOS trust bundle has no certificates")
	}
}

func TestIsYouTubeHost(t *testing.T) {
	t.Parallel()

	if !isYouTubeHost("https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx") {
		t.Fatal("channel host should be recognized")
	}
	if isYouTubeHost("https://www.crunchyroll.com/watch/GE00198973JAJP/dawn") {
		t.Fatal("crunchyroll host should be rejected")
	}
}

func TestYouTubeFormatSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		quality string
		want    string
	}{
		{quality: "1080p", want: "bv*[height<=1080]+ba/b[height<=1080]/b"},
		{quality: "720p", want: "bv*[height<=720]+ba/b[height<=720]/b"},
		{quality: "480p", want: "bv*[height<=480]+ba/b[height<=480]/b"},
		{quality: "360p", want: "bv*[height<=360]+ba/b[height<=360]/b"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.quality, func(t *testing.T) {
			t.Parallel()
			got, err := youtubeFormatSelector(test.quality)
			if err != nil {
				t.Fatalf("youtubeFormatSelector() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("youtubeFormatSelector() = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := youtubeFormatSelector("best"); err == nil {
		t.Fatal("youtubeFormatSelector(best) succeeded")
	}
}

func TestYouTubeCommandArgs(t *testing.T) {
	t.Parallel()

	lookup := youtubeLookupArgs("https://youtu.be/jNQXAC9IVRw", false)
	if got, want := joinArgs(lookup), "--dump-single-json --no-download --js-runtimes deno --no-playlist https://youtu.be/jNQXAC9IVRw"; got != want {
		t.Fatalf("video lookup = %q", got)
	}

	playlist := youtubeLookupArgs("https://www.youtube.com/playlist?list=PLtest", true)
	if got, want := joinArgs(playlist), "--dump-single-json --no-download --js-runtimes deno --flat-playlist https://www.youtube.com/playlist?list=PLtest"; got != want {
		t.Fatalf("playlist lookup = %q", got)
	}

	download, err := youtubeDownloadArgs("jNQXAC9IVRw", "1080p", "Me at the zoo [1080p].mp4")
	if err != nil {
		t.Fatalf("youtubeDownloadArgs() error = %v", err)
	}
	want := "--no-playlist --js-runtimes deno --newline -f bv*[height<=1080]+ba/b[height<=1080]/b --merge-output-format mp4 --remux-video mp4 -o Me at the zoo [1080p].mp4 https://www.youtube.com/watch?v=jNQXAC9IVRw"
	if got := joinArgs(download); got != want {
		t.Fatalf("download = %q", got)
	}
}

func TestMediaDownloadKeepsOneURL(t *testing.T) {
	t.Parallel()

	page := "https://afriforumtv.co.za/user/watch/f2ef181c-25a0-41ed-9618-191603dcf123?episodeId=58781465-1f2c-40dc-8f6f-bc7925082559"
	media := "https://cdn.example/bcdn_token=abc&token_path=%2F&expires=1/11111111-1111-1111-1111-111111111111/playlist.m3u8"
	args, err := mediaDownloadArgs(media, page, "720p", "AfriForumTV [720p].mp4")
	if err != nil {
		t.Fatalf("mediaDownloadArgs() error = %v", err)
	}
	if got := positionalURLs(args); len(got) != 1 || got[0] != media {
		t.Fatalf("download URLs = %#v", got)
	}
	referer := flagValue(args, "--referer")
	if referer != page {
		t.Fatalf("referer = %q", referer)
	}
	if cookies := flagValue(args, "--cookies"); cookies != "" {
		cookieAt := indexOf(args, "--cookies")
		if cookieAt < 0 || cookieAt+2 >= len(args) || args[cookieAt+2] != media {
			t.Fatalf("cookies were not placed before the media URL: %q", joinArgs(args))
		}
	}
}

func TestCookieArgDoesNotStealReferer(t *testing.T) {
	t.Parallel()

	args := withCookieFile([]string{"--referer", "https://example.com/watch"}, "/tmp/cookies.txt")
	if got, want := joinArgs(args), "--referer https://example.com/watch --cookies /tmp/cookies.txt"; got != want {
		t.Fatalf("referer cookies = %q", got)
	}
	args = withCookieFile([]string{"-o", "out.mp4", "https://cdn.example/playlist.m3u8"}, "/tmp/cookies.txt")
	if got, want := joinArgs(args), "-o out.mp4 --cookies /tmp/cookies.txt https://cdn.example/playlist.m3u8"; got != want {
		t.Fatalf("download cookies = %q", got)
	}
}

func TestParseYouTubeDump(t *testing.T) {
	t.Parallel()

	video, err := parseYouTubeDump([]byte(`{"id":"jNQXAC9IVRw","title":"Me at the zoo"}`), youtubeVideo)
	if err != nil {
		t.Fatalf("parseYouTubeDump(video) error = %v", err)
	}
	if video.Title != "Me at the zoo" || len(video.Entries) != 1 || video.Entries[0].ID != "jNQXAC9IVRw" {
		t.Fatalf("video dump = %+v", video)
	}

	playlist, err := parseYouTubeDump([]byte(`{
		"id": "PLtest",
		"title": "Favorites",
		"entries": [
			{"id": "abcdefghijk", "title": "One", "playlist_index": 1},
			null,
			{"id": "bcdefghijkl", "title": "", "playlist_index": 3}
		]
	}`), youtubePlaylist)
	if err != nil {
		t.Fatalf("parseYouTubeDump(playlist) error = %v", err)
	}
	if playlist.ID != "PLtest" || playlist.Title != "Favorites" || len(playlist.Entries) != 2 {
		t.Fatalf("playlist dump = %+v", playlist)
	}
	if playlist.Entries[1].Index != 3 || playlist.Entries[1].ID != "bcdefghijkl" {
		t.Fatalf("second entry = %+v", playlist.Entries[1])
	}

	if _, err := parseYouTubeDump([]byte(`{"id":"PLempty","title":"Empty","entries":[null]}`), youtubePlaylist); err == nil {
		t.Fatal("empty playlist succeeded")
	}
}

func TestYouTubeJobs(t *testing.T) {
	t.Parallel()

	video, err := youtubeJobs("out", youtubeLookup{
		Kind:  youtubeVideo,
		ID:    "jNQXAC9IVRw",
		Title: "Me: at the zoo?",
		Entries: []youtubeEntry{{
			ID:    "jNQXAC9IVRw",
			Title: "Me: at the zoo?",
		}},
	}, nil, "1080p")
	if err != nil {
		t.Fatalf("youtubeJobs(video) error = %v", err)
	}
	wantVideo := filepath.Join("out", "Me_ at the zoo_ [1080p].mp4")
	if len(video) != 1 || video[0].Output != wantVideo {
		t.Fatalf("video jobs = %+v", video)
	}

	playlist, err := youtubeJobs("out", youtubeLookup{
		Kind:  youtubePlaylist,
		ID:    "PLtest",
		Title: "Favorites",
		Entries: []youtubeEntry{
			{ID: "abcdefghijk", Title: "One", Index: 1},
			{ID: "bcdefghijkl", Title: "", Index: 3},
		},
	}, []string{"bcdefghijkl"}, "720p")
	if err != nil {
		t.Fatalf("youtubeJobs(playlist) error = %v", err)
	}
	wantPlaylist := filepath.Join("out", "Favorites", "03 bcdefghijkl [720p].mp4")
	if len(playlist) != 1 || playlist[0].Output != wantPlaylist || playlist[0].VideoID != "bcdefghijkl" {
		t.Fatalf("playlist jobs = %+v", playlist)
	}

	unnamed, err := youtubeJobs("out", youtubeLookup{
		Kind: youtubePlaylist,
		ID:   "PLtest",
		Entries: []youtubeEntry{
			{ID: "abcdefghijk", Title: "One", Index: 1},
		},
	}, []string{"abcdefghijk"}, "360p")
	if err != nil {
		t.Fatalf("youtubeJobs(unnamed) error = %v", err)
	}
	wantUnnamed := filepath.Join("out", "PLtest", "01 One [360p].mp4")
	if unnamed[0].Output != wantUnnamed {
		t.Fatalf("unnamed playlist = %q", unnamed[0].Output)
	}

	if _, err := youtubeJobs("out", youtubeLookup{Kind: youtubePlaylist}, nil, "1080p"); err == nil {
		t.Fatal("empty selection succeeded")
	}
}

func TestYouTubeProgress(t *testing.T) {
	t.Parallel()

	chunk := "[download]   1.6% of  190.70KiB\r[download]  33.0% of  190.70KiB\r"
	got, ok := youtubeProgressPercent(chunk)
	if !ok || got != 33 {
		t.Fatalf("youtubeProgressPercent() = %v, %v", got, ok)
	}
	if _, ok := youtubeProgressPercent("merging formats"); ok {
		t.Fatal("non-progress line matched")
	}

	overall := youtubeOverallProgress(1, 4, 50)
	if overall != 0.375 {
		t.Fatalf("youtubeOverallProgress() = %v", overall)
	}
	if got := youtubeOverallProgress(0, 0, 10); got != 0 {
		t.Fatalf("empty overall = %v", got)
	}
}

func TestValidateYouTubeToolVersion(t *testing.T) {
	t.Parallel()

	if err := validateYouTubeToolVersion("2026.11.23"); err != nil {
		t.Fatalf("November release error = %v", err)
	}
	if err := validateYouTubeToolVersion("2026.11.23\n"); err != nil {
		t.Fatalf("unpadded release error = %v", err)
	}
	if err := validateYouTubeToolVersion("2026.12.01"); err != nil {
		t.Fatalf("newer release error = %v", err)
	}
	if err := validateYouTubeToolVersion("2026.08.19"); err == nil {
		t.Fatal("August release was accepted")
	}
	if err := validateYouTubeToolVersion("not-a-version"); err == nil {
		t.Fatal("garbage version was accepted")
	}
}

func TestURLsRequireCrunchyrollAuth(t *testing.T) {
	t.Parallel()

	youtube := "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	crunchyroll := "https://www.crunchyroll.com/watch/GE00198973JAJP/dawn"
	if urlsRequireCrunchyrollAuth([]string{youtube}) {
		t.Fatal("YouTube-only batch required a cookie")
	}
	if !urlsRequireCrunchyrollAuth([]string{youtube, crunchyroll}) {
		t.Fatal("mixed batch skipped the cookie")
	}
	if !urlsRequireCrunchyrollAuth([]string{crunchyroll}) {
		t.Fatal("Crunchyroll batch skipped the cookie")
	}
	if urlsRequireCrunchyrollAuth([]string{"not a url"}) {
		t.Fatal("junk URL required a cookie")
	}
}

func TestSearchPageFindsPlayerPlaylist(t *testing.T) {
	if os.Getenv("PAGE_SEARCH_LIVE") != "1" {
		t.Skip("set PAGE_SEARCH_LIVE=1 to search a signed-in page")
	}
	lookup, err := searchPageForVideo(t.Context(), "https://afriforumtv.co.za/user/watch/f2ef181c-25a0-41ed-9618-191603dcf123?episodeId=58781465-1f2c-40dc-8f6f-bc7925082559", io.Discard)
	if err != nil {
		t.Fatalf("searchPageForVideo error = %v", err)
	}
	if len(lookup.Entries) != 1 {
		t.Fatalf("entries = %d", len(lookup.Entries))
	}
	media := lookup.Entries[0].MediaURL
	if !strings.Contains(media, "7265054a-5d0c-42bb-9ed7-4aaa3c728367/playlist.m3u8") {
		t.Fatal("page search did not choose this episode's playlist")
	}
}

func positionalURLs(args []string) []string {
	var urls []string
	for i := 0; i < len(args); i++ {
		if takesArg(args[i]) {
			i++
			continue
		}
		if strings.Contains(args[i], "://") {
			urls = append(urls, args[i])
		}
	}
	return urls
}

func flagValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func indexOf(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

func takesArg(arg string) bool {
	switch arg {
	case "--referer", "--cookies", "-o", "--output", "-f", "--format", "--js-runtimes", "--merge-output-format", "--remux-video", "--compat-options":
		return true
	default:
		return false
	}
}

func joinArgs(args []string) string {
	return joinWords(args)
}

func joinWords(args []string) string {
	out := ""
	for i, arg := range args {
		if i > 0 {
			out += " "
		}
		out += arg
	}
	return out
}
