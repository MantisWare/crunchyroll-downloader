# YouTube download design

YouTube is a third job type beside Crunchyroll download and local convert. A dedicated tab and the CLI accept a video or playlist link, let the user pick a video height, and save an MP4. `yt-dlp` extracts and downloads. This app classifies the URL, applies the quality ceiling, chooses files, and reports progress.

## Decisions

- The New tab offers YouTube beside Download and Convert.
- A watch, Shorts, or `youtu.be` link is one video. A `list` query on those links stays part of that one video.
- A `/playlist` link is a playlist. The tab shows a checklist. The CLI downloads every entry.
- The quality choices are `1080p`, `720p`, `480p`, and `360p`. The saved video quality is the same preference the Crunchyroll tab already stores. The menu starts on that saved value, or `1080p` when it is empty.
- Output is one MP4 per video: the best video at or below the chosen height, plus the best audio, merged and remuxed.
- Language, subtitle, season, dub, audio-quality, and Crunchyroll sign-in controls are absent from this job.
- `-url` and every line of `-urls` accept a YouTube link. A YouTube-only run does not require `etp_rt`. A batch that also contains a Crunchyroll link still requires it. Crunchyroll-only flags are unused for a YouTube link and do not cause an error.

## Tools

Lookup and download require `yt-dlp`, `deno`, and `ffmpeg` on `PATH`.

`yt-dlp --version` must be 19 August 2026 or newer. Compare the numeric year, month, and day, so `2026.08.19` and `2026.8.19` are the same release. The `2026.07.04` build answers YouTube with HTTP 403, so the app rejects an older build before it calls YouTube and tells the user to update `yt-dlp`.

Every `yt-dlp` invocation passes `--js-runtimes deno`.

## URL classification

Accepted hosts are `youtube.com`, `www.youtube.com`, `m.youtube.com`, `music.youtube.com`, and `youtu.be`, over `http` or `https`.

| Input | Kind |
| --- | --- |
| `/watch` with an 11-character `v` id | video |
| `/shorts/` plus an 11-character id | video |
| `youtu.be/` plus an 11-character id | video |
| `/playlist` with a non-empty `list` id | playlist |

A video id matches `[A-Za-z0-9_-]{11}`. Any other URL, including a Crunchyroll URL, is unrecognized and fails before `yt-dlp` runs. Channel, live, and embed URLs are unrecognized.

## Lookup

The tab starts with a URL field, **Lookup**, the shared download folder, and the prompt to paste a YouTube link. Quality, the checklist, and **Download** stay hidden until lookup succeeds.

A video lookup runs:

```text
yt-dlp --dump-single-json --no-download --no-playlist --js-runtimes deno <url>
```

The tab shows the video title and enables **Download**. After lookup, the tab title becomes that title, truncated the same way other tabs truncate titles.

A playlist lookup runs:

```text
yt-dlp --flat-playlist --dump-single-json --js-runtimes deno <url>
```

The tab shows a checklist of entry titles, all selected, with **Select all** and **Select none**. The download button label is **Download selected** and stays disabled until at least one entry is checked. The tab title becomes the playlist title.

## Download

Each selected video is a separate `yt-dlp` process. A playlist selection is downloaded as `https://www.youtube.com/watch?v=<id>` so the checklist is the whole request. The CLI playlist path does the same for every entry returned by lookup.

```text
yt-dlp --no-playlist --js-runtimes deno --newline \
  -f "bv*[height<=HEIGHT]+ba/b[height<=HEIGHT]/b" \
  --merge-output-format mp4 --remux-video mp4 \
  -o <destination> \
  https://www.youtube.com/watch?v=<id>
```

`HEIGHT` is the numeric part of the quality label: `1080`, `720`, `480`, or `360`.

Filenames use the existing `sanitizeFilename` rules. A single video is saved in the download folder as:

```text
Title [1080p].mp4
```

A playlist creates a folder named with the playlist title. Each file uses that entry's playlist index, including entries the user left unchecked, padded to the width of the playlist length and at least two digits:

```text
Playlist Title/01 Video Title [1080p].mp4
```

An empty title falls back to the video id. An empty playlist title falls back to the playlist id. Videos run one at a time. A destination MP4 that already exists and has size greater than zero is skipped. When that finished file is missing, a sibling `.part` file is deleted and the video downloads again.

**Stop** kills the running `yt-dlp` process, deletes the partial MP4 and any `.part` files for that output, and leaves completed files in place. The status reads `Stopping…`. Closing a busy tab uses the existing stop-and-close confirmation.

Progress reads the percentage from `yt-dlp` `--newline` output. Each selected video occupies an equal slice of the bar, in order. The log receives the same lines.

Changing the quality menu writes the shared video-quality preference. Changing the folder writes the shared output directory.

**Reset** clears the URL, checklist, log, and progress, and restores the numbered YouTube tab title.

## CLI

`processUrl` classifies a YouTube link before the Crunchyroll parser. A YouTube-only `-url` or `-urls` invocation skips the `etp_rt` requirement. When a batch contains both kinds, the cookie check still runs, and each line then follows its own path.

A YouTube playlist on the CLI downloads every entry at `-video-quality`. There is no checklist. Output uses the configured output directory.

## Errors

- A missing `yt-dlp`, `deno`, or `ffmpeg`, or a `yt-dlp` older than 19 August 2026, stops the run with a message that names the tool to install or update.
- An unrecognized URL fails in the parser.
- A private, deleted, age-restricted, or region-blocked video records `yt-dlp`'s error in the log. There is no YouTube sign-in.
- When the chosen height is unavailable, the format string already selects the next lower video and the best audio. If `yt-dlp` still exits non-zero, that video fails.
- On a playlist, the log records the failed entry and the remaining entries continue.
- `yt-dlp` keeps its own connection retries.

## Tests

`go test` does not call YouTube. It covers:

- URL classification for watch, Shorts, `youtu.be`, playlist, a watch URL that also has `list`, a Crunchyroll URL, and junk.
- The format string for each of the four quality labels.
- Single-video and playlist filenames, including sanitization, index padding, and empty-title fallback.
- Progress-line parsing.
- Parsing saved `yt-dlp` JSON fixtures for one video and one flat playlist.

A manual pass downloads one video and one playlist from the tab, then the same two links with `-url`.

## Out of scope

YouTube cookies or sign-in, subtitles, audio-only downloads, channel URLs, live streams, embed URLs, and SponsorBlock.
