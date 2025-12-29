package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/gui"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/runcontrol"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/sender"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/ziputil"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
)

type SendImagesRequest struct {
	ImageDir      string `json:"image_dir"`
	ZipFile       string `json:"zip_file"`
	GroupSize     int    `json:"group_size"`
	StartIndex    int    `json:"start_index"`
	EndIndex      int    `json:"end_index"`
	BatchDelaySec int    `json:"batch_delay_sec"`
	EnableZip     bool   `json:"enable_zip"`
}

type SendFilesRequest struct {
	SendType      string `json:"send_type"`
	FilePath      string `json:"file_path"`
	DirPath       string `json:"dir_path"`
	ZipFile       string `json:"zip_file"`
	StartIndex    int    `json:"start_index"`
	EndIndex      int    `json:"end_index"`
	BatchDelaySec int    `json:"batch_delay_sec"`
	EnableZip     bool   `json:"enable_zip"`
}

type sendItem struct {
	sourceType string
	path       string
	innerPath  string
}

func sendImages(
	ctx context.Context,
	client *telegram.Client,
	settings SettingsBundle,
	req SendImagesRequest,
	pause *runcontrol.PauseGate,
	report func(sender.ProgressUpdate),
) error {
	if settings.Settings.ChatID == "" {
		return errors.New("chat_id is required")
	}
	if req.ImageDir == "" && req.ZipFile == "" {
		return errors.New("image_dir or zip_file is required")
	}
	zipPasswords, err := gui.LoadZipPasswords(settings.Settings.ZipPasswords, settings.Settings.ZipPassFile)
	if err != nil {
		return err
	}
	groupSize := req.GroupSize
	if groupSize <= 0 {
		groupSize = 4
	}
	items := []sendItem{}
	if req.ImageDir != "" {
		dirItems, err := collectImageItemsFromDir(req.ImageDir, settings.Settings.Include, settings.Settings.Exclude, req.EnableZip, zipPasswords)
		if err != nil {
			return err
		}
		items = append(items, dirItems...)
	}
	if req.ZipFile != "" {
		zipItems, err := collectImageItemsFromZip(req.ZipFile, settings.Settings.Include, settings.Settings.Exclude, zipPasswords)
		if err != nil {
			return err
		}
		items = append(items, zipItems...)
	}
	if len(items) == 0 {
		return errors.New("no images found")
	}

	start := req.StartIndex * groupSize
	end := req.EndIndex * groupSize
	if start > 0 && start < len(items) {
		items = items[start:]
	}
	if end > 0 && end < len(items) {
		items = items[:end]
	}
	if len(items) == 0 {
		return errors.New("no images found after applying range")
	}

	retry := telegram.RetryConfig{MaxRetries: 3, Delay: 3 * time.Second}
	_ = client.SendMessage(settings.Settings.ChatID, fmt.Sprintf("Starting image upload: %d file(s)", len(items)), settings.Settings.TopicID, retry)

	avgPerFile := int64(0)
	sent := 0
	batchDelay := time.Duration(req.BatchDelaySec) * time.Second
	for i := 0; i < len(items); {
		if pause != nil && !pause.Wait(ctx) {
			return ctx.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		group := []sendItem{}
		for i < len(items) && len(group) < groupSize {
			group = append(group, items[i])
			i++
		}

		startTime := time.Now()
		media := []telegram.MediaFile{}
		for _, item := range group {
			data, filename, err := loadSendItem(item, zipPasswords)
			if err != nil {
				log.Printf("failed to load image: %v", err)
				continue
			}
			media = append(media, telegram.MediaFile{Filename: filename, Data: data})
		}
		if len(media) == 0 {
			continue
		}
		if err := client.SendMediaGroup(settings.Settings.ChatID, media, settings.Settings.TopicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
		}
		perFile := time.Since(startTime).Milliseconds()
		if len(media) > 0 {
			perFile = perFile / int64(len(media))
		}
		sent += len(media)
		reportProgress(report, group[len(group)-1], len(items)-sent, len(items), sent, perFile, &avgPerFile, "sending")

		if !sleepWithContext(ctx, batchDelay) {
			return ctx.Err()
		}
	}

	reportProgress(report, items[len(items)-1], 0, len(items), sent, avgPerFile, &avgPerFile, "completed")
	_ = client.SendMessage(settings.Settings.ChatID, fmt.Sprintf("Completed image upload (%d file(s))", len(items)), settings.Settings.TopicID, retry)
	return nil
}

func sendFiles(
	ctx context.Context,
	client *telegram.Client,
	settings SettingsBundle,
	req SendFilesRequest,
	pause *runcontrol.PauseGate,
	report func(sender.ProgressUpdate),
) error {
	if settings.Settings.ChatID == "" {
		return errors.New("chat_id is required")
	}
	if req.FilePath == "" && req.DirPath == "" && req.ZipFile == "" {
		return errors.New("file_path, dir_path, or zip_file is required")
	}
	zipPasswords, err := gui.LoadZipPasswords(settings.Settings.ZipPasswords, settings.Settings.ZipPassFile)
	if err != nil {
		return err
	}
	sendType := req.SendType
	if sendType == "" {
		sendType = "file"
	}
	items := []sendItem{}
	if req.FilePath != "" {
		items = append(items, sendItem{sourceType: "file", path: req.FilePath})
	}
	if req.DirPath != "" {
		dirItems, err := collectFileItemsFromDir(req.DirPath, sendType, settings.Settings.Include, settings.Settings.Exclude, req.EnableZip, zipPasswords)
		if err != nil {
			return err
		}
		items = append(items, dirItems...)
	}
	if req.ZipFile != "" {
		zipItems, err := collectFileItemsFromZip(req.ZipFile, sendType, settings.Settings.Include, settings.Settings.Exclude, zipPasswords)
		if err != nil {
			return err
		}
		items = append(items, zipItems...)
	}
	if len(items) == 0 {
		return errors.New("no files found")
	}
	if req.StartIndex > 0 && req.StartIndex < len(items) {
		items = items[req.StartIndex:]
	}
	if req.EndIndex > 0 && req.EndIndex < len(items) {
		items = items[:req.EndIndex]
	}
	if len(items) == 0 {
		return errors.New("no files found after applying range")
	}

	retry := telegram.RetryConfig{MaxRetries: 3, Delay: 3 * time.Second}
	label := sendTypeLabel(sendType)
	_ = client.SendMessage(settings.Settings.ChatID, fmt.Sprintf("Starting %s upload: %d file(s)", label, len(items)), settings.Settings.TopicID, retry)

	avgPerFile := int64(0)
	sent := 0
	delay := time.Duration(req.BatchDelaySec) * time.Second
	for _, item := range items {
		if pause != nil && !pause.Wait(ctx) {
			return ctx.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		data, filename, err := loadSendItem(item, zipPasswords)
		if err != nil {
			log.Printf("failed to read file: %v", err)
			continue
		}
		if err := sendSingleFile(client, settings.Settings.ChatID, settings.Settings.TopicID, sendType, filename, data, retry); err != nil {
			log.Printf("send failed: %v", err)
		}
		perFile := time.Since(start).Milliseconds()
		sent++
		reportProgress(report, item, len(items)-sent, len(items), sent, perFile, &avgPerFile, "sending")
		if !sleepWithContext(ctx, delay) {
			return ctx.Err()
		}
	}

	reportProgress(report, items[len(items)-1], 0, len(items), sent, avgPerFile, &avgPerFile, "completed")
	_ = client.SendMessage(settings.Settings.ChatID, fmt.Sprintf("Completed %s upload (%d file(s))", label, len(items)), settings.Settings.TopicID, retry)
	return nil
}

func collectImageItemsFromDir(root string, include []string, exclude []string, enableZip bool, zipPasswords []string) ([]sendItem, error) {
	items := []sendItem{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !matchesInclude(rel, include) || matchesExclude(rel, exclude) {
			return nil
		}
		nameLower := strings.ToLower(path)
		if isImage(nameLower) {
			items = append(items, sendItem{sourceType: "file", path: path})
			return nil
		}
		if enableZip && strings.HasSuffix(nameLower, ".zip") {
			zipItems, err := collectImageItemsFromZip(path, include, exclude, zipPasswords)
			if err != nil {
				log.Printf("skipping zip: %v", err)
				return nil
			}
			items = append(items, zipItems...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func collectImageItemsFromZip(zipPath string, include []string, exclude []string, zipPasswords []string) ([]sendItem, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	items := []sendItem{}
	var first *zip.File
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(file.Name)
		if !matchesInclude(name, include) || matchesExclude(name, exclude) {
			continue
		}
		if isImage(name) {
			if first == nil {
				first = file
			}
			items = append(items, sendItem{sourceType: "zip", path: zipPath, innerPath: name})
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	if first != nil {
		if _, err := ziputil.ReadFile(first, zipPasswords); err != nil {
			return nil, fmt.Errorf("zip password check failed (%s): %w", filepath.Base(zipPath), err)
		}
	}
	return items, nil
}

func collectFileItemsFromDir(root string, sendType string, include []string, exclude []string, enableZip bool, zipPasswords []string) ([]sendItem, error) {
	items := []sendItem{}
	allowed := allowedExtsForType(sendType)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !matchesInclude(rel, include) || matchesExclude(rel, exclude) {
			return nil
		}
		nameLower := strings.ToLower(path)
		if strings.HasSuffix(nameLower, ".zip") {
			if enableZip || allowed == nil {
				zipItems, err := collectFileItemsFromZip(path, sendType, include, exclude, zipPasswords)
				if err != nil {
					log.Printf("skipping zip: %v", err)
					return nil
				}
				items = append(items, zipItems...)
			}
			return nil
		}
		if allowed == nil || matchesExt(nameLower, allowed) {
			items = append(items, sendItem{sourceType: "file", path: path})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func collectFileItemsFromZip(zipPath string, sendType string, include []string, exclude []string, zipPasswords []string) ([]sendItem, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	allowed := allowedExtsForType(sendType)
	items := []sendItem{}
	var first *zip.File
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(file.Name)
		if !matchesInclude(name, include) || matchesExclude(name, exclude) {
			continue
		}
		if allowed != nil && !matchesExt(name, allowed) {
			continue
		}
		if first == nil {
			first = file
		}
		items = append(items, sendItem{sourceType: "zip", path: zipPath, innerPath: name})
	}
	if len(items) == 0 {
		return nil, nil
	}
	if first != nil {
		if _, err := ziputil.ReadFile(first, zipPasswords); err != nil {
			return nil, fmt.Errorf("zip password check failed (%s): %w", filepath.Base(zipPath), err)
		}
	}
	return items, nil
}

func loadSendItem(item sendItem, zipPasswords []string) ([]byte, string, error) {
	switch item.sourceType {
	case "file":
		data, err := os.ReadFile(item.path)
		if err != nil {
			return nil, "", err
		}
		return data, filepath.Base(item.path), nil
	case "zip":
		archive, err := zip.OpenReader(item.path)
		if err != nil {
			return nil, "", err
		}
		defer archive.Close()
		for _, file := range archive.File {
			if filepath.ToSlash(file.Name) != filepath.ToSlash(item.innerPath) {
				continue
			}
			data, err := ziputil.ReadFile(file, zipPasswords)
			if err != nil {
				return nil, "", err
			}
			return data, filepath.Base(file.Name), nil
		}
		return nil, "", os.ErrNotExist
	default:
		return nil, "", fmt.Errorf("unsupported source type: %s", item.sourceType)
	}
}

func reportProgress(report func(sender.ProgressUpdate), item sendItem, remaining int, total int, completed int, perFileMS int64, avg *int64, status string) {
	if report == nil {
		return
	}
	if perFileMS > 0 {
		if *avg == 0 {
			*avg = perFileMS
		} else {
			*avg = (*avg*7 + perFileMS) / 8
		}
	}
	eta := int64(remaining) * *avg
	report(sender.ProgressUpdate{
		Status:         status,
		CurrentFile:    displayName(item),
		RemainingFiles: remaining,
		TotalFiles:     total,
		CompletedFiles: completed,
		PerFileMS:      *avg,
		ETAMS:          eta,
	})
}

func displayName(item sendItem) string {
	if item.sourceType == "zip" && item.innerPath != "" {
		return fmt.Sprintf("%s:%s", filepath.Base(item.path), item.innerPath)
	}
	return filepath.Base(item.path)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
