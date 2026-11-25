# Video download design

The YouTube job becomes the Video job. One tab and the CLI accept a YouTube link or any other `http` or `https` page link, let the user pick a video height, and save an MP4. `yt-dlp` extracts and downloads when it knows the site. When `yt-dlp` reports `Unsupported URL`, the app loads that page in a browser and downloads the video it finds there. This app classifies the URL, applies the quality ceiling, chooses files, stores an optional per-site sign-in, and reports progress.

The AfriForum TV watch link is one of those pages. Its HTML is only the site shell, so the video is found after the page scripts run. There is no separate AfriForum API client.

## Decisions

- The New tab offers **Video** beside Download and Convert. The tab's numbered title is `Video`, then `Video 2`, and so on. After a successful lookup the title becomes the video or playlist title, truncated the same way other tabs truncate titles. **Reset** restores the numbered title.
- The URL field placeholder is `https://... video or playlist link`.
- A Crunchyroll link is rejected on the Video tab before `yt-dlp` runs. On the CLI it stays on the Crunchyroll download.
- A YouTube watch, Shorts, or `youtu.be` link is one video. A `list` query on those links stays part of that one video. A YouTube `/playlist` link is a playlist. A YouTube channel, live, or embed link is still unrecognized and fails before `yt-dlp`.
- Any other `http` or `https` link uses Video. Lookup asks `yt-dlp` first. A JSON result whose `entries` value is an array is a playlist, including an empty array. A result with no `entries` array is one video. When `yt-dlp` exits with `Unsupported URL`, lookup loads the page and searches it for one video.
- The tab shows a checklist for a playlist. The CLI downloads every entry.
- The quality choices are `1080p`, `720p`, `480p`, and `360p`. The saved video quality is the same preference the Crunchyroll tab already stores. The menu starts on that saved value, or `1080p` when it is empty. Changing the menu writes that shared preference. Changing the folder writes the shared output directory.
- Output is one MP4 per video: the best video at or below the chosen height, plus the best audio, merged and remuxed.
- Language, subtitle, season, dub, and audio-quality controls are absent from this job.
- **Sign in** on the Video tab is optional. Public videos lookup and download with no session. The Crunchyroll **Sign in with browser** button is unchanged and still only captures the Crunchyroll session.
- `-url` and every line of `-urls` accept a Video link. A Video-only run does not require `etp_rt`. A batch that also contains a Crunchyroll link still requires it. Crunchyroll-only flags are unused for a Video link and do not cause an error.
- CLI help, the `-url` flag description, and the README say Video, not YouTube.

## Tools

Lookup and download require `yt-dlp`, `deno`, and `ffmpeg` on `PATH`.

`yt-dlp --version` must be 19 August 2026 or newer. Compare the numeric year, month, and day, so `2026.08.19` and `2026.8.19` are the same release. The `2026.07.04` build answers YouTube with HTTP 403, so the app rejects an older build before it calls any site and tells the user to update `yt-dlp`.

Every `yt-dlp` invocation passes `--js-runtimes deno`. When a cookie file exists for the host being contacted, that invocation also passes `--cookies` with that file.

Page search uses the same Chromium-family browser as sign-in. That browser is required only after `yt-dlp` reports `Unsupported URL`.

## URL classification

YouTube hosts stay `youtube.com`, `www.youtube.com`, `m.youtube.com`, `music.youtube.com`, and `youtu.be`, over `http` or `https`.

| Input | Kind |
| --- | --- |
| YouTube `/watch` with an 11-character `v` id | video |
| YouTube `/shorts/` plus an 11-character id | video |
| `youtu.be/` plus an 11-character id | video |
| YouTube `/playlist` with a non-empty `list` id | playlist |
| YouTube channel, live, or embed | unrecognized, fail before `yt-dlp` |
| Any other `http` or `https` link | ask `yt-dlp`, then search the page if it reports `Unsupported URL` |
| A Crunchyroll link on the Video tab | fail before `yt-dlp` |
| Anything that is not `http` or `https` | fail before `yt-dlp` |

A YouTube video id matches `[A-Za-z0-9_-]{11}`.

For a non-YouTube link, lookup runs with `--flat-playlist`. If `yt-dlp` returns JSON, an `entries` array makes the link a playlist. `null` entries are skipped. An entry with an empty id is skipped. After that, a playlist with no videos fails with `playlist has no videos`. If the JSON has no `entries` array, the link is one video and must include an id. A page-search result does not need a `yt-dlp` id. Its title comes from the page, and its download URL is the media URL chosen below.

## Lookup

The tab starts with a URL field, **Lookup**, **Sign in**, the shared download folder, and the prompt to paste a video link. Quality, the checklist, and **Download** stay hidden until lookup succeeds. **Sign out** is visible and disabled until the current URL's host has a cookie file.

A YouTube video lookup runs:

```text
yt-dlp --dump-single-json --no-download --no-playlist --js-runtimes deno <url>
```

The tab shows the video title and enables **Download**.

A YouTube playlist lookup runs:

```text
yt-dlp --flat-playlist --dump-single-json --js-runtimes deno <url>
```

Any other `http` or `https` lookup uses the same flat-playlist command. The app then reads `entries` as described above.

A playlist shows a checklist of entry titles, all selected, with **Select all** and **Select none**. The download button label is **Download selected** and stays disabled until at least one entry is checked. A single video shows its title and enables **Download**.

Each playlist entry keeps its id, title, playlist index, and, when `yt-dlp` provides one, an absolute entry URL. The absolute URL is `webpage_url` when that value is `http` or `https`. Otherwise it is `url` when that value is `http` or `https`.

When a cookie file exists for the pasted URL's host, lookup adds `--cookies` for that file.

## Page search

Page search runs only when all of these are true:

- The link is not a classified YouTube link.
- `yt-dlp` exited non-zero.
- The `yt-dlp` error text contains `Unsupported URL`.

Any other `yt-dlp` error is the final lookup result. A YouTube link never opens the page-search browser.

The search uses the same Chromium-family browser as sign-in, headless, with a temporary profile. When a cookie file exists for the page host, those cookies are loaded before navigation. The window is not shown. The status reads `Looking for a video on the page…`.

The browser opens the pasted URL, then waits until a candidate has been seen and no new candidate arrives for 2 seconds, or until 20 seconds have passed.

Candidates are absolute `http` or `https` URLs from:

- `video.currentSrc`, `video.src`, and `source.src`
- requests whose path ends in `.m3u8`, `.mpd`, `.mp4`, or `.webm`
- requests whose content type is `application/vnd.apple.mpegurl`, `application/x-mpegURL`, `application/dash+xml`, `video/mp4`, or `video/webm`

`blob:` URLs are ignored. One page produces one video and no checklist. The title is `document.title`, trimmed, or the host name when the title is empty.

The chosen URL is the first match in this order:

1. A `video` current source that is an absolute `http` or `https` URL.
2. The last manifest requested (`.m3u8` or `.mpd`).
3. The last progressive file requested (`.mp4` or `.webm`).

Before download, a chosen `.m3u8` or `.mpd` is fetched with the page URL as the referer and the host's cookies when they exist. If that manifest signals Widevine, PlayReady, or FairPlay, lookup fails with `This video is encrypted` and nothing is downloaded. A plain AES-128 HLS key is not treated as that encryption. No decryption is added for any site other than the existing Crunchyroll path.

If no candidate remains, lookup fails with `No video found on this page.` The `yt-dlp` error is included in the log. When the host has no cookie file, the log also adds: `If this site requires a login, use Sign in and try again.`

**Stop** during the search closes the headless browser and leaves cookie files in place.

## Download

Each selected video is a separate `yt-dlp` process. Videos run one at a time.

A YouTube video is downloaded as `https://www.youtube.com/watch?v=<id>` with `--no-playlist`, including an entry chosen from a playlist. A video found by page search is downloaded from the chosen media URL, with `--no-playlist` and `--referer` set to the pasted page URL. A single non-YouTube video found by `yt-dlp` is downloaded from the URL the user pasted, with `--no-playlist`. A non-YouTube playlist entry is downloaded from its absolute entry URL, with `--no-playlist`. An entry with no absolute URL fails on its own, and the remaining entries continue.

```text
yt-dlp --no-playlist --js-runtimes deno --newline \
  -f "bv*[height<=HEIGHT]+ba/b[height<=HEIGHT]/b" \
  --merge-output-format mp4 --remux-video mp4 \
  -o <destination> \
  <url>
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

An empty title falls back to the video id. An empty playlist title falls back to the playlist id. A destination MP4 that already exists and has size greater than zero is skipped. When that finished file is missing, a sibling `.part` file is deleted and the video downloads again.

**Stop** kills the running `yt-dlp` process, deletes the partial MP4 and any `.part` files for that output, and leaves completed files in place. The status reads `Stopping…`. Closing a busy tab uses the existing stop-and-close confirmation.

Progress reads the percentage from `yt-dlp` `--newline` output. Each selected video occupies an equal slice of the bar, in order. The log receives the same lines.

**Reset** clears the URL, checklist, log, and progress, and restores the numbered Video tab title. It does not delete cookie files.

When a cookie file exists for the host of the URL being downloaded, the download adds `--cookies` for that file. A playlist entry on a different host uses the cookie file for the entry URL's host.

## Sign-in

**Sign in** is enabled when the URL field holds an `http` or `https` link that is not Crunchyroll. It opens Chrome, Edge, Brave, or Chromium, whichever the Crunchyroll login already finds, with an isolated temporary profile, at the pasted URL. The window stays open until the user finishes or the 10-minute timeout fires. The app never receives the password, 2FA code, or CAPTCHA response.

In the tab, **Done signing in** writes the cookies and closes the window. On the CLI, the user presses Enter. Closing the window before that, cancelling, or reaching the timeout leaves any existing cookie file in place and deletes the temporary profile.

On success, every cookie in that profile is written as a Netscape `cookies.txt` file. On macOS and Linux the file mode is `0600`. Cookie values are not written to the log or to `config.json`. The file is:

```text
~/.crunchyroll.config/cookies/<host>.txt
```

`<host>` is the URL hostname, lowercased, with one leading `www.` removed. `www.vimeo.com` and `vimeo.com` share `vimeo.com.txt`. A different subdomain, such as `m.youtube.com` beside `youtube.com`, has its own file. Signing in again for the same host replaces the file. The temporary profile is then deleted.

**Done** or Enter with no cookies for that host reports an error and does not create or replace the file.

The tab status reads `Signed in to <host>` when that file exists, and `Not signed in` otherwise. **Sign out** is enabled only when the file exists. It deletes that file and leaves every other host's file in place.

`-browser-login` with no URL, or with a Crunchyroll URL, keeps today's Crunchyroll login. `-browser-login` with `-url` set to a Video link runs Video sign-in for that URL's host and then continues into the download. `-browser-login` with `-urls` does not open a browser. Each line in the file uses a cookie file that was saved earlier. A Video sign-in does not write `etp_rt`.

## CLI

`processUrl` sends a YouTube link and any other non-Crunchyroll `http` or `https` link down the Video path before the Crunchyroll parser. A Video-only `-url` or `-urls` invocation skips the `etp_rt` requirement. When a batch contains both kinds, the Crunchyroll cookie check still runs, and each line then follows its own path.

A playlist on the CLI downloads every entry at `-video-quality`. There is no checklist. Output uses the configured output directory.

## Errors

- A missing `yt-dlp`, `deno`, or `ffmpeg`, or a `yt-dlp` older than 19 August 2026, stops the run with a message that names the tool to install or update.
- A Crunchyroll URL on the Video tab fails before `yt-dlp`. On the CLI it uses the Crunchyroll download.
- A value that is not an `http` or `https` URL fails in the parser.
- A YouTube channel, live, or embed URL fails with the existing unrecognized-YouTube error.
- `Unsupported URL` from `yt-dlp` on a non-YouTube link starts page search. Other `yt-dlp` errors, including a private, deleted, age-restricted, or region-blocked video, are final and are written to the log. When that host has no cookie file, the log adds: `If this site requires a login, use Sign in and try again.`
- Page search with no candidate fails with `No video found on this page.` An encrypted manifest fails with `This video is encrypted` and is not downloaded.
- Page search with no Chromium-family browser fails with a message that names the browser, and includes the `yt-dlp` error.
- When the chosen height is unavailable, the format string already selects the next lower video and the best audio. If `yt-dlp` still exits non-zero, that video fails.
- On a playlist, the log records the failed entry and the remaining entries continue.
- No Chromium-family browser, the 10-minute timeout, or closing the window before **Done** or Enter leaves the existing cookie file untouched.
- **Done** or Enter with no cookies for that host reports an error and does not replace an existing file.
- `yt-dlp` keeps its own connection retries.

## Tests

`go test` does not open a browser and does not call `yt-dlp`. It covers:

- The existing YouTube classifications: watch, Shorts, `youtu.be`, playlist, a watch URL that also has `list`, channel, live, embed, a Crunchyroll URL, and junk.
- A non-YouTube `https` URL taking the Video path, and a non-`http` URL being rejected.
- Saved JSON fixtures: an `entries` array is a playlist, no `entries` array is one video, `null` entries are skipped, and a playlist that ends empty fails.
- YouTube ids become `https://www.youtube.com/watch?v=<id>`. A YouTube entry does not need `webpage_url` or `url`. For a non-YouTube playlist entry, an absolute `webpage_url` is kept. An absolute `url` is kept when `webpage_url` is absent. An entry with neither fails that entry.
- Cookie-file names: a leading `www.` is removed, and a different subdomain stays separate.
- A Netscape cookie line written from a fixture cookie, with no live browser.
- The format string for each of the four quality labels.
- Single-video and playlist filenames, including sanitization, index padding, and empty-title fallback.
- Progress-line parsing.
- Page-search selection from a fixture list: a `blob:` URL is ignored, a `video` current source wins, otherwise the last manifest wins over a progressive file, and no candidate fails with `No video found on this page.`
- A manifest fixture that signals Widevine fails with `This video is encrypted` and produces no download URL.
- `Unsupported URL` on a non-YouTube link is the only `yt-dlp` failure that requests page search. A YouTube link and any other `yt-dlp` error do not.
- `etp_rt` is required for a Crunchyroll URL and for a batch that mixes Crunchyroll with a Video URL. It is not required for a YouTube URL or for another `https` URL alone.

A manual pass downloads one YouTube video, one YouTube playlist, one public non-YouTube video, and one non-YouTube playlist, from the tab and with `-url`. It signs in, confirms the cookie file exists and is mode `0600` on macOS or Linux, runs a second lookup that passes `--cookies`, signs out, and closes the window before **Done** to confirm the previous file remains. After signing in to AfriForum TV, lookup of `https://afriforumtv.co.za/user/watch/f2ef181c-25a0-41ed-9618-191603dcf123?episodeId=58781465-1f2c-40dc-8f6f-bc7925082559` either shows one video and saves an MP4, or stops with `This video is encrypted`. Ending on `Unsupported URL` alone means page search did not run.

## Out of scope

A custom AfriForum API client, DRM decryption for any site other than the existing Crunchyroll path, subtitles, audio-only downloads, SponsorBlock, reading cookies from the user's everyday browser, and changes to Crunchyroll download behavior. YouTube channel, live, and embed URLs stay unrecognized. Page search does not build a playlist from posters on a series page.
