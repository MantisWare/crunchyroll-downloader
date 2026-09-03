# Changelog

## Unreleased

- Added a cross-platform desktop UI (macOS, Linux, Windows) for fetching series/episode lists and downloading with the same options as the CLI
- Added `build.sh` for the CLI binary and `build_UI.sh` for the GUI binary
- Desktop UI: info tooltip for `etp_rt`, Lookup next to the URL, and download options shown only after a successful lookup of available audio/subs/quality/seasons
- Desktop UI: options use compact single-line rows and the window grows to fit them, so revealing them after Lookup no longer squashes the episode and progress panes
- Desktop UI: episode checkbox selection is remembered when changing audio, subtitles, quality, or season instead of resetting to all selected
- Settings (`etp_rt`, languages, quality, output folder) are saved in `~/.crunchyroll.config/config.json` on launch and whenever they change
- Desktop UI: added a Reset button that clears the URL, episode list, log, and progress so a new link can be pasted, keeping saved credentials and preferences
- Desktop UI: added a download progress bar showing the current episode, run position, and percentage, driven by the downloader's segment and pass output

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
