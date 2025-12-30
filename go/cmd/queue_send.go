package cmd

import (
	"archive/zip"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/ziputil"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
)

type queueSendConfig struct {
	chatID          string
	topicID         *int
	groupSize       int
	batchDelay      time.Duration
	maxDimension    int
	maxBytes        int
	pngStartLevel   int
	retry           telegram.RetryConfig
	zipPasswords    []string
	logZipPasswords bool
	queueRetries    int
}

func resolveAbsPaths(values []string) ([]string, error) {
	paths := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return nil, err
		}
		paths = append(paths, abs)
	}
	return paths, nil
}

func validateQueueRetries(value int) (int, error) {
	if value < 1 {
		return 0, fmt.Errorf("queue-retries must be >= 1")
	}
	return value, nil
}

func enqueueFileItem(q *queue.Queue, path string, sendType string) int {
	info, err := os.Stat(path)
	if err != nil {
		log.Printf("stat failed for %s: %v", path, err)
		return 0
	}
	mtimeNS := info.ModTime().UnixNano()
	item := queue.Item{
		SourceType:        "file",
		SourcePath:        path,
		SourceFingerprint: queue.BuildSourceFingerprint(path, info.Size(), &mtimeNS),
		Path:              path,
		Size:              info.Size(),
		MTimeNS:           &mtimeNS,
		SendType:          sendType,
		Fingerprint:       queue.BuildFingerprint("file", path, nil, info.Size(), &mtimeNS, nil),
	}
	added, err := q.Enqueue(item)
	if err != nil {
		log.Printf("enqueue failed for %s: %v", path, err)
		return 0
	}
	if added == nil {
		return 0
	}
	return 1
}

func enqueueImagesFromDir(q *queue.Queue, dir string, include []string, exclude []string, enableZip bool, startIndex int, endIndex int, groupSize int, zipPasswords []string) int {
	files := collectFiles(dir, include, exclude, enableZip, constants.ImageExtensions)
	if len(files) == 0 {
		log.Printf("no images found in %s", dir)
		return 0
	}
	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	rangeStart, rangeEnd := clampRange(minIndex, maxIndex, len(files))
	enqueued := 0
	for _, path := range files[rangeStart:rangeEnd] {
		if enableZip && strings.HasSuffix(strings.ToLower(path), ".zip") {
			enqueued += enqueueZipImages(q, path, include, exclude, 0, 0, groupSize, zipPasswords)
			continue
		}
		if !isImage(path) {
			continue
		}
		enqueued += enqueueFileItem(q, path, "image")
	}
	return enqueued
}

func enqueueZipImages(q *queue.Queue, zipPath string, include []string, exclude []string, startIndex int, endIndex int, groupSize int, zipPasswords []string) int {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return 0
	}
	defer archive.Close()

	sourceInfo, err := os.Stat(zipPath)
	if err != nil {
		log.Printf("stat failed for %s: %v", zipPath, err)
		return 0
	}
	mtimeNS := sourceInfo.ModTime().UnixNano()
	sourceFingerprint := queue.BuildSourceFingerprint(zipPath, sourceInfo.Size(), &mtimeNS)

	entries := []*zip.File{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(file.Name)
		if !matchesInclude(name, include) {
			continue
		}
		if matchesExclude(name, exclude) {
			continue
		}
		if !isImage(name) {
			continue
		}
		entries = append(entries, file)
	}
	if len(entries) == 0 {
		log.Printf("no images found in %s", zipPath)
		return 0
	}

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	rangeStart, rangeEnd := clampRange(minIndex, maxIndex, len(entries))

	enqueued := 0
	for _, file := range entries[rangeStart:rangeEnd] {
		inner := filepath.ToSlash(file.Name)
		innerCopy := inner
		size := int64(file.UncompressedSize64)
		crc := file.CRC32
		crcCopy := crc
		item := queue.Item{
			SourceType:        "zip",
			SourcePath:        zipPath,
			SourceFingerprint: sourceFingerprint,
			Path:              zipPath,
			InnerPath:         &innerCopy,
			Size:              size,
			CRC:               &crcCopy,
			SendType:          "image",
			Fingerprint:       queue.BuildFingerprint("zip", zipPath, &innerCopy, size, nil, &crcCopy),
		}
		added, err := q.Enqueue(item)
		if err != nil {
			log.Printf("enqueue failed for %s:%s: %v", zipPath, inner, err)
			continue
		}
		if added != nil {
			enqueued++
		}
	}
	return enqueued
}

func enqueueFilesFromDir(q *queue.Queue, dir string, sendType string, include []string, exclude []string, enableZip bool, startIndex int, endIndex int, zipPasswords []string) int {
	allowed := allowedExtsForType(sendType)
	files := collectFiles(dir, include, exclude, enableZip, allowed)
	if len(files) == 0 {
		log.Printf("no files found in %s", dir)
		return 0
	}
	rangeStart, rangeEnd := clampRange(startIndex, endIndex, len(files))
	enqueued := 0
	for _, path := range files[rangeStart:rangeEnd] {
		if enableZip && strings.HasSuffix(strings.ToLower(path), ".zip") {
			enqueued += enqueueZipFiles(q, path, sendType, include, exclude, 0, 0, zipPasswords)
			continue
		}
		enqueued += enqueueFileItem(q, path, sendType)
	}
	return enqueued
}

func enqueueZipFiles(q *queue.Queue, zipPath string, sendType string, include []string, exclude []string, startIndex int, endIndex int, zipPasswords []string) int {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return 0
	}
	defer archive.Close()

	sourceInfo, err := os.Stat(zipPath)
	if err != nil {
		log.Printf("stat failed for %s: %v", zipPath, err)
		return 0
	}
	mtimeNS := sourceInfo.ModTime().UnixNano()
	sourceFingerprint := queue.BuildSourceFingerprint(zipPath, sourceInfo.Size(), &mtimeNS)
	allowed := allowedExtsForType(sendType)

	entries := []*zip.File{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(file.Name)
		if !matchesInclude(name, include) {
			continue
		}
		if matchesExclude(name, exclude) {
			continue
		}
		if allowed != nil && !matchesExt(name, allowed) {
			continue
		}
		entries = append(entries, file)
	}
	if len(entries) == 0 {
		log.Printf("no matching files found in %s", zipPath)
		return 0
	}

	rangeStart, rangeEnd := clampRange(startIndex, endIndex, len(entries))
	enqueued := 0
	for _, file := range entries[rangeStart:rangeEnd] {
		inner := filepath.ToSlash(file.Name)
		innerCopy := inner
		size := int64(file.UncompressedSize64)
		crc := file.CRC32
		crcCopy := crc
		item := queue.Item{
			SourceType:        "zip",
			SourcePath:        zipPath,
			SourceFingerprint: sourceFingerprint,
			Path:              zipPath,
			InnerPath:         &innerCopy,
			Size:              size,
			CRC:               &crcCopy,
			SendType:          sendType,
			Fingerprint:       queue.BuildFingerprint("zip", zipPath, &innerCopy, size, nil, &crcCopy),
		}
		added, err := q.Enqueue(item)
		if err != nil {
			log.Printf("enqueue failed for %s:%s: %v", zipPath, inner, err)
			continue
		}
		if added != nil {
			enqueued++
		}
	}
	return enqueued
}

func collectSourceFiles(dir string, include []string, exclude []string) []string {
	files := []string{}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !matchesInclude(rel, include) {
			return nil
		}
		if matchesExclude(rel, exclude) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files
}

func enqueueMixedFromPaths(q *queue.Queue, paths []string, sel mixedSelection, include []string, exclude []string, applyFilters bool, enableZip bool, zipPasswords []string) int {
	enqueued := 0
	for _, path := range paths {
		name := filepath.Base(path)
		if applyFilters {
			if include != nil && !matchesInclude(name, include) {
				continue
			}
			if exclude != nil && matchesExclude(name, exclude) {
				continue
			}
		}
		if enableZip && strings.HasSuffix(strings.ToLower(path), ".zip") {
			enqueued += enqueueZipMixed(q, path, sel, include, exclude, zipPasswords)
			continue
		}
		sendType := mixedSendType(name, sel)
		if sendType == "" {
			continue
		}
		enqueued += enqueueFileItem(q, path, sendType)
	}
	return enqueued
}

func enqueueZipMixed(q *queue.Queue, zipPath string, sel mixedSelection, include []string, exclude []string, zipPasswords []string) int {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return 0
	}
	defer archive.Close()

	sourceInfo, err := os.Stat(zipPath)
	if err != nil {
		log.Printf("stat failed for %s: %v", zipPath, err)
		return 0
	}
	mtimeNS := sourceInfo.ModTime().UnixNano()
	sourceFingerprint := queue.BuildSourceFingerprint(zipPath, sourceInfo.Size(), &mtimeNS)

	enqueued := 0
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(file.Name)
		if include != nil && !matchesInclude(name, include) {
			continue
		}
		if exclude != nil && matchesExclude(name, exclude) {
			continue
		}
		sendType := mixedSendType(name, sel)
		if sendType == "" {
			continue
		}
		inner := filepath.ToSlash(file.Name)
		innerCopy := inner
		size := int64(file.UncompressedSize64)
		crc := file.CRC32
		crcCopy := crc
		item := queue.Item{
			SourceType:        "zip",
			SourcePath:        zipPath,
			SourceFingerprint: sourceFingerprint,
			Path:              zipPath,
			InnerPath:         &innerCopy,
			Size:              size,
			CRC:               &crcCopy,
			SendType:          sendType,
			Fingerprint:       queue.BuildFingerprint("zip", zipPath, &innerCopy, size, nil, &crcCopy),
		}
		added, err := q.Enqueue(item)
		if err != nil {
			log.Printf("enqueue failed for %s:%s: %v", zipPath, inner, err)
			continue
		}
		if added != nil {
			enqueued++
		}
	}
	return enqueued
}

func loadQueueItem(item *queue.Item, zipPasswords []string, opts ziputil.ReadOptions) ([]byte, string, error) {
	switch item.SourceType {
	case "file":
		data, err := os.ReadFile(item.Path)
		if err != nil {
			return nil, "", err
		}
		return data, filepath.Base(item.Path), nil
	case "zip":
		if item.InnerPath == nil {
			return nil, "", fmt.Errorf("zip entry missing")
		}
		archive, err := zip.OpenReader(item.Path)
		if err != nil {
			return nil, "", err
		}
		defer archive.Close()
		for _, file := range archive.File {
			if filepath.ToSlash(file.Name) != filepath.ToSlash(*item.InnerPath) {
				continue
			}
			data, err := ziputil.ReadFileWithOptions(file, zipPasswords, opts)
			if err != nil {
				return nil, "", err
			}
			return data, filepath.Base(file.Name), nil
		}
		return nil, "", fmt.Errorf("zip entry not found: %s", *item.InnerPath)
	default:
		return nil, "", fmt.Errorf("unsupported source type: %s", item.SourceType)
	}
}

func markFailed(q *queue.Queue, item *queue.Item, err error) {
	if item == nil {
		return
	}
	msg := err.Error()
	attempts := item.Attempts + 1
	if updateErr := q.UpdateStatusWithAttempts(item.ID, queue.StatusFailed, &msg, &attempts); updateErr != nil {
		log.Printf("queue update failed: %v", updateErr)
	}
}

func drainQueue(client *telegram.Client, q *queue.Queue, label string, cfg queueSendConfig) (int, int, int64) {
	pending := q.PendingWithAttempts(0, cfg.queueRetries)
	if len(pending) == 0 {
		return 0, 0, 0
	}

	progressState := newProgressTracker(len(pending), label)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)
	zipOpts := ziputil.ReadOptions{LogPasswords: cfg.logZipPasswords}

	for i := 0; i < len(pending); {
		item := pending[i]
		sendType := item.SendType
		if sendType == "" {
			sendType = "image"
		}
		if sendType == "image" {
			group := []*queue.Item{}
			for i < len(pending) && len(group) < cfg.groupSize {
				current := pending[i]
				currentType := current.SendType
				if currentType == "" {
					currentType = "image"
				}
				if currentType != "image" {
					break
				}
				group = append(group, current)
				i++
			}

			media := []telegram.MediaFile{}
			itemRefs := []*queue.Item{}
			groupBytes := int64(0)
			for _, entry := range group {
				_ = q.UpdateStatus(entry.ID, queue.StatusSending, nil)
				data, filename, err := loadQueueItem(entry, cfg.zipPasswords, zipOpts)
				if err != nil {
					markFailed(q, entry, err)
					skipped++
					continue
				}
				prepared, err := prepareImageMedia(data, filename, cfg.maxDimension, cfg.maxBytes, cfg.pngStartLevel)
				if err != nil {
					markFailed(q, entry, err)
					skipped++
					continue
				}
				media = append(media, prepared)
				itemRefs = append(itemRefs, entry)
				groupBytes += int64(len(data))
			}

			if len(media) > 0 {
				if err := client.SendMediaGroup(cfg.chatID, media, cfg.topicID, cfg.retry); err != nil {
					for _, entry := range itemRefs {
						markFailed(q, entry, err)
					}
					skipped += len(itemRefs)
				} else {
					for _, entry := range itemRefs {
						_ = q.UpdateStatus(entry.ID, queue.StatusSent, nil)
					}
					sent += len(itemRefs)
					sentBytes += groupBytes
				}
				time.Sleep(cfg.batchDelay)
			}

			processed += len(group)
			progressState.Print(processed, sent, skipped, false)
			continue
		}

		_ = q.UpdateStatus(item.ID, queue.StatusSending, nil)
		data, filename, err := loadQueueItem(item, cfg.zipPasswords, zipOpts)
		if err != nil {
			markFailed(q, item, err)
			skipped++
			processed++
			progressState.Print(processed, sent, skipped, false)
			i++
			continue
		}

		if err := sendSingleFile(client, cfg.chatID, cfg.topicID, sendType, filename, data, cfg.retry); err != nil {
			markFailed(q, item, err)
			skipped++
		} else {
			_ = q.UpdateStatus(item.ID, queue.StatusSent, nil)
			sent++
			sentBytes += int64(len(data))
		}
		processed++
		progressState.Print(processed, sent, skipped, false)
		i++
		time.Sleep(cfg.batchDelay)
	}

	progressState.Print(processed, sent, skipped, true)
	return sent, skipped, sentBytes
}
