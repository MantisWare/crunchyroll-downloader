# YouTube Download Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a YouTube tab and CLI path that downloads one video or a playlist to MP4 through yt-dlp.

**Architecture:** Shared YouTube code classifies URLs, builds yt-dlp arguments, names files, and runs downloads. The CLI routes YouTube links before the Crunchyroll parser and skips `etp_rt` when every target is YouTube. The GUI adds a YouTube session from the New tab.

**Tech Stack:** Go 1.25, Fyne v2, yt-dlp, Deno, FFmpeg.

## Global Constraints

- Accepted hosts: `youtube.com`, `www.youtube.com`, `m.youtube.com`, `music.youtube.com`, `youtu.be` over `http` or `https`.
- Watch, Shorts, and `youtu.be` are one video, even with a `list` query. `/playlist` with a non-empty `list` is a playlist.
- Video ids match `[A-Za-z0-9_-]{11}`. Channel, live, and embed URLs are unrecognized.
- Qualities are `1080p`, `720p`, `480p`, `360p`. Format string is `bv*[height<=HEIGHT]+ba/b[height<=HEIGHT]/b`.
- Output is MP4. Single file: `Title [1080p].mp4`. Playlist file: `Playlist Title/01 Video Title [1080p].mp4`, using the playlist index padded to the playlist length and at least two digits.
- Filenames use `sanitizeFilename`. Empty titles fall back to the video id. Empty playlist titles fall back to the playlist id.
- Skip a destination MP4 only when it exists and its size is greater than zero. Videos run one at a time.
- Every yt-dlp invocation passes `--js-runtimes deno`. Downloads also pass `--no-playlist`, `--newline`, `--merge-output-format mp4`, and `--remux-video mp4`.
- Require `yt-dlp`, `deno`, and `ffmpeg` on `PATH`. Accept yt-dlp dated 19 August 2026 or newer (`2026.08.19` and `2026.8.19` match).
- A YouTube-only CLI run does not require `etp_rt`. A batch that also contains a Crunchyroll URL still does.
- No YouTube sign-in, subtitles, audio-only, channel, live, or embed support.
- `go test` does not call YouTube.
- Do not commit. The user handles git.

---

### Task 1: Pure YouTube rules

**Files:**
- Create: `youtube.go`
- Test: `youtube_test.go`

**Interfaces:**
- Produces: `parseYouTubeURL`, `isYouTubeHost`, `youtubeFormatSelector`, `youtubeLookupArgs`, `youtubeDownloadArgs`, `parseYouTubeDump`, `youtubeJobs`, `youtubeProgressPercent`, `youtubeOverallProgress`, `validateYouTubeToolVersion`, `urlsRequireCrunchyrollAuth`

- [ ] Write failing tests for classification, format strings, dump parsing, filenames, progress, version, and Crunchyroll auth detection.
- [ ] Run `go test -count=1 -run 'YouTube|URLsRequireCrunchyrollAuth' .` and confirm failure because the functions are missing.
- [ ] Implement the pure functions in `youtube.go`.
- [ ] Re-run the same test command and confirm pass.

### Task 2: yt-dlp execution

**Files:**
- Modify: `youtube.go`

**Interfaces:**
- Consumes: Task 1 builders and parsers
- Produces: `ensureYouTubeTools() error`, `lookupYouTube(ctx, raw, log) (youtubeLookup, error)`, `downloadYouTubeURL(env, raw) error`

- [ ] `ensureYouTubeTools` checks the three binaries and the version floor.
- [ ] `lookupYouTube` runs the lookup arguments, keeps stderr out of the JSON buffer, and parses stdout.
- [ ] `downloadYouTubeURL` looks up the link, selects every playlist entry, and downloads jobs one at a time.
- [ ] Cancellation returns `errCancelled`, deletes the in-progress MP4 and its `.part` file, and leaves earlier finished files.
- [ ] A failed playlist entry is logged and the rest continue. Any failure returns an error after the loop.

### Task 3: CLI routing

**Files:**
- Modify: `main.go`

- [ ] Collect `-url` / `-urls` before the cookie check.
- [ ] Require Crunchyroll auth only for `-browser-login` or a batch that contains a Crunchyroll URL.
- [ ] `processUrl` handles a YouTube host before `parseContentURL`.
- [ ] Help text documents YouTube, yt-dlp, and Deno.

### Task 4: YouTube tab

**Files:**
- Modify: `tab_state.go`, `tab_state_test.go`, `gui.go`, `gui_tabs.go`
- Create: `gui_youtube.go`

- [ ] Add `tabKindYouTube` with titles `YouTube` and `YouTube 2`.
- [ ] New tab offers Download, YouTube, and Convert.
- [ ] The session has URL, Lookup, shared folder, quality, checklist, log, progress, Download, Stop, and Reset.
- [ ] Quality and the checklist stay hidden until lookup. A playlist uses **Download selected**. A video uses **Download**.
- [ ] Quality and folder write the shared preferences. Stop cancels the session context. Reset clears the tab.

### Task 5: Docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`

- [ ] Document the YouTube tab, CLI behavior, and required tools.

### Task 6: Verification

- [ ] `go test -count=1 .`
- [ ] `go test -tags gui -count=1 .` to compile the GUI package. Do not run `build_UI.sh`.
