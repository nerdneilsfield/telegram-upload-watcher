package main

import (
	"context"
	"errors"
	"sync"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/gui"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context
	mu  sync.Mutex
	run *runState
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

type SettingsBundle struct {
	Settings     gui.Settings       `json:"settings"`
	Telegram     gui.TelegramConfig `json:"telegram"`
	SettingsPath string             `json:"settings_path"`
}

func (a *App) LoadSettings() (SettingsBundle, error) {
	settings, err := gui.LoadSettings("")
	if err != nil {
		return SettingsBundle{}, err
	}
	bundle := SettingsBundle{Settings: settings}
	if settings.ConfigPath != "" {
		telegram, err := gui.LoadTelegramConfig(settings.ConfigPath)
		if err != nil {
			return SettingsBundle{}, err
		}
		bundle.Telegram = telegram
	}
	settingsPath, err := gui.SettingsPath()
	if err == nil {
		bundle.SettingsPath = settingsPath
	}
	return bundle, nil
}

func (a *App) SaveSettings(bundle SettingsBundle) error {
	if err := gui.SaveSettings(bundle.SettingsPath, bundle.Settings); err != nil {
		return err
	}
	if bundle.Settings.ConfigPath == "" {
		return nil
	}
	return gui.SaveTelegramConfig(bundle.Settings.ConfigPath, bundle.Telegram)
}

func (a *App) PickFile(title string, defaultDir string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("app not ready")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
	})
}

func (a *App) PickDirectory(title string, defaultDir string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("app not ready")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
	})
}
