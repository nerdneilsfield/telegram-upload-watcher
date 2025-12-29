package cmd

import (
	"fmt"
	"os"
)

type progressTracker struct {
	enabled bool
	total   int
	label   string
}

func (p progressTracker) Print(processed int, sent int, skipped int, done bool) {
	if !p.enabled || p.total <= 0 {
		return
	}
	percent := float64(processed) / float64(p.total) * 100
	line := fmt.Sprintf("%s %d/%d (%.1f%%) sent=%d skipped=%d", p.label, processed, p.total, percent, sent, skipped)
	if done {
		fmt.Fprintln(os.Stderr, "\r"+line)
		return
	}
	fmt.Fprintf(os.Stderr, "\r%s", line)
}

func clampRange(start int, end int, total int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end <= 0 || end > total {
		end = total
	}
	if start > end {
		start = end
	}
	return start, end
}
