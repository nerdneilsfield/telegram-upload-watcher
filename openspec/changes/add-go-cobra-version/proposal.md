## Why
The Go CLI needs a version command with build metadata, and Cobra provides a structured, extensible command framework to support that cleanly.

## What Changes
- Replace the Go CLI flag parsing with Cobra commands (send-message, send-images, watch, version)
- Add a `version` subcommand that prints version/buildTime/gitCommit/goVersion
- Update build tooling (Makefile/justfile/Dockerfile/GoReleaser) to inject version metadata via ldflags
- Update README examples for the new command

## Impact
- Affected specs: go-cli
- Affected code: go/cmd, go/main.go, go/internal, Makefile, justfile, Dockerfile, .goreleaser.yml, README.md
