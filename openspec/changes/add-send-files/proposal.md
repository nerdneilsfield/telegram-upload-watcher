## Why
Users need to send general files and media (video/audio) without media groups, using Telegram's sendDocument/sendVideo/sendVoice APIs with the same folder/zip/watch workflow.

## What Changes
- Add `send-file`, `send-video`, and `send-audio` commands (Python + Go)
- Support directory traversal, zip files, include/exclude, and zip passwords for these commands
- Extend watch mode with `--with-image`, `--with-video`, `--with-audio`, and `--all` flags (combinable) to select what gets queued/sent
- Keep image sending on the existing media-group pipeline; send file/video/audio as single-item uploads
- Track the send type in queued items for correct dispatch
- Update README with new commands and examples

## Impact
- Affected specs: file-send
- Affected code: telegram_upload_watcher, go/cmd, go/internal, queue, watcher, sender, README.md
