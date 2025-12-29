package cmd

import (
	"fmt"
	"time"
)

func formatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(time.Second).String()
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(size)
	idx := 0
	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f %s", value, units[idx])
}

func formatSpeed(bytes int64, elapsed time.Duration) string {
	if bytes <= 0 || elapsed <= 0 {
		return "0 B/s"
	}
	perSecond := float64(bytes) / elapsed.Seconds()
	return fmt.Sprintf("%s/s", formatBytes(int64(perSecond)))
}
