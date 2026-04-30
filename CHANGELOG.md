# Changelog

## 1.3.0

- Fixed `-audio-lang` flag being ignored — API calls had `preferred_audio_language` hardcoded to `ja-JP` instead of using the user-provided value
- Fixed season/series downloads always using the Japanese audio GUID instead of resolving the correct dub version per episode
- Episodes with no matching dub are now skipped with a warning instead of silently downloading the wrong language
- Added `-help` command with full setup guide, usage examples, and supported language codes
- Added `ja-JP` (Japanese) to the language names map
- Widevine CDM files are now searched in `./`, `assets/`, and `assets/` relative to the binary — no longer requires running from the same directory as the `.wvd` file
- Added GitHub Actions CI/CD workflow with cross-platform builds (Linux, macOS, Windows) and automatic GitHub Releases on tag push
- Build output now targets `bin/` directory

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
