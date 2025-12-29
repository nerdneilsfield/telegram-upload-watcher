package sender

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	imageutil "github.com/nerdneilsfield/telegram-upload-watcher/go/internal/image"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/runcontrol"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/ziputil"
	zip "github.com/yeka/zip"
)

type Config struct {
	ChatID        string
	TopicID       *int
	GroupSize     int
	SendInterval  time.Duration
	BatchDelay    time.Duration
	PauseEvery    int
	PauseSeconds  time.Duration
	MaxDimension  int
	MaxBytes      int
	PNGStartLevel int
	Retry         telegram.RetryConfig
	ZipPasswords  []string
}

func Loop(cfg Config, q *queue.Queue, client *telegram.Client) {
	sentSincePause := 0
	for {
		pending := q.Pending(0)
		if len(pending) == 0 {
			time.Sleep(cfg.SendInterval)
			continue
		}

		for i := 0; i < len(pending); {
			item := pending[i]
			sendType := item.SendType
			if sendType == "" {
				sendType = "image"
			}

			sent := 0
			if sendType == "image" {
				group := []*queue.Item{}
				for i < len(pending) && len(group) < cfg.GroupSize {
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
				sent = sendImageGroup(cfg, q, client, group)
			} else {
				sent = sendSingle(cfg, q, client, item, sendType)
				i++
			}
			sentSincePause += sent
			time.Sleep(cfg.BatchDelay)

			if cfg.PauseEvery > 0 && sentSincePause >= cfg.PauseEvery && cfg.PauseSeconds > 0 {
				log.Printf("pausing sender for %s after %d images", cfg.PauseSeconds, sentSincePause)
				time.Sleep(cfg.PauseSeconds)
				sentSincePause = 0
			}
		}

		time.Sleep(cfg.SendInterval)
	}
}

type ProgressUpdate struct {
	Status         string `json:"status"`
	CurrentFile    string `json:"current_file"`
	RemainingFiles int    `json:"remaining_files"`
	TotalFiles     int    `json:"total_files"`
	CompletedFiles int    `json:"completed_files"`
	PerFileMS      int64  `json:"per_file_ms"`
	ETAMS          int64  `json:"eta_ms"`
}

type ProgressReporter func(update ProgressUpdate)

func LoopWithContext(
	ctx context.Context,
	cfg Config,
	q *queue.Queue,
	client *telegram.Client,
	pause *runcontrol.PauseGate,
	report ProgressReporter,
) {
	sentSincePause := 0
	var avgPerFileMS int64
	for {
		if pause != nil && !pause.Wait(ctx) {
			return
		}
		pending := q.Pending(0)
		if len(pending) == 0 {
			if report != nil {
				report(ProgressUpdate{Status: "idle"})
			}
			if !sleepWithContext(ctx, cfg.SendInterval) {
				return
			}
			continue
		}

		for i := 0; i < len(pending); {
			if pause != nil && !pause.Wait(ctx) {
				return
			}
			item := pending[i]
			sendType := item.SendType
			if sendType == "" {
				sendType = "image"
			}

			start := time.Now()
			sent := 0
			perFileMS := int64(0)
			if sendType == "image" {
				group := []*queue.Item{}
				for i < len(pending) && len(group) < cfg.GroupSize {
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
				sent = sendImageGroup(cfg, q, client, group)
				if len(group) > 0 {
					perFileMS = time.Since(start).Milliseconds() / int64(len(group))
					reportProgress(report, group[len(group)-1], q, perFileMS, &avgPerFileMS, "sending")
				}
			} else {
				sent = sendSingle(cfg, q, client, item, sendType)
				perFileMS = time.Since(start).Milliseconds()
				reportProgress(report, item, q, perFileMS, &avgPerFileMS, "sending")
				i++
			}
			sentSincePause += sent
			if !sleepWithContext(ctx, cfg.BatchDelay) {
				return
			}

			if cfg.PauseEvery > 0 && sentSincePause >= cfg.PauseEvery && cfg.PauseSeconds > 0 {
				log.Printf("pausing sender for %s after %d images", cfg.PauseSeconds, sentSincePause)
				if !sleepWithContext(ctx, cfg.PauseSeconds) {
					return
				}
				sentSincePause = 0
			}
		}

		if !sleepWithContext(ctx, cfg.SendInterval) {
			return
		}
	}
}

func reportProgress(report ProgressReporter, item *queue.Item, q *queue.Queue, perFileMS int64, avg *int64, status string) {
	if report == nil || item == nil {
		return
	}
	if perFileMS > 0 {
		if *avg == 0 {
			*avg = perFileMS
		} else {
			*avg = (*avg*7 + perFileMS) / 8
		}
	}
	remaining := len(q.Pending(0))
	stats := q.Stats()
	total := 0
	for _, count := range stats {
		total += count
	}
	completed := total - (stats[queue.StatusQueued] + stats[queue.StatusFailed] + stats[queue.StatusSending])
	if completed < 0 {
		completed = 0
	}
	eta := int64(remaining) * *avg
	report(ProgressUpdate{
		Status:         status,
		CurrentFile:    displayName(item),
		RemainingFiles: remaining,
		TotalFiles:     total,
		CompletedFiles: completed,
		PerFileMS:      *avg,
		ETAMS:          eta,
	})
}

func displayName(item *queue.Item) string {
	if item == nil {
		return ""
	}
	if item.SourceType == "zip" && item.InnerPath != nil {
		return fmt.Sprintf("%s:%s", filepath.Base(item.Path), *item.InnerPath)
	}
	return filepath.Base(item.Path)
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

func sendImageGroup(cfg Config, q *queue.Queue, client *telegram.Client, items []*queue.Item) int {
	mediaFiles := []telegram.MediaFile{}
	itemRefs := []*queue.Item{}

	for _, item := range items {
		if err := q.UpdateStatus(item.ID, queue.StatusSending, nil); err != nil {
			continue
		}
		data, filename, err := loadItem(item, cfg.ZipPasswords)
		if err != nil {
			msg := err.Error()
			q.UpdateStatus(item.ID, queue.StatusFailed, &msg)
			continue
		}
		result, err := imageutil.Prepare(data, filename, cfg.MaxDimension, cfg.MaxBytes, cfg.PNGStartLevel)
		if err != nil {
			msg := err.Error()
			q.UpdateStatus(item.ID, queue.StatusFailed, &msg)
			continue
		}
		mediaFiles = append(mediaFiles, telegram.MediaFile{Filename: result.Filename, Data: result.Data})
		itemRefs = append(itemRefs, item)
	}

	if len(mediaFiles) == 0 {
		return 0
	}

	if err := client.SendMediaGroup(cfg.ChatID, mediaFiles, cfg.TopicID, cfg.Retry); err != nil {
		msg := err.Error()
		for _, item := range itemRefs {
			q.UpdateStatus(item.ID, queue.StatusFailed, &msg)
		}
		return 0
	}

	for _, item := range itemRefs {
		q.UpdateStatus(item.ID, queue.StatusSent, nil)
	}
	return len(itemRefs)
}

func sendSingle(cfg Config, q *queue.Queue, client *telegram.Client, item *queue.Item, sendType string) int {
	if err := q.UpdateStatus(item.ID, queue.StatusSending, nil); err != nil {
		return 0
	}
	data, filename, err := loadItem(item, cfg.ZipPasswords)
	if err != nil {
		msg := err.Error()
		q.UpdateStatus(item.ID, queue.StatusFailed, &msg)
		return 0
	}

	var sendErr error
	file := telegram.MediaFile{Filename: filename, Data: data}
	switch sendType {
	case "file":
		sendErr = client.SendDocument(cfg.ChatID, file, cfg.TopicID, cfg.Retry)
	case "video":
		sendErr = client.SendVideo(cfg.ChatID, file, cfg.TopicID, cfg.Retry)
	case "audio":
		sendErr = client.SendAudio(cfg.ChatID, file, cfg.TopicID, cfg.Retry)
	default:
		sendErr = fmt.Errorf("unsupported send type: %s", sendType)
	}
	if sendErr != nil {
		msg := sendErr.Error()
		q.UpdateStatus(item.ID, queue.StatusFailed, &msg)
		return 0
	}
	q.UpdateStatus(item.ID, queue.StatusSent, nil)
	return 1
}

func loadItem(item *queue.Item, zipPasswords []string) ([]byte, string, error) {
	switch item.SourceType {
	case "file":
		data, err := os.ReadFile(item.Path)
		if err != nil {
			return nil, "", err
		}
		return data, filepath.Base(item.Path), nil
	case "zip":
		archive, err := zip.OpenReader(item.Path)
		if err != nil {
			return nil, "", err
		}
		defer archive.Close()
		for _, file := range archive.File {
			if item.InnerPath == nil {
				continue
			}
			if filepath.ToSlash(file.Name) != filepath.ToSlash(*item.InnerPath) {
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
		return nil, "", fmt.Errorf("unsupported source type: %s", item.SourceType)
	}
}
