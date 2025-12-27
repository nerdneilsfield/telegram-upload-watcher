package watcher

import (
	"archive/zip"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
)

type Config struct {
	Root          string
	Recursive     bool
	ExcludeGlobs  []string
	ScanInterval  time.Duration
	SettleSeconds int
}

type stabilityTracker struct {
	settleSeconds int
	state         map[string]entry
}

type entry struct {
	size       int64
	mtimeNS    int64
	lastChange time.Time
}

func newTracker(settleSeconds int) *stabilityTracker {
	return &stabilityTracker{
		settleSeconds: settleSeconds,
		state:         map[string]entry{},
	}
}

func (t *stabilityTracker) isStable(path string, size int64, mtimeNS int64) bool {
	now := time.Now()
	prev, ok := t.state[path]
	if !ok {
		t.state[path] = entry{size: size, mtimeNS: mtimeNS, lastChange: now}
		return false
	}
	if prev.size != size || prev.mtimeNS != mtimeNS {
		t.state[path] = entry{size: size, mtimeNS: mtimeNS, lastChange: now}
		return false
	}
	if now.Sub(prev.lastChange) >= time.Duration(t.settleSeconds)*time.Second {
		delete(t.state, path)
		return true
	}
	return false
}

func (t *stabilityTracker) prune(paths map[string]struct{}) {
	for key := range t.state {
		if _, ok := paths[key]; !ok {
			delete(t.state, key)
		}
	}
}

func matchesExclude(rel string, patterns []string) bool {
	rel = path.Clean(path.ToSlash(rel))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
	}
	return false
}

func isImage(name string) bool {
	name = strings.ToLower(name)
	for _, ext := range constants.ImageExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func scanOnce(cfg Config, q *queue.Queue, tracker *stabilityTracker) int {
	root := cfg.Root
	enqueued := 0
	seen := map[string]struct{}{}

	handleFile := func(path string, info os.FileInfo) {
		seen[path] = struct{}{}
		nameLower := strings.ToLower(info.Name())
		if !isImage(nameLower) && !strings.HasSuffix(nameLower, ".zip") {
			return
		}

		mtimeNS := info.ModTime().UnixNano()
		fingerprint := queue.BuildFingerprint("file", path, nil, info.Size(), &mtimeNS, nil)
		if q.HasFingerprint(fingerprint) {
			return
		}

		if strings.HasSuffix(nameLower, ".zip") {
			sourceFingerprint := queue.BuildSourceFingerprint(path, info.Size(), &mtimeNS)
			if q.HasSourceFingerprint("zip", sourceFingerprint) {
				return
			}
			if !tracker.isStable(path, info.Size(), mtimeNS) {
				return
			}
			enqueued += enqueueZip(q, path, info, cfg.ExcludeGlobs)
			return
		}

		if !tracker.isStable(path, info.Size(), mtimeNS) {
			return
		}
		item := queue.Item{
			SourceType:        "file",
			SourcePath:        path,
			SourceFingerprint: queue.BuildSourceFingerprint(path, info.Size(), &mtimeNS),
			Path:              path,
			Size:              info.Size(),
			MTimeNS:           &mtimeNS,
			Fingerprint:       fingerprint,
		}
		if _, err := q.Enqueue(item); err == nil {
			enqueued++
		}
	}

	if cfg.Recursive {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == root {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			if matchesExclude(rel, cfg.ExcludeGlobs) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			handleFile(path, info)
			return nil
		})
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return 0
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if matchesExclude(entry.Name(), cfg.ExcludeGlobs) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			handleFile(filepath.Join(root, entry.Name()), info)
		}
	}

	tracker.prune(seen)
	return enqueued
}

func enqueueZip(q *queue.Queue, zipPath string, info os.FileInfo, exclude []string) int {
	count := 0
	sourceFingerprint := queue.BuildSourceFingerprint(zipPath, info.Size(), ptrInt64(info.ModTime().UnixNano()))
	if q.HasSourceFingerprint("zip", sourceFingerprint) {
		return 0
	}

	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return 0
	}
	defer archive.Close()

	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		inner := path.Clean(path.ToSlash(file.Name))
		if matchesExclude(inner, exclude) {
			continue
		}
		if !isImage(inner) {
			continue
		}
		innerCopy := inner
		size := int64(file.UncompressedSize64)
		crc := file.CRC32
		item := queue.Item{
			SourceType:        "zip",
			SourcePath:        zipPath,
			SourceFingerprint: sourceFingerprint,
			Path:              zipPath,
			InnerPath:         &innerCopy,
			Size:              size,
			Fingerprint:       queue.BuildFingerprint("zip", zipPath, &innerCopy, size, nil, &crc),
			CRC:               &crc,
		}
		if _, err := q.Enqueue(item); err == nil {
			count++
		}
	}
	return count
}

func ptrInt64(value int64) *int64 {
	return &value
}

func WatchLoop(cfg Config, q *queue.Queue) {
	tracker := newTracker(cfg.SettleSeconds)
	for {
		enqueued := scanOnce(cfg, q, tracker)
		if enqueued > 0 {
			log.Printf("enqueued %d file(s)", enqueued)
		}
		time.Sleep(cfg.ScanInterval)
	}
}
