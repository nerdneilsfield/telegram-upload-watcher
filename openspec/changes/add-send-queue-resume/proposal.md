## Why
One-off send commands currently lose progress on interruption and cannot resume or
retry failed items across runs. Users need a queue-backed mode to resume and apply
per-item retry limits.

## What Changes
- Add optional queue support to send-images, send-file, send-video, send-audio, and send-mixed (Python + Go).
- Introduce `--queue-file` to enable JSONL persistence and resume behavior.
- Introduce `--queue-retries` (default 3) to control per-item retry attempts.
- Record queue metadata for command parameters and validate it on resume.
- Persist per-item attempt counts and last error in the queue log.

## Impact
- Affected specs: send-queue-resume (new)
- Affected code: Python CLI + queue handling, Go CLI + queue handling, README examples
