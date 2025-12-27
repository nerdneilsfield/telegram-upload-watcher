## Why
We need to support encrypted zip files and better filtering controls (include + exclude) across send and watch workflows.

## What Changes
- Add zip password attempts from CLI or password file, skipping files that cannot be decrypted.
- Allow send-images to traverse zip files inside a directory when enabled.
- Add --include filters alongside --exclude for watch/send-images/zip processing.

## Impact
- Affected specs: zip-include-filter (new)
- Affected code: watcher, sender, CLI flags (Python + Go)
