package notify

import (
	"fmt"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
)

type Config struct {
	Enabled      bool
	Interval     time.Duration
	NotifyOnIdle bool
}

func Loop(cfg Config, q *queue.Queue, client *telegram.Client, chatID string, topicID *int) {
	if !cfg.Enabled {
		return
	}

	start := time.Now()
	retry := telegram.RetryConfig{MaxRetries: 3, Delay: 3 * time.Second}
	_ = client.SendMessage(chatID, fmt.Sprintf("Watch started (elapsed %s)", formatElapsed(0)), topicID, retry)

	lastPending := -1
	for {
		time.Sleep(cfg.Interval)
		elapsed := formatElapsed(time.Since(start))
		stats := q.Stats()
		pending := stats[queue.StatusQueued] + stats[queue.StatusFailed]
		_ = client.SendMessage(
			chatID,
			fmt.Sprintf(
				"Watch status: elapsed %s, queued %d, sending %d, sent %d, failed %d",
				elapsed,
				stats[queue.StatusQueued],
				stats[queue.StatusSending],
				stats[queue.StatusSent],
				stats[queue.StatusFailed],
			),
			topicID,
			retry,
		)

		if cfg.NotifyOnIdle {
			if lastPending >= 0 && lastPending > 0 && pending == 0 {
				_ = client.SendMessage(chatID, fmt.Sprintf("Watch idle (elapsed %s)", elapsed), topicID, retry)
			}
			lastPending = pending
		}
	}
}

func formatElapsed(duration time.Duration) string {
	total := int(duration.Seconds())
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
