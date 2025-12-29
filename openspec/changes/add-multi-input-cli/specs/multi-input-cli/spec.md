## ADDED Requirements
### Requirement: Repeatable Send-Images Inputs
The CLI SHALL accept multiple `--image-dir` and `--zip-file` values for send-images.

#### Scenario: Multiple sources
- **WHEN** a user provides more than one `--image-dir` or `--zip-file`
- **THEN** each source is processed in order

### Requirement: Repeatable Zip Inputs for File Sends
The CLI SHALL accept multiple `--zip-file` values for send-file, send-video, and send-audio.

#### Scenario: Multiple zip files
- **WHEN** a user provides more than one `--zip-file`
- **THEN** each zip is sent in order with the same filters and retry settings

### Requirement: Repeatable Directory Inputs for File Sends
The CLI SHALL accept multiple `--dir` values for send-file, send-video, and send-audio.

#### Scenario: Multiple directories
- **WHEN** a user provides more than one `--dir`
- **THEN** each directory is sent in order with the same filters and retry settings

### Requirement: Repeatable Watch Directories
The CLI SHALL accept multiple `--watch-dir` values and start a watcher per directory.

#### Scenario: Multi-watch
- **WHEN** a user provides multiple watch directories
- **THEN** all directories are scanned and queued into the same queue file

### Requirement: Watch Metadata Consistency
Queue metadata SHALL record all watch directories when multiple paths are provided.

#### Scenario: Metadata mismatch
- **WHEN** a queue file was created with different watch directories
- **THEN** the run fails with a metadata mismatch error
