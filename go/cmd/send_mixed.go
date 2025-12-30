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

type mixedSelection struct {
	withImage bool
	withVideo bool
	withAudio bool
	withFile  bool
}

func resolveMixedSelection(withImage bool, withVideo bool, withAudio bool, withFile bool) mixedSelection {
	if !withImage && !withVideo && !withAudio && !withFile {
		return mixedSelection{
			withImage: true,
			withVideo: true,
			withAudio: true,
			withFile:  true,
		}
	}
	return mixedSelection{
		withImage: withImage,
		withVideo: withVideo,
		withAudio: withAudio,
		withFile:  withFile,
	}
}

func mixedSendType(name string, sel mixedSelection) string {
	lower := strings.ToLower(name)
	if sel.withImage && isImage(lower) {
		return "image"
	}
	if sel.withVideo && matchesExt(lower, constants.VideoExtensions) {
		return "video"
	}
	if sel.withAudio && matchesExt(lower, constants.AudioExtensions) {
		return "audio"
	}
	if sel.withFile {
		return "file"
	}
	return ""
}

func newSendMixedCmd() *cobra.Command {
	cfg := &commonFlags{}
	filePaths := &stringSlice{}
	dirPaths := &stringSlice{}
	zipPaths := &stringSlice{}
	var groupSize int
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
	var withImage bool
	var withVideo bool
	var withAudio bool
	var withFile bool
	var queueFile string
	var queueRetries int

	cmd := &cobra.Command{
		Use:          "send-mixed",
		Short:        "Send mixed media from files, directories, or zips",
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
			selection := resolveMixedSelection(withImage, withVideo, withAudio, withFile)
			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}

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
						Command:       "send-mixed",
						ChatID:        cfg.chatID,
						TopicID:       topicPtr(cfg),
						Files:         resolvedFiles,
						Dirs:          resolvedDirs,
						ZipFiles:      resolvedZips,
						Include:       includes.Values(),
						Exclude:       excludes.Values(),
						GroupSize:     groupSize,
						BatchDelay:    batchDelay,
						EnableZip:     enableZip,
						WithImage:     selection.withImage,
						WithVideo:     selection.withVideo,
						WithAudio:     selection.withAudio,
						WithFile:      selection.withFile,
						QueueRetries:  queueRetries,
						MaxRetries:    cfg.maxRetries,
						RetryDelay:    cfg.retryDelaySec,
						MaxDimension:  maxDimension,
						MaxBytes:      maxBytes,
						PNGStartLevel: pngStartLevel,
					},
				}
				q, err := queue.New(queueFile, meta)
				if err != nil {
					return err
				}
				defer q.Close()

				if len(resolvedFiles) > 0 {
					for _, filePath := range resolvedFiles {
						if _, err := os.Stat(filePath); err != nil {
							return err
						}
					}
					enqueueMixedFromPaths(q, resolvedFiles, selection, includes.Values(), excludes.Values(), true, enableZip, zipPasswords)
				}
				for _, dirPath := range resolvedDirs {
					if _, err := os.Stat(dirPath); err != nil {
						return err
					}
					files := collectSourceFiles(dirPath, includes.Values(), excludes.Values())
					enqueueMixedFromPaths(q, files, selection, includes.Values(), excludes.Values(), false, enableZip, zipPasswords)
				}
				for _, zipPath := range resolvedZips {
					if _, err := os.Stat(zipPath); err != nil {
						return err
					}
					enqueueZipMixed(q, zipPath, selection, includes.Values(), excludes.Values(), zipPasswords)
				}

				pending := q.PendingWithAttempts(0, queueRetries)
				if len(pending) == 0 {
					log.Printf("no queued items to send")
					return nil
				}

				startedAt := time.Now()
				_ = client.SendMessage(
					cfg.chatID,
					fmt.Sprintf("Starting mixed upload from queue: %d file(s) at %s", len(pending), formatTimestamp(startedAt)),
					topicPtr(cfg),
					retry,
				)

				sent, skipped, sentBytes := drainQueue(client, q, "mixed", queueSendConfig{
					chatID:          cfg.chatID,
					topicID:         topicPtr(cfg),
					groupSize:       groupSize,
					batchDelay:      time.Duration(batchDelay) * time.Second,
					maxDimension:    maxDimension,
					maxBytes:        maxBytes,
					pngStartLevel:   pngStartLevel,
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
						"Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)",
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
				printSummary("mixed", queueFile, startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
				return nil
			}

			if len(filePaths.Values()) > 0 {
				sendMixedFromPaths(
					client,
					cfg.chatID,
					topicPtr(cfg),
					"files",
					filePaths.Values(),
					selection,
					groupSize,
					time.Duration(batchDelay)*time.Second,
					includes.Values(),
					excludes.Values(),
					true,
					enableZip,
					zipPasswords,
					logZipPasswords,
					maxDimension,
					maxBytes,
					pngStartLevel,
					retry,
				)
			}
			for _, dirPath := range dirPaths.Values() {
				sendMixedFromDir(
					client,
					cfg.chatID,
					topicPtr(cfg),
					dirPath,
					selection,
					groupSize,
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
			for _, zipPath := range zipPaths.Values() {
				sendMixedFromZip(
					client,
					cfg.chatID,
					topicPtr(cfg),
					zipPath,
					selection,
					groupSize,
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
	flags.Var(filePaths, "file", "File path (repeatable or comma-separated)")
	flags.Var(dirPaths, "dir", "Directory path (repeatable or comma-separated)")
	flags.Var(zipPaths, "zip-file", "Zip file path (repeatable or comma-separated)")
	flags.IntVar(&groupSize, "group-size", 4, "Images per media group")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between sends (seconds)")
	flags.BoolVar(&enableZip, "enable-zip", false, "Process zip files when scanning directories")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	flags.BoolVar(&logZipPasswords, "zip-pass-log", false, "Log zip passwords while checking (use with care)")
	flags.IntVar(&maxDimension, "max-dimension", 2000, "Max image dimension (0 to disable resize)")
	flags.IntVar(&maxBytes, "max-bytes", 5*1024*1024, "Max image size in bytes (0 to disable size limit)")
	flags.IntVar(&pngStartLevel, "png-start-level", 8, "PNG compression start level (0-9)")
	flags.BoolVar(&withImage, "with-image", false, "Send matching images (media groups)")
	flags.BoolVar(&withVideo, "with-video", false, "Send matching videos")
	flags.BoolVar(&withAudio, "with-audio", false, "Send matching audio files")
	flags.BoolVar(&withFile, "with-file", false, "Send other files as documents")
	flags.StringVar(&queueFile, "queue-file", "", "Path to JSONL queue file (enables resume mode)")
	flags.IntVar(&queueRetries, "queue-retries", 3, "Maximum queue retry attempts per item")
	return cmd
}

type mixedEntry struct {
	path    string
	isZip   bool
	sendTyp string
}

func sendMixedFromDir(client *telegram.Client, chatID string, topicID *int, dir string, sel mixedSelection, groupSize int, delay time.Duration, include []string, exclude []string, enableZip bool, zipPasswords []string, logZipPasswords bool, maxDimension int, maxBytes int, pngStartLevel int, retry telegram.RetryConfig) {
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
	if len(files) == 0 {
		log.Printf("no matching files found in %s", dir)
		return
	}
	sendMixedFromPaths(
		client,
		chatID,
		topicID,
		dir,
		files,
		sel,
		groupSize,
		delay,
		include,
		exclude,
		false,
		enableZip,
		zipPasswords,
		logZipPasswords,
		maxDimension,
		maxBytes,
		pngStartLevel,
		retry,
	)
}

func sendMixedFromPaths(client *telegram.Client, chatID string, topicID *int, sourceLabel string, paths []string, sel mixedSelection, groupSize int, delay time.Duration, include []string, exclude []string, applyFilters bool, enableZip bool, zipPasswords []string, logZipPasswords bool, maxDimension int, maxBytes int, pngStartLevel int, retry telegram.RetryConfig) {
	entries := []mixedEntry{}
	for _, path := range paths {
		rel := filepath.Base(path)
		if applyFilters && include != nil && !matchesInclude(rel, include) {
			continue
		}
		if applyFilters && exclude != nil && matchesExclude(rel, exclude) {
			continue
		}
		if enableZip && strings.HasSuffix(strings.ToLower(path), ".zip") {
			entries = append(entries, mixedEntry{path: path, isZip: true})
			continue
		}
		sendType := mixedSendType(rel, sel)
		if sendType == "" {
			continue
		}
		entries = append(entries, mixedEntry{path: path, sendTyp: sendType})
	}
	if len(entries) == 0 {
		log.Printf("no matching files found in %s", sourceLabel)
		return
	}

	startedAt := time.Now()
	_ = client.SendMessage(
		chatID,
		fmt.Sprintf("Starting mixed upload from %s: %d file(s) at %s", sourceLabel, len(entries), formatTimestamp(startedAt)),
		topicID,
		retry,
	)

	progressState := newProgressTracker(len(entries), "mixed")
	media := []telegram.MediaFile{}
	batchBytes := int64(0)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)

	flushImages := func() {
		if len(media) == 0 {
			return
		}
		batchCount := len(media)
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
			skipped += len(media)
		} else {
			sent += len(media)
			sentBytes += batchBytes
		}
		processed += batchCount
		progressState.Print(processed, sent, skipped, false)
		media = media[:0]
		batchBytes = 0
		time.Sleep(delay)
	}

	for _, entry := range entries {
		if entry.isZip {
			flushImages()
			sendMixedFromZip(
				client,
				chatID,
				topicID,
				entry.path,
				sel,
				groupSize,
				delay,
				include,
				exclude,
				zipPasswords,
				logZipPasswords,
				maxDimension,
				maxBytes,
				pngStartLevel,
				retry,
			)
			processed++
			progressState.Print(processed, sent, skipped, false)
			continue
		}

		if entry.sendTyp == "image" {
			data, err := os.ReadFile(entry.path)
			if err != nil {
				skipped++
				processed++
				progressState.Print(processed, sent, skipped, false)
				continue
			}
			prepared, err := prepareImageMedia(data, filepath.Base(entry.path), maxDimension, maxBytes, pngStartLevel)
			if err != nil {
				log.Printf("invalid image %s: %v", filepath.Base(entry.path), err)
				skipped++
				processed++
				progressState.Print(processed, sent, skipped, false)
				continue
			}
			media = append(media, prepared)
			batchBytes += int64(len(prepared.Data))
			if len(media) >= groupSize {
				flushImages()
			}
			continue
		}

		flushImages()
		data, err := os.ReadFile(entry.path)
		if err != nil {
			skipped++
			processed++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		sourceBytes := int64(len(data))
		if err := sendSingleFile(client, chatID, topicID, entry.sendTyp, filepath.Base(entry.path), data, retry); err != nil {
			log.Printf("send failed: %v", err)
			skipped++
		} else {
			sent++
			sentBytes += sourceBytes
		}
		processed++
		progressState.Print(processed, sent, skipped, false)
		time.Sleep(delay)
	}

	flushImages()
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
			"Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)",
			sourceLabel,
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
	printSummary("mixed", sourceLabel, startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}

func sendMixedFromZip(client *telegram.Client, chatID string, topicID *int, zipPath string, sel mixedSelection, groupSize int, delay time.Duration, include []string, exclude []string, zipPasswords []string, logZipPasswords bool, maxDimension int, maxBytes int, pngStartLevel int, retry telegram.RetryConfig) {
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
		names = append(names, name)
		filesByName[name] = file
	}
	if len(names) == 0 {
		log.Printf("no matching files found in %s", zipPath)
		return
	}

	startedAt := time.Now()
	_ = client.SendMessage(
		chatID,
		fmt.Sprintf(
			"Starting mixed upload from %s: %d file(s) at %s",
			filepath.Base(zipPath),
			len(names),
			formatTimestamp(startedAt),
		),
		topicID,
		retry,
	)

	zipOpts := ziputil.ReadOptions{LogPasswords: logZipPasswords}
	progressState := newProgressTracker(len(names), "mixed")
	media := []telegram.MediaFile{}
	batchBytes := int64(0)
	processed := 0
	sent := 0
	skipped := 0
	sentBytes := int64(0)

	flushImages := func() {
		if len(media) == 0 {
			return
		}
		batchCount := len(media)
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
			skipped += len(media)
		} else {
			sent += len(media)
			sentBytes += batchBytes
		}
		processed += batchCount
		progressState.Print(processed, sent, skipped, false)
		media = media[:0]
		batchBytes = 0
		time.Sleep(delay)
	}

	for _, name := range names {
		file := filesByName[name]
		if file == nil {
			skipped++
			processed++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		sendType := mixedSendType(name, sel)
		if sendType == "" {
			continue
		}
		if sendType == "image" {
			data, err := ziputil.ReadFileWithOptions(file, zipPasswords, zipOpts)
			if err != nil {
				skipped++
				processed++
				progressState.Print(processed, sent, skipped, false)
				continue
			}
			prepared, err := prepareImageMedia(data, filepath.Base(name), maxDimension, maxBytes, pngStartLevel)
			if err != nil {
				log.Printf("invalid image %s: %v", filepath.Base(name), err)
				skipped++
				processed++
				progressState.Print(processed, sent, skipped, false)
				continue
			}
			media = append(media, prepared)
			batchBytes += int64(len(prepared.Data))
			if len(media) >= groupSize {
				flushImages()
			}
			continue
		}

		flushImages()
		data, err := ziputil.ReadFileWithOptions(file, zipPasswords, zipOpts)
		if err != nil {
			skipped++
			processed++
			progressState.Print(processed, sent, skipped, false)
			continue
		}
		sourceBytes := int64(len(data))
		if err := sendSingleFile(client, chatID, topicID, sendType, filepath.Base(name), data, retry); err != nil {
			log.Printf("send failed: %v", err)
			skipped++
		} else {
			sent++
			sentBytes += sourceBytes
		}
		processed++
		progressState.Print(processed, sent, skipped, false)
		time.Sleep(delay)
	}

	flushImages()
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
			"Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)",
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
	printSummary("mixed", filepath.Base(zipPath), startedAt, finishedAt, elapsed, sent, skipped, sentBytes)
}
