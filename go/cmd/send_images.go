package cmd

import (
	"archive/zip"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	imageutil "github.com/nerdneilsfield/telegram-upload-watcher/go/internal/image"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/ziputil"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
	"github.com/spf13/cobra"
)

func newSendImagesCmd() *cobra.Command {
	cfg := &commonFlags{}
	imageDirs := &stringSlice{}
	zipFiles := &stringSlice{}
	var groupSize int
	var startIndex int
	var endIndex int
	var batchDelay int
	var enableZip bool
	includes := &stringSlice{}
	excludes := &stringSlice{}
	zipPasses := &stringSlice{}
	var zipPassFile string
	var logZipPasswords bool
	var maxDimension int
	var maxBytes int
	var pngStartLevel int

	cmd := &cobra.Command{
		Use:          "send-images",
		Short:        "Send images from a directory or zip",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryDelay = time.Duration(cfg.retryDelaySec) * time.Second
			if cfg.chatID == "" {
				return fmt.Errorf("chat-id is required")
			}
			if len(imageDirs.Values()) == 0 && len(zipFiles.Values()) == 0 {
				return fmt.Errorf("image-dir or zip-file is required")
			}

			apiURLs, tokens, err := resolveConfig(cfg)
			if err != nil {
				return err
			}
			client, _, _, err := buildClient(cfg, apiURLs, tokens)
			if err != nil {
				return err
			}

			zipPasswords, err := loadZipPasswords(zipPasses.Values(), zipPassFile)
			if err != nil {
				return err
			}

			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
			for _, imageDir := range imageDirs.Values() {
				sendImagesFromDir(
					client,
					cfg.chatID,
					topicPtr(cfg),
					imageDir,
					groupSize,
					startIndex,
					endIndex,
					time.Duration(batchDelay)*time.Second,
					includes.Values(),
					excludes.Values(),
					enableZip,
					zipPasswords,
					logZipPasswords,
					maxDimension,
					maxBytes,
					pngStartLevel,
					retry,
				)
			}
			for _, zipFile := range zipFiles.Values() {
				sendImagesFromZip(
					client,
					cfg.chatID,
					topicPtr(cfg),
					zipFile,
					groupSize,
					startIndex,
					endIndex,
					time.Duration(batchDelay)*time.Second,
					includes.Values(),
					excludes.Values(),
					zipPasswords,
					logZipPasswords,
					maxDimension,
					maxBytes,
					pngStartLevel,
					retry,
				)
			}
			return nil
		},
	}

	bindCommonFlags(cmd, cfg)
	flags := cmd.Flags()
	flags.Var(imageDirs, "image-dir", "Image directory (repeatable or comma-separated)")
	flags.Var(zipFiles, "zip-file", "Zip file path (repeatable or comma-separated)")
	flags.IntVar(&groupSize, "group-size", 4, "Images per media group")
	flags.IntVar(&startIndex, "start-index", 0, "Start group index (0-based)")
	flags.IntVar(&endIndex, "end-index", 0, "End group index (0 for no limit)")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between media groups (seconds)")
	flags.BoolVar(&enableZip, "enable-zip", false, "Process zip files when scanning directories")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	flags.BoolVar(&logZipPasswords, "zip-pass-log", false, "Log zip passwords while checking (use with care)")
	flags.IntVar(&maxDimension, "max-dimension", 2000, "Max image dimension (0 to disable resize)")
	flags.IntVar(&maxBytes, "max-bytes", 5*1024*1024, "Max image size in bytes (0 to disable size limit)")
	flags.IntVar(&pngStartLevel, "png-start-level", 8, "PNG compression start level (0-9)")
	return cmd
}

func sendImagesFromDir(client *telegram.Client, chatID string, topicID *int, dir string, groupSize int, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, enableZip bool, zipPasswords []string, logZipPasswords bool, maxDimension int, maxBytes int, pngStartLevel int, retry telegram.RetryConfig) {
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
		nameLower := strings.ToLower(path)
		if isImage(nameLower) {
			files = append(files, path)
		} else if enableZip && strings.HasSuffix(nameLower, ".zip") {
			files = append(files, path)
		}
		return nil
	})

	if len(files) == 0 {
		log.Printf("no images found in %s", dir)
		return
	}

	startedAt := time.Now()
	_ = client.SendMessage(chatID, fmt.Sprintf("Starting image upload: %d file(s) at %s", len(files), formatTimestamp(startedAt)), topicID, retry)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	rangeStart, rangeEnd := clampRange(minIndex, maxIndex, len(files))
	total := rangeEnd - rangeStart
	progressState := newProgressTracker(total, "image")

	media := []telegram.MediaFile{}
	batchBytes := int64(0)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)
	for idx, path := range files {
		if idx < rangeStart {
			continue
		}
		if idx >= rangeEnd {
			break
		}
		if strings.HasSuffix(strings.ToLower(path), ".zip") {
			sendImagesFromZip(client, chatID, topicID, path, groupSize, 0, 0, delay, include, exclude, zipPasswords, logZipPasswords, maxDimension, maxBytes, pngStartLevel, retry)
			processed++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		sourceBytes := int64(len(data))
		prepared, err := prepareImageMedia(data, filepath.Base(path), maxDimension, maxBytes, pngStartLevel)
		if err != nil {
			log.Printf("invalid image %s: %v", filepath.Base(path), err)
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		media = append(media, prepared)
		batchBytes += sourceBytes
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
				skipped += len(media)
			} else {
				sent += len(media)
				sentBytes += batchBytes
			}
			processed += len(media)
			progressState.Print(processed, sent, skipped, false)
			media = media[:0]
			batchBytes = 0
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
			skipped += len(media)
		} else {
			sent += len(media)
			sentBytes += batchBytes
		}
		processed += len(media)
	}
	progressState.Print(processed, sent, skipped, true)

	finishedAt := time.Now()
	elapsed := finishedAt.Sub(startedAt)
	avgPer := time.Duration(0)
	if sent > 0 {
		avgPer = elapsed / time.Duration(sent)
	}
	_ = client.SendMessage(
		chatID,
		fmt.Sprintf(
			"Completed image upload from %s at %s (elapsed %s, avg/image %s, total %s, avg %s, sent %d, skipped %d)",
			dir,
			formatTimestamp(finishedAt),
			formatDuration(elapsed),
			formatDuration(avgPer),
			formatBytes(sentBytes),
			formatSpeed(sentBytes, elapsed),
			sent,
			skipped,
		),
		topicID,
		retry,
	)
	printSummary("image", dir, startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}

func sendImagesFromZip(client *telegram.Client, chatID string, topicID *int, zipPath string, groupSize int, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, zipPasswords []string, logZipPasswords bool, maxDimension int, maxBytes int, pngStartLevel int, retry telegram.RetryConfig) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return
	}
	defer archive.Close()

	names := []string{}
	filesByName := map[string]*zip.File{}
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
		if isImage(name) {
			names = append(names, name)
			filesByName[name] = file
		}
	}
	if len(names) == 0 {
		log.Printf("no images found in %s", zipPath)
		return
	}

	zipOpts := ziputil.ReadOptions{LogPasswords: logZipPasswords}

	startedAt := time.Now()
	_ = client.SendMessage(
		chatID,
		fmt.Sprintf(
			"Starting image upload from %s: %d file(s) at %s",
			filepath.Base(zipPath),
			len(names),
			formatTimestamp(startedAt),
		),
		topicID,
		retry,
	)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	rangeStart, rangeEnd := clampRange(minIndex, maxIndex, len(names))
	total := rangeEnd - rangeStart
	progressState := newProgressTracker(total, "image")
	media := []telegram.MediaFile{}
	batchBytes := int64(0)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)
	readErrors := 0
	var lastReadErr error

	for idx, name := range names {
		if idx < rangeStart {
			continue
		}
		if idx >= rangeEnd {
			break
		}
		file := filesByName[name]
		if file == nil {
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		data, err := ziputil.ReadFileWithOptions(file, zipPasswords, zipOpts)
		if err != nil {
			readErrors++
			lastReadErr = err
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		sourceBytes := int64(len(data))
		prepared, err := prepareImageMedia(data, filepath.Base(name), maxDimension, maxBytes, pngStartLevel)
		if err != nil {
			log.Printf("invalid image %s: %v", filepath.Base(name), err)
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		media = append(media, prepared)
		batchBytes += sourceBytes
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
				skipped += len(media)
			} else {
				sent += len(media)
				sentBytes += batchBytes
			}
			processed += len(media)
			progressState.Print(processed, sent, skipped, false)
			media = media[:0]
			batchBytes = 0
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
			skipped += len(media)
		} else {
			sent += len(media)
			sentBytes += batchBytes
		}
		processed += len(media)
	}
	progressState.Print(processed, sent, skipped, true)
	if sent == 0 && readErrors > 0 {
		log.Printf("zip read failed for all entries: %s (last=%v)", zipPath, lastReadErr)
	}

	finishedAt := time.Now()
	elapsed := finishedAt.Sub(startedAt)
	avgPer := time.Duration(0)
	if sent > 0 {
		avgPer = elapsed / time.Duration(sent)
	}
	_ = client.SendMessage(
		chatID,
		fmt.Sprintf(
			"Completed image upload from %s at %s (elapsed %s, avg/image %s, total %s, avg %s, sent %d, skipped %d)",
			filepath.Base(zipPath),
			formatTimestamp(finishedAt),
			formatDuration(elapsed),
			formatDuration(avgPer),
			formatBytes(sentBytes),
			formatSpeed(sentBytes, elapsed),
			sent,
			skipped,
		),
		topicID,
		retry,
	)
	printSummary("image", filepath.Base(zipPath), startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}

func matchesExclude(rel string, patterns []string) bool {
	rel = path.Clean(filepath.ToSlash(rel))
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

func matchesInclude(rel string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	rel = path.Clean(filepath.ToSlash(rel))
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

func prepareImageMedia(data []byte, filename string, maxDimension int, maxBytes int, pngStartLevel int) (telegram.MediaFile, error) {
	if maxDimension <= 0 && maxBytes <= 0 {
		return telegram.MediaFile{Filename: filename, Data: data}, nil
	}
	if maxBytes <= 0 {
		maxBytes = maxInt()
	}
	result, err := imageutil.Prepare(data, filename, maxDimension, maxBytes, pngStartLevel)
	if err != nil {
		return telegram.MediaFile{}, err
	}
	return telegram.MediaFile{Filename: result.Filename, Data: result.Data}, nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func printSummary(kind string, source string, startedAt time.Time, finishedAt time.Time, elapsed time.Duration, sent int, skipped int, bytes int64) {
	avgPer := time.Duration(0)
	if sent > 0 {
		avgPer = elapsed / time.Duration(sent)
	}
	fmt.Fprintf(
		os.Stdout,
		"Summary %s from %s: start=%s end=%s elapsed=%s avg=%s total=%s speed=%s sent=%d skipped=%d\n",
		kind,
		source,
		formatTimestamp(startedAt),
		formatTimestamp(finishedAt),
		formatDuration(elapsed),
		formatDuration(avgPer),
		formatBytes(bytes),
		formatSpeed(bytes, elapsed),
		sent,
		skipped,
	)
}
