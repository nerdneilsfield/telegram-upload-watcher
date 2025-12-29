## ADDED Requirements
### Requirement: Mixed Send Command
The CLI SHALL provide a `send-mixed` command that can send images, videos, audio, and other files in one run.

#### Scenario: Mixed send from a zip
- **WHEN** a user runs `send-mixed --zip-file <zip>`
- **THEN** the command sends matching entries based on the enabled media selectors

### Requirement: Repeatable Inputs
The `send-mixed` command SHALL accept repeatable `--file`, `--dir`, and `--zip-file` inputs.

#### Scenario: Multiple sources
- **WHEN** a user specifies multiple input flags
- **THEN** each source is processed in order

### Requirement: Media Selectors
The `send-mixed` command SHALL support `--with-image`, `--with-video`, `--with-audio`, and `--with-file`.

#### Scenario: Select images and videos
- **WHEN** a user enables `--with-image --with-video`
- **THEN** images are sent via media groups and videos are sent via sendVideo

#### Scenario: Select files
- **WHEN** a user enables `--with-file`
- **THEN** non-image/video/audio entries are sent via sendDocument

### Requirement: Include/Exclude Filtering
The `send-mixed` command SHALL apply `--include` and `--exclude` filters to directory and zip entries.

#### Scenario: Filtered zip
- **WHEN** a user provides include/exclude patterns
- **THEN** only matching entries are processed before sending
