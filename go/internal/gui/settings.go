package gui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/config"
)

const settingsFileName = "gui-settings.json"

type Settings struct {
	ConfigPath        string   `json:"config_path"`
	ChatID            string   `json:"chat_id"`
	TopicID           *int     `json:"topic_id,omitempty"`
	WatchDir          string   `json:"watch_dir"`
	QueueFile         string   `json:"queue_file"`
	Recursive         bool     `json:"recursive"`
	WithImage         bool     `json:"with_image"`
	WithVideo         bool     `json:"with_video"`
	WithAudio         bool     `json:"with_audio"`
	WithAll           bool     `json:"with_all"`
	Include           []string `json:"include,omitempty"`
	Exclude           []string `json:"exclude,omitempty"`
	ZipPasswords      []string `json:"zip_passwords,omitempty"`
	ZipPassFile       string   `json:"zip_pass_file"`
	ScanIntervalSec   int      `json:"scan_interval_sec"`
	SendIntervalSec   int      `json:"send_interval_sec"`
	SettleSeconds     int      `json:"settle_seconds"`
	GroupSize         int      `json:"group_size"`
	BatchDelaySec     int      `json:"batch_delay_sec"`
	PauseEvery        int      `json:"pause_every"`
	PauseSecondsSec   int      `json:"pause_seconds_sec"`
	NotifyEnabled     bool     `json:"notify_enabled"`
	NotifyIntervalSec int      `json:"notify_interval_sec"`
	MaxDimension      int      `json:"max_dimension"`
	MaxBytes          int      `json:"max_bytes"`
	PNGStartLevel     int      `json:"png_start_level"`
}

type TelegramConfig struct {
	APIURLs []string `json:"api_urls"`
	Tokens  []string `json:"tokens"`
}

func DefaultSettings() Settings {
	return Settings{
		QueueFile:         "queue.jsonl",
		WithImage:         true,
		ScanIntervalSec:   30,
		SendIntervalSec:   30,
		SettleSeconds:     5,
		GroupSize:         4,
		BatchDelaySec:     3,
		PauseEvery:        0,
		PauseSecondsSec:   0,
		NotifyEnabled:     false,
		NotifyIntervalSec: 300,
		MaxDimension:      2000,
		MaxBytes:          5 * 1024 * 1024,
		PNGStartLevel:     8,
	}
}

func SettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("config dir not found")
	}
	return filepath.Join(dir, "telegram-upload-watcher", settingsFileName), nil
}

func LoadSettings(path string) (Settings, error) {
	if path == "" {
		defaultPath, err := SettingsPath()
		if err != nil {
			return Settings{}, err
		}
		path = defaultPath
	}
	settings := DefaultSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return Settings{}, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	settings.Include = append([]string{}, settings.Include...)
	settings.Exclude = append([]string{}, settings.Exclude...)
	settings.ZipPasswords = append([]string{}, settings.ZipPasswords...)
	return settings, nil
}

func SaveSettings(path string, settings Settings) error {
	if path == "" {
		defaultPath, err := SettingsPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func LoadTelegramConfig(path string) (TelegramConfig, error) {
	apiURLs, tokens, err := config.LoadConfig(path)
	if err != nil {
		return TelegramConfig{}, err
	}
	return TelegramConfig{APIURLs: apiURLs, Tokens: tokens}, nil
}

func SaveTelegramConfig(path string, cfg TelegramConfig) error {
	return config.SaveConfig(path, cfg.APIURLs, cfg.Tokens)
}
