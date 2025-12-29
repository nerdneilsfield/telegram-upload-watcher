package cmd

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/ziputil"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
	"github.com/spf13/cobra"
	zip "github.com/yeka/zip"
)

func newSendImagesCmd() *cobra.Command {
	cfg := &commonFlags{}
	var imageDir string
	var zipFile string
	var groupSize int
	var startIndex int
	var endIndex int
	var batchDelay int
	var enableZip bool
	includes := &stringSlice{}
	excludes := &stringSlice{}
	zipPasses := &stringSlice{}
	var zipPassFile string

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
			if imageDir == "" && zipFile == "" {
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
			if imageDir != "" {
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
					retry,
				)
			}
			if zipFile != "" {
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
					retry,
				)
			}
			return nil
		},
	}

	bindCommonFlags(cmd, cfg)
	flags := cmd.Flags()
	flags.StringVar(&imageDir, "image-dir", "", "Image directory")
	flags.StringVar(&zipFile, "zip-file", "", "Zip file path")
	flags.IntVar(&groupSize, "group-size", 4, "Images per media group")
	flags.IntVar(&startIndex, "start-index", 0, "Start group index (0-based)")
	flags.IntVar(&endIndex, "end-index", 0, "End group index (0 for no limit)")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between media groups (seconds)")
	flags.BoolVar(&enableZip, "enable-zip", false, "Process zip files when scanning directories")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	return cmd
}

func sendImagesFromDir(client *telegram.Client, chatID string, topicID *int, dir string, groupSize int, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, enableZip bool, zipPasswords []string, retry telegram.RetryConfig) {
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

	_ = client.SendMessage(chatID, fmt.Sprintf("Starting image upload: %d file(s)", len(files)), topicID, retry)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize

	media := []telegram.MediaFile{}
	for idx, path := range files {
		if idx < minIndex {
			continue
		}
		if endIndex > 0 && idx >= maxIndex {
			break
		}
		if strings.HasSuffix(strings.ToLower(path), ".zip") {
			sendImagesFromZip(client, chatID, topicID, path, groupSize, 0, 0, delay, include, exclude, zipPasswords, retry)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		media = append(media, telegram.MediaFile{Filename: filepath.Base(path), Data: data})
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
			}
			media = media[:0]
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
		}
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed image upload from %s", dir), topicID, retry)
}

func sendImagesFromZip(client *telegram.Client, chatID string, topicID *int, zipPath string, groupSize int, startIndex int, endIndex int, delay time.Duration, include []string, exclude []string, zipPasswords []string, retry telegram.RetryConfig) {
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

	first := filesByName[names[0]]
	if first != nil {
		if _, err := ziputil.ReadFile(first, zipPasswords); err != nil {
			log.Printf(
				"zip password check failed: %s (file=%s, encrypted=%t, flags=0x%x, method=%d, comp=%d, uncomp=%d, crc=0x%x, err=%v)",
				zipPath,
				first.Name,
				first.IsEncrypted(),
				first.Flags,
				first.Method,
				first.CompressedSize64,
				first.UncompressedSize64,
				first.CRC32,
				err,
			)
			_ = client.SendMessage(chatID, fmt.Sprintf("Skipping zip (passwords failed): %s", filepath.Base(zipPath)), topicID, retry)
			return
		}
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Starting image upload: %d file(s)", len(names)), topicID, retry)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	media := []telegram.MediaFile{}

	for idx, name := range names {
		if idx < minIndex {
			continue
		}
		if endIndex > 0 && idx >= maxIndex {
			break
		}
		file := filesByName[name]
		if file == nil {
			continue
		}
		data, err := ziputil.ReadFile(file, zipPasswords)
		if err != nil {
			continue
		}
		media = append(media, telegram.MediaFile{Filename: filepath.Base(name), Data: data})
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
			}
			media = media[:0]
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
		}
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed image upload from %s", filepath.Base(zipPath)), topicID, retry)
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
