# Changelog

## Unreleased

- Added a cross-platform desktop UI (macOS, Linux, Windows) for fetching series/episode lists and downloading with the same options as the CLI
- Added `build.sh` for the CLI binary and `build_UI.sh` for the GUI binary
- Desktop UI: info tooltip for `etp_rt`, Lookup next to the URL, and download options shown only after a successful lookup of available audio/subs/quality/seasons
- Added automatic Crunchyroll sign-in: the desktop UI opens an isolated Chrome, Edge, Brave, or Chromium window, captures the `etp_rt` session cookie after login, and removes the temporary browser profile
- Added `-browser-login` to provide the same guided sign-in flow from the CLI, with a 10-minute timeout and Ctrl+C cancellation
- Manual `etp_rt` entry remains available when no supported Chromium-based browser is installed
- Desktop UI: options use compact single-line rows and the window grows to fit them, so revealing them after Lookup no longer squashes the episode and progress panes
- Desktop UI: episode checkbox selection is remembered when changing audio, subtitles, quality, or season instead of resetting to all selected
- Settings (`etp_rt`, languages, quality, output folder) are saved in `~/.crunchyroll.config/config.json` on launch and whenever they change
- Desktop UI: added a Reset button that clears the URL, episode list, log, and progress so a new link can be pasted, keeping saved credentials and preferences
- Desktop UI: added a download progress bar showing the current episode, run position, and percentage, driven by the downloader's segment and pass output
- Desktop UI: added a Stop button that interrupts an in-flight download, cancelling segment fetches, range streaming, and the FFmpeg mux, then cleaning up partial files
- Network and mux work now runs under a cancellable context; segment/subtitle/manifest fetches and retry waits abort promptly when stopped
- Manifest, subtitle, and stream-teardown failures now return errors instead of panicking, so a network error can no longer crash the UI
- Desktop UI: reorganized the app into Download and Convert tabs
- Convert tab: added drag-and-drop plus file/folder pickers, selectable video/audio quality, FFmpeg output, queue progress, and cancellation
- Converted files keep the source basename (including episode titles) and apply a quality tag such as `Show S01E02 Dawn [720p].mp4`; folder conversions are written to `converted_720p/`
- Downloaded files include the Crunchyroll episode title, e.g. `Show S01E02 Dawn and Confusion [1080p].mkv`; existing SxxExx files without a title still count as already downloaded
- Fixed an infinite "Access token expired. Refetching one..." loop that stalled season downloads partway through: a rejected token exchange returned an empty token, which was then used to sign the retry, guaranteeing another 401 forever
- Token refresh is now bounded (3 attempts) with backoff, and reports a clear error naming an expired `etp_rt` cookie instead of looping silently
- A failed token exchange (rate limit, revoked cookie, empty `access_token`) is now an error rather than being accepted as a valid token
- Access tokens are refreshed proactively based on `expires_in`, so long season runs no longer hit a 401 on nearly every episode
- Concurrent 401s collapse into a single token fetch, so retries no longer stampede the rate-limited token endpoint
- Fixed a connection leak: rejected 401 responses were never closed, and each request built a throwaway HTTP client
- Requests now share pooled clients with dial, TLS, and response-header timeouts, so a silent server can no longer hang a download indefinitely
- Token reads and writes are mutex-guarded, fixing a data race between the download and UI goroutines
- Fixed GUI token refreshes using an empty CLI cookie after the initial login; the active GUI cookie is now retained for long-running season downloads
- Fixed a startup crash in the Convert tab: applying the saved video quality fired its change handler before the Convert button existed, dereferencing a nil widget
- Fixed saved conversion qualities being overwritten with defaults on launch, because the Download tab's selects persisted settings before the Convert tab was built

## 1.3.0

- Fixed `-audio-lang` flag being ignored — API calls had `preferred_audio_language` hardcoded to `ja-JP` instead of using the user-provided value
- Fixed season/series downloads always using the Japanese audio GUID instead of resolving the correct dub version per episode
- Episodes with no matching dub are now skipped with a warning instead of silently downloading the wrong language
- Added `-help` command with full setup guide, usage examples, and supported language codes
- Added `ja-JP` (Japanese) to the language names map
- Widevine CDM files are now searched in `./`, `assets/`, and `assets/` relative to the binary — no longer requires running from the same directory as the `.wvd` file
- Added GitHub Actions CI/CD workflow with cross-platform builds (Linux, macOS, Windows) and automatic GitHub Releases on tag push
- Build output now targets `bin/` directory
- Season downloads retry missing or incomplete episodes after the first pass
- Re-running a season scans the series folder for existing `SxxExx` MKVs and skips complete files

## 1.2.0

- Parallel segment downloads (10 workers) for much faster downloads
- Retry with backoff on connection errors instead of crashing
- Added `--urls` flag to batch download from a text file with one URL per line
- Invalid URLs in batch mode are skipped instead of stopping the whole process

## 1.1.1

- Optimized code, tried to handle errors
- Some random fixes
- Added a way to automatically refetch an access token if the current one expires

## 1.1.0

- Added support for downloading entire seasons
- Fixed MPD parsing
- Temporary downloaded files (video, audio segments and subtitles) are now stored in the OS temporary files then deleted
- Fixed FFmpeg merge command
- Docs improvements
- Support for `device_id.bin` and `private_key.pem` files

## 1.0.0

Initial release
