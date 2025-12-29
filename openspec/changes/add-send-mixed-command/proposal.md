## Why
Users need a single command to send mixed media (images, videos, audio, and other files)
from files, directories, or zips without using watch mode.

## What Changes
- Add a new `send-mixed` command to both Python and Go CLIs.
- Support repeatable `--file`, `--dir`, and `--zip-file` inputs.
- Add media selectors: `--with-image`, `--with-video`, `--with-audio`, `--with-file`.
- Route images via media groups, videos via sendVideo, audio via sendAudio, and other files via sendDocument.
- Apply include/exclude filters to directory and zip entries.

## Impact
- Affected specs: send-mixed (new)
- Affected code: Python CLI + sender utilities, Go CLI + sender utilities, README examples
