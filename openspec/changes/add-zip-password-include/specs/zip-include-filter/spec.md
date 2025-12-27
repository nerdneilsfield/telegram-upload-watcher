## ADDED Requirements
### Requirement: Zip Password Attempts
The system SHALL allow users to provide multiple zip passwords via CLI flags and/or a password file and attempt them in order.

#### Scenario: Zip password succeeds
- **WHEN** a provided password decrypts the archive
- **THEN** the zip contents are processed normally

#### Scenario: All passwords fail
- **WHEN** none of the provided passwords can decrypt the archive
- **THEN** the archive is skipped and items are marked failed

### Requirement: Include Filters
The system SHALL support --include glob filters in addition to --exclude, applied across watch and send operations.

#### Scenario: Include filter
- **WHEN** include globs are provided
- **THEN** only matching files are considered before applying excludes

### Requirement: Directory Zip Traversal
The system SHALL optionally include zip files when scanning image directories if --enable-zip is set.

#### Scenario: Enable zip traversal
- **WHEN** --enable-zip is provided
- **THEN** zip files within the directory are processed
