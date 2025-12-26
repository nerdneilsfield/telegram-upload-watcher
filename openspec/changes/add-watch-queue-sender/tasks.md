## 1. Implementation
- [ ] 1.1 Add watcher config (poll interval, recursive flag, exclude globs)
- [ ] 1.2 Implement file scanning and change detection for new files
- [ ] 1.3 Implement JSONL queue persistence with enqueue/dequeue/status updates
- [ ] 1.4 Add file stability check (settle window) before enqueue
- [ ] 1.5 Expand directory/zip inputs into file queue entries and track sent status for restart resume
- [ ] 1.6 Add image preprocessing for max dimension scaling and size-based PNG compression
- [ ] 1.7 Implement sender loop that drains queue on a schedule
- [ ] 1.8 Wire CLI flags for watch/queue/sender settings
- [ ] 1.9 Update README with watch/queue usage
