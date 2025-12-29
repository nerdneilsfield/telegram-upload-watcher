package cmd

import (
	"archive/zip"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	var filePath string
	var dirPath string
	var zipPath string
	var startIndex int
	var endIndex int
	var batchDelay int
	var enableZip bool
	includes := &stringSlice{}
	excludes := &stringSlice{}
	zipPasses := &stringSlice{}
	var zipPassFile string
	var logZipPasswords bool

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
			if filePath == "" && dirPath == "" && zipPath == "" {
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

			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
			if filePath != "" {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return err
				}
				filename := filepath.Base(filePath)
				if err := sendSingleFile(client, cfg.chatID, topicPtr(cfg), sendType, filename, data, retry); err != nil {
					return err
				}
			}
			if dirPath != "" {
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
			if zipPath != "" {
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
	flags.StringVar(&filePath, "file", "", "File path")
	flags.StringVar(&dirPath, "dir", "", "Directory path")
	flags.StringVar(&zipPath, "zip-file", "", "Zip file path")
	flags.IntVar(&startIndex, "start-index", 0, "Start index (0-based)")
	flags.IntVar(&endIndex, "end-index", 0, "End index (0 for no limit)")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between sends (seconds)")
	flags.BoolVar(&enableZip, "enable-zip", false, "Process zip files when scanning directories")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	flags.BoolVar(&logZipPasswords, "zip-pass-log", false, "Log zip passwords while checking (use with care)")
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
	_ = client.SendMessage(chatID, fmt.Sprintf("Starting %s upload: %d file(s)", label, len(files)), topicID, retry)

	minIndex := startIndex
	maxIndex := endIndex
	for idx, path := range files {
		if idx < minIndex {
			continue
		}
		if endIndex > 0 && idx >= maxIndex {
			break
		}
		if strings.HasSuffix(strings.ToLower(path), ".zip") && enableZip {
			sendFilesFromZip(client, chatID, topicID, path, sendType, 0, 0, delay, include, exclude, zipPasswords, logZipPasswords, retry)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := sendSingleFile(client, chatID, topicID, sendType, filepath.Base(path), data, retry); err != nil {
			log.Printf("send failed: %v", err)
		}
		time.Sleep(delay)
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed %s upload from %s", label, dir), topicID, retry)
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
	_ = client.SendMessage(chatID, fmt.Sprintf("Starting %s upload: %d file(s)", label, len(names)), topicID, retry)

	minIndex := startIndex
	maxIndex := endIndex
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
		data, err := ziputil.ReadFileWithOptions(file, zipPasswords, zipOpts)
		if err != nil {
			continue
		}
		if err := sendSingleFile(client, chatID, topicID, sendType, filepath.Base(name), data, retry); err != nil {
			log.Printf("send failed: %v", err)
		}
		time.Sleep(delay)
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed %s upload from %s", label, filepath.Base(zipPath)), topicID, retry)
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
