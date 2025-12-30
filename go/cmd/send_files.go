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
	"github.com/spf13/cobra"
)

func newSendFileCmd() *cobra.Command {
	return newSendFilesCmd("send-file", "Send files using sendDocument", "file")
}

func newSendVideoCmd() *cobra.Command {
	return newSendFilesCmd("send-video", "Send videos using sendVideo", "video")
}

func newSendAudioCmd() *cobra.Command {
	return newSendFilesCmd("send-audio", "Send audio using sendAudio", "audio")
}

func newSendFilesCmd(use string, short string, sendType string) *cobra.Command {
	cfg := &commonFlags{}
	filePaths := &stringSlice{}
	dirPaths := &stringSlice{}
	zipPaths := &stringSlice{}
	var startIndex int
	var endIndex int
	var batchDelay int
	var enableZip bool
	includes := &stringSlice{}
	excludes := &stringSlice{}
	zipPasses := &stringSlice{}
	var zipPassFile string
	var logZipPasswords bool
	var queueFile string
	var queueRetries int

	cmd := &cobra.Command{
		Use:          use,
		Short:        short,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryDelay = time.Duration(cfg.retryDelaySec) * time.Second
			if cfg.chatID == "" {
				return fmt.Errorf("chat-id is required")
			}
			if len(filePaths.Values()) == 0 && len(dirPaths.Values()) == 0 && len(zipPaths.Values()) == 0 {
				return fmt.Errorf("file, dir, or zip-file is required")
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

			if queueFile != "" {
				queueRetries, err = validateQueueRetries(queueRetries)
				if err != nil {
					return err
				}
				resolvedFiles, err := resolveAbsPaths(filePaths.Values())
				if err != nil {
					return err
				}
				resolvedDirs, err := resolveAbsPaths(dirPaths.Values())
				if err != nil {
					return err
				}
				resolvedZips, err := resolveAbsPaths(zipPaths.Values())
				if err != nil {
					return err
				}
				meta := &queue.Meta{
					Params: queue.MetaParams{
						Command:      use,
						ChatID:       cfg.chatID,
						TopicID:      topicPtr(cfg),
						Files:        resolvedFiles,
						Dirs:         resolvedDirs,
						ZipFiles:     resolvedZips,
						Include:      includes.Values(),
						Exclude:      excludes.Values(),
						StartIndex:   startIndex,
						EndIndex:     endIndex,
						BatchDelay:   batchDelay,
						EnableZip:    enableZip,
						QueueRetries: queueRetries,
						MaxRetries:   cfg.maxRetries,
						RetryDelay:   cfg.retryDelaySec,
					},
				}
				q, err := queue.New(queueFile, meta)
				if err != nil {
					return err
				}
				defer q.Close()

				for _, filePath := range resolvedFiles {
					if _, err := os.Stat(filePath); err != nil {
						return err
					}
					enqueueFileItem(q, filePath, sendType)
				}
				for _, dirPath := range resolvedDirs {
					if _, err := os.Stat(dirPath); err != nil {
						return err
					}
					enqueueFilesFromDir(q, dirPath, sendType, includes.Values(), excludes.Values(), enableZip, startIndex, endIndex, zipPasswords)
				}
				for _, zipPath := range resolvedZips {
					if _, err := os.Stat(zipPath); err != nil {
						return err
					}
					enqueueZipFiles(q, zipPath, sendType, includes.Values(), excludes.Values(), startIndex, endIndex, zipPasswords)
				}

				pending := q.PendingWithAttempts(0, queueRetries)
				if len(pending) == 0 {
					log.Printf("no queued files to send")
					return nil
				}

				label := sendTypeLabel(sendType)
				startedAt := time.Now()
				retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
				_ = client.SendMessage(
					cfg.chatID,
					fmt.Sprintf("Starting %s upload from queue: %d file(s) at %s", label, len(pending), formatTimestamp(startedAt)),
					topicPtr(cfg),
					retry,
				)

				sent, skipped, sentBytes := drainQueue(client, q, label, queueSendConfig{
					chatID:          cfg.chatID,
					topicID:         topicPtr(cfg),
					groupSize:       1,
					batchDelay:      time.Duration(batchDelay) * time.Second,
					maxDimension:    0,
					maxBytes:        0,
					pngStartLevel:   0,
					retry:           retry,
					zipPasswords:    zipPasswords,
					logZipPasswords: logZipPasswords,
					queueRetries:    queueRetries,
				})

				finishedAt := time.Now()
				elapsed := finishedAt.Sub(startedAt)
				avgPer := time.Duration(0)
				if sent > 0 {
					avgPer = elapsed / time.Duration(sent)
				}
				_ = client.SendMessage(
					cfg.chatID,
					fmt.Sprintf(
						"Completed %s upload from %s at %s (elapsed %s, avg/file %s, total %s, avg %s, sent %d, skipped %d)",
						label,
						queueFile,
						formatTimestamp(finishedAt),
						formatDuration(elapsed),
						formatDuration(avgPer),
						formatBytes(sentBytes),
						formatSpeed(sentBytes, elapsed),
						sent,
						skipped,
					),
					topicPtr(cfg),
					retry,
				)
				printSummary(label, queueFile, startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
				return nil
			}

			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
			for _, filePath := range filePaths.Values() {
				label := sendTypeLabel(sendType)
				progressState := newProgressTracker(1, label)
				startedAt := time.Now()
				_ = client.SendMessage(
					cfg.chatID,
					fmt.Sprintf("Starting %s upload: 1 file(s) at %s", label, formatTimestamp(startedAt)),
					topicPtr(cfg),
					retry,
				)
				data, err := os.ReadFile(filePath)
				if err != nil {
					progressState.Print(1, 0, 1, true)
					return err
				}
				filename := filepath.Base(filePath)
				if err := sendSingleFile(client, cfg.chatID, topicPtr(cfg), sendType, filename, data, retry); err != nil {
					progressState.Print(1, 0, 1, true)
					return err
				}
				progressState.Print(1, 1, 0, true)
				finishedAt := time.Now()
				elapsed := finishedAt.Sub(startedAt)
				avgPer := elapsed
				sentBytes := int64(len(data))
				_ = client.SendMessage(
					cfg.chatID,
					fmt.Sprintf(
						"Completed %s upload from %s at %s (elapsed %s, avg/file %s, total %s, avg %s, sent %d, skipped %d)",
						label,
						filename,
						formatTimestamp(finishedAt),
						formatDuration(elapsed),
						formatDuration(avgPer),
						formatBytes(sentBytes),
						formatSpeed(sentBytes, elapsed),
						1,
						0,
					),
					topicPtr(cfg),
					retry,
				)
				printSummary(label, filename, startedAt, finishedAt, elapsed, 1, 0, sentBytes)
			}
			for _, dirPath := range dirPaths.Values() {
				sendFilesFromDir(
					client,
					cfg.chatID,
					topicPtr(cfg),
					dirPath,
					sendType,
					startIndex,
					endIndex,
					time.Duration(batchDelay)*time.Second,
					includes.Values(),
					excludes.Values(),
					enableZip,
					zipPasswords,
					logZipPasswords,
					retry,
				)
			}
			for _, zipPath := range zipPaths.Values() {
				sendFilesFromZip(
					client,
					cfg.chatID,
					topicPtr(cfg),
					zipPath,
					sendType,
					startIndex,
					endIndex,
					time.Duration(batchDelay)*time.Second,
					includes.Values(),
					excludes.Values(),
					zipPasswords,
					logZipPasswords,
					retry,
				)
			}
			return nil
		},
	}

	bindCommonFlags(cmd, cfg)
	flags := cmd.Flags()
	flags.Var(filePaths, "file", "File path (repeatable or comma-separated)")
	flags.Var(dirPaths, "dir", "Directory path (repeatable or comma-separated)")
	flags.Var(zipPaths, "zip-file", "Zip file path (repeatable or comma-separated)")
	flags.IntVar(&startIndex, "start-index", 0, "Start index (0-based)")
	flags.IntVar(&endIndex, "end-index", 0, "End index (0 for no limit)")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between sends (seconds)")
	flags.BoolVar(&enableZip, "enable-zip", false, "Process zip files when scanning directories")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	flags.BoolVar(&logZipPasswords, "zip-pass-log", false, "Log zip passwords while checking (use with care)")
	flags.StringVar(&queueFile, "queue-file", "", "Path to JSONL queue file (enables resume mode)")
	flags.IntVar(&queueRetries, "queue-retries", 3, "Maximum queue retry attempts per item")
	return cmd
}

func sendFilesFromDir(client *telegram.Client, chatID string, topicID *int, dir string, sendType string, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, enableZip bool, zipPasswords []string, logZipPasswords bool, retry telegram.RetryConfig) {
	allowed := allowedExtsForType(sendType)
	files := collectFiles(dir, include, exclude, enableZip, allowed)
	if len(files) == 0 {
		log.Printf("no files found in %s", dir)
		return
	}

	label := sendTypeLabel(sendType)
	startedAt := time.Now()
	_ = client.SendMessage(chatID, fmt.Sprintf("Starting %s upload: %d file(s) at %s", label, len(files), formatTimestamp(startedAt)), topicID, retry)

	rangeStart, rangeEnd := clampRange(startIndex, endIndex, len(files))
	total := rangeEnd - rangeStart
	progressState := newProgressTracker(total, label)
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
		if strings.HasSuffix(strings.ToLower(path), ".zip") && enableZip {
			sendFilesFromZip(client, chatID, topicID, path, sendType, 0, 0, delay, include, exclude, zipPasswords, logZipPasswords, retry)
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
		if err := sendSingleFile(client, chatID, topicID, sendType, filepath.Base(path), data, retry); err != nil {
			log.Printf("send failed: %v", err)
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
		} else {
			processed++
			sent++
			sentBytes += int64(len(data))
			progressState.Print(processed, sent, skipped, false)
		}
		time.Sleep(delay)
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
			"Completed %s upload from %s at %s (elapsed %s, avg/file %s, total %s, avg %s, sent %d, skipped %d)",
			label,
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
	printSummary(label, dir, startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}

func sendFilesFromZip(client *telegram.Client, chatID string, topicID *int, zipPath string, sendType string, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, zipPasswords []string, logZipPasswords bool, retry telegram.RetryConfig) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return
	}
	defer archive.Close()

	allowed := allowedExtsForType(sendType)
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
		if allowed != nil && !matchesExt(name, allowed) {
			continue
		}
		names = append(names, name)
		filesByName[name] = file
	}
	if len(names) == 0 {
		log.Printf("no files found in %s", zipPath)
		return
	}

	zipOpts := ziputil.ReadOptions{LogPasswords: logZipPasswords}
	first := filesByName[names[0]]
	if first != nil {
		if _, err := ziputil.ReadFileWithOptions(first, zipPasswords, zipOpts); err != nil {
			log.Printf(
				"zip password check failed: %s (file=%s, encrypted=%t, flags=0x%x, method=%d, comp=%d, uncomp=%d, crc=0x%x, err=%v)",
				zipPath,
				first.Name,
				ziputil.IsEncrypted(first),
				first.Flags,
				ziputil.EffectiveMethod(first),
				first.CompressedSize64,
				first.UncompressedSize64,
				first.CRC32,
				err,
			)
			_ = client.SendMessage(chatID, fmt.Sprintf("Skipping zip (passwords failed): %s", filepath.Base(zipPath)), topicID, retry)
			return
		}
	}

	label := sendTypeLabel(sendType)
	startedAt := time.Now()
	_ = client.SendMessage(chatID, fmt.Sprintf("Starting %s upload: %d file(s) at %s", label, len(names), formatTimestamp(startedAt)), topicID, retry)

	rangeStart, rangeEnd := clampRange(startIndex, endIndex, len(names))
	total := rangeEnd - rangeStart
	progressState := newProgressTracker(total, label)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)
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
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		if err := sendSingleFile(client, chatID, topicID, sendType, filepath.Base(name), data, retry); err != nil {
			log.Printf("send failed: %v", err)
			processed++
			skipped++
			progressState.Print(processed, sent, skipped, false)
		} else {
			processed++
			sent++
			sentBytes += int64(len(data))
			progressState.Print(processed, sent, skipped, false)
		}
		time.Sleep(delay)
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
			"Completed %s upload from %s at %s (elapsed %s, avg/file %s, total %s, avg %s, sent %d, skipped %d)",
			label,
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
	printSummary(label, filepath.Base(zipPath), startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}

func collectFiles(root string, include []string, exclude []string, enableZip bool, allowedExts []string) []string {
	files := []string{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
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
		if strings.HasSuffix(nameLower, ".zip") {
			if enableZip || allowedExts == nil {
				files = append(files, path)
			}
			return nil
		}
		if allowedExts == nil || matchesExt(nameLower, allowedExts) {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func sendSingleFile(client *telegram.Client, chatID string, topicID *int, sendType string, filename string, data []byte, retry telegram.RetryConfig) error {
	file := telegram.MediaFile{Filename: filename, Data: data}
	switch sendType {
	case "file":
		return client.SendDocument(chatID, file, topicID, retry)
	case "video":
		return client.SendVideo(chatID, file, topicID, retry)
	case "audio":
		return client.SendAudio(chatID, file, topicID, retry)
	default:
		return fmt.Errorf("unsupported send type: %s", sendType)
	}
}

func allowedExtsForType(sendType string) []string {
	switch sendType {
	case "video":
		return constants.VideoExtensions
	case "audio":
		return constants.AudioExtensions
	case "file":
		return nil
	default:
		return nil
	}
}

func sendTypeLabel(sendType string) string {
	switch sendType {
	case "video":
		return "video"
	case "audio":
		return "audio"
	default:
		return "file"
	}
}

func matchesExt(name string, exts []string) bool {
	name = strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
