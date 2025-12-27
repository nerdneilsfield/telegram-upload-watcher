## Context
Zip archives may be password-protected, and users need finer control over which files are processed.

## Goals / Non-Goals
- Goals: password retry for zip files, include filters, optional zip traversal in image directories, and parity across Python/Go.
- Non-Goals: brute-force password discovery or advanced archive formats beyond zip.

## Decisions
- Support multiple --zip-pass flags and an optional --zip-pass-file containing one password per line.
- If all passwords fail, mark the zip items as failed and skip sending.
- Add --include filters (glob) in addition to existing --exclude filters; include is applied first.
- Add --enable-zip flag to allow zip traversal in send-images directory scans.

## Risks / Trade-offs
- Password attempts add latency to zip processing.
- Include/exclude ordering must be consistent across Python and Go.

## Migration Plan
- Add new flags and default behavior without changing existing flows unless flags are provided.
