package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/gui"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/notify"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/runcontrol"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/sender"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/watcher"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type runState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	pauseGate *runcontrol.PauseGate
	queue     *queue.Queue
	paused    bool
}

type RunStatus struct {
	Running bool   `json:"running"`
	Paused  bool   `json:"paused"`
	Error   string `json:"error,omitempty"`
}

func (a *App) StartRun(bundle SettingsBundle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run != nil {
		return errors.New("run already active")
	}
	settings := bundle.Settings
	if settings.ChatID == "" {
		return errors.New("chat_id is required")
	}
	if settings.WatchDir == "" {
		return errors.New("watch_dir is required")
	}
	if settings.QueueFile == "" {
		settings.QueueFile = "queue.jsonl"
	}
	if settings.WithAll {
		settings.WithImage = true
		settings.WithVideo = true
		settings.WithAudio = true
	}
	if !settings.WithImage && !settings.WithVideo && !settings.WithAudio && !settings.WithAll {
		settings.WithImage = true
	}

	absWatchDir, err := filepath.Abs(settings.WatchDir)
	if err != nil {
		return err
	}
	meta := &queue.Meta{
		Params: queue.MetaParams{
			Command:   "watch",
			WatchDir:  queue.WatchDirs{absWatchDir},
			Recursive: settings.Recursive,
			ChatID:    settings.ChatID,
			TopicID:   settings.TopicID,
			WithImage: settings.WithImage,
			WithVideo: settings.WithVideo,
			WithAudio: settings.WithAudio,
			WithAll:   settings.WithAll,
			Include:   settings.Include,
			Exclude:   settings.Exclude,
		},
	}

	q, err := queue.New(settings.QueueFile, meta)
	if err != nil {
		return err
	}

	zipPasswords, err := gui.LoadZipPasswords(settings.ZipPasswords, settings.ZipPassFile)
	if err != nil {
		return err
	}

	client, err := buildClient(bundle.Telegram)
	if err != nil {
		return err
	}

	watchCfg := watcher.Config{
		Root:          absWatchDir,
		Recursive:     settings.Recursive,
		IncludeGlobs:  settings.Include,
		ExcludeGlobs:  settings.Exclude,
		WithImage:     settings.WithImage,
		WithVideo:     settings.WithVideo,
		WithAudio:     settings.WithAudio,
		WithAll:       settings.WithAll,
		ScanInterval:  time.Duration(settings.ScanIntervalSec) * time.Second,
		SettleSeconds: settings.SettleSeconds,
	}

	sendCfg := sender.Config{
		ChatID:        settings.ChatID,
		TopicID:       settings.TopicID,
		GroupSize:     settings.GroupSize,
		SendInterval:  time.Duration(settings.SendIntervalSec) * time.Second,
		BatchDelay:    time.Duration(settings.BatchDelaySec) * time.Second,
		PauseEvery:    settings.PauseEvery,
		PauseSeconds:  time.Duration(settings.PauseSecondsSec) * time.Second,
		MaxDimension:  settings.MaxDimension,
		MaxBytes:      settings.MaxBytes,
		PNGStartLevel: settings.PNGStartLevel,
		Retry: telegram.RetryConfig{
			MaxRetries: 3,
			Delay:      3 * time.Second,
		},
		ZipPasswords: zipPasswords,
	}

	notifyCfg := notify.Config{
		Enabled:      settings.NotifyEnabled,
		Interval:     time.Duration(settings.NotifyIntervalSec) * time.Second,
		NotifyOnIdle: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	pauseGate := runcontrol.NewPauseGate()
	a.run = &runState{
		ctx:       ctx,
		cancel:    cancel,
		pauseGate: pauseGate,
		queue:     q,
		paused:    false,
	}

	runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: true})

	go watcher.WatchLoopWithContext(ctx, watchCfg, q, pauseGate)
	go sender.LoopWithContext(ctx, sendCfg, q, client, pauseGate, a.emitProgress)
	if notifyCfg.Enabled {
		go notify.LoopWithContext(ctx, notifyCfg, q, client, settings.ChatID, settings.TopicID)
	}
	return nil
}

func (a *App) StartSendImages(bundle SettingsBundle, req SendImagesRequest) error {
	return a.startOneOff(bundle, func(ctx context.Context, pause *runcontrol.PauseGate, client *telegram.Client) error {
		return sendImages(ctx, client, bundle, req, pause, a.emitProgress)
	})
}

func (a *App) StartSendFiles(bundle SettingsBundle, req SendFilesRequest) error {
	return a.startOneOff(bundle, func(ctx context.Context, pause *runcontrol.PauseGate, client *telegram.Client) error {
		return sendFiles(ctx, client, bundle, req, pause, a.emitProgress)
	})
}

func (a *App) PauseRun() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run == nil {
		return errors.New("no active run")
	}
	if a.run.paused {
		return nil
	}
	a.run.paused = true
	a.run.pauseGate.Pause()
	runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: true, Paused: true})
	return nil
}

func (a *App) ResumeRun() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run == nil {
		return errors.New("no active run")
	}
	if !a.run.paused {
		return nil
	}
	a.run.paused = false
	a.run.pauseGate.Resume()
	runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: true, Paused: false})
	return nil
}

func (a *App) StopRun() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run == nil {
		return nil
	}
	a.run.pauseGate.Resume()
	a.run.cancel()
	a.run.queue.Close()
	a.run = nil
	runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: false})
	return nil
}

func (a *App) RunStatus() RunStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run == nil {
		return RunStatus{Running: false}
	}
	return RunStatus{Running: true, Paused: a.run.paused}
}

func (a *App) QueueStats() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.run == nil || a.run.queue == nil {
		return map[string]int{}
	}
	return a.run.queue.Stats()
}

func (a *App) emitProgress(update sender.ProgressUpdate) {
	runtime.EventsEmit(a.ctx, "progress", update)
}

func buildClient(cfg gui.TelegramConfig) (*telegram.Client, error) {
	if len(cfg.APIURLs) == 0 || len(cfg.Tokens) == 0 {
		return nil, errors.New("api_urls and tokens are required")
	}
	urlPool := telegram.NewURLPool(cfg.APIURLs)
	tokenPool := telegram.NewTokenPool(cfg.Tokens)
	return telegram.NewClient(urlPool, tokenPool), nil
}

type oneOffJob func(ctx context.Context, pause *runcontrol.PauseGate, client *telegram.Client) error

func (a *App) startOneOff(bundle SettingsBundle, job oneOffJob) error {
	a.mu.Lock()
	if a.run != nil {
		a.mu.Unlock()
		return errors.New("run already active")
	}
	client, err := buildClient(bundle.Telegram)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	pauseGate := runcontrol.NewPauseGate()
	a.run = &runState{
		ctx:       ctx,
		cancel:    cancel,
		pauseGate: pauseGate,
		queue:     nil,
		paused:    false,
	}
	a.mu.Unlock()

	runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: true})

	go func() {
		err := job(ctx, pauseGate, client)
		if err != nil && !errors.Is(err, context.Canceled) {
			runtime.EventsEmit(a.ctx, "run-error", err.Error())
		}
		a.mu.Lock()
		if a.run != nil && a.run.ctx == ctx {
			a.run = nil
		}
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "run-status", RunStatus{Running: false})
	}()
	return nil
}
