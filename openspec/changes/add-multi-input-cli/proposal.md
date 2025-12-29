## Why
Running multiple sends or watch targets currently requires separate processes, which
complicates scheduling and increases queue collisions. We need repeatable CLI inputs
so one run can handle multiple sources.

## What Changes
- Allow repeatable `--image-dir` and `--zip-file` for send-images.
- Allow repeatable `--zip-file` and `--dir` for send-file/video/audio.
- Allow repeatable `--watch-dir` for watch, running a watcher per directory.
- Persist multiple watch directories in queue metadata so mismatched runs fail fast.

## Impact
- Affected specs: multi-input-cli (new)
- Affected code: Go CLI commands and queue metadata handling
