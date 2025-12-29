package cmd

import (
	"fmt"
	"os"

	ansi "github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

type progressTracker struct {
	bar     *progressbar.ProgressBar
	enabled bool
	total   int
	label   string
}

func newProgressTracker(total int, label string) progressTracker {
	if total <= 0 {
		return progressTracker{enabled: false}
	}
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionSetWidth(18),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetDescription(label),
	)
	return progressTracker{bar: bar, enabled: true, total: total, label: label}
}

func (p progressTracker) Print(processed int, sent int, skipped int, done bool) {
	if !p.enabled || p.total <= 0 || p.bar == nil {
		return
	}
	if processed < 0 {
		processed = 0
	}
	if processed > p.total {
		processed = p.total
	}
	desc := fmt.Sprintf("%s sent=%d skipped=%d", p.label, sent, skipped)
	p.bar.Describe(desc)
	_ = p.bar.Set(processed)
	if done {
		_ = p.bar.Finish()
		fmt.Fprintln(os.Stdout)
	}
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
