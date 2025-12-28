package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/notify"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/sender"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/watcher"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	cfg := &commonFlags{}
	var watchDir string
	var queueFile string
	var recursive bool
	var withImage bool
	var withVideo bool
	var withAudio bool
	var withAll bool
	includes := &stringSlice{}
	excludes := &stringSlice{}
	var scanInterval int
	var sendInterval int
	var settleSeconds int
	var groupSize int
	var batchDelay int
	var pauseEvery int
	var pauseSeconds int
	var maxDimension int
	var maxBytes int
	var pngStart int
	var notifyEnabled bool
	var notifyInterval int
	zipPasses := &stringSlice{}
	var zipPassFile string

	cmd := &cobra.Command{
		Use:          "watch",
		Short:        "Watch folder and send queued images",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryDelay = time.Duration(cfg.retryDelaySec) * time.Second
			if cfg.chatID == "" {
				return fmt.Errorf("chat-id is required")
			}
			if watchDir == "" {
				return fmt.Errorf("watch-dir is required")
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

			if withAll {
				withImage = true
				withVideo = true
				withAudio = true
			}
			if !withImage && !withVideo && !withAudio && !withAll {
				withImage = true
			}

			absWatchDir, err := filepath.Abs(watchDir)
			if err != nil {
				return err
			}
			q, err := queue.New(queueFile, &queue.Meta{
				Params: queue.MetaParams{
					Command:   "watch",
					WatchDir:  absWatchDir,
					Recursive: recursive,
					ChatID:    cfg.chatID,
					TopicID:   topicPtr(cfg),
					WithImage: withImage,
					WithVideo: withVideo,
					WithAudio: withAudio,
					WithAll:   withAll,
					Include:   includes.Values(),
					Exclude:   excludes.Values(),
				},
			})
			if err != nil {
				return err
			}

			watchCfg := watcher.Config{
				Root:          watchDir,
				Recursive:     recursive,
				IncludeGlobs:  includes.Values(),
				ExcludeGlobs:  excludes.Values(),
				WithImage:     withImage,
				WithVideo:     withVideo,
				WithAudio:     withAudio,
				WithAll:       withAll,
				ScanInterval:  time.Duration(scanInterval) * time.Second,
				SettleSeconds: settleSeconds,
			}

			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
			sendCfg := sender.Config{
				ChatID:        cfg.chatID,
				TopicID:       topicPtr(cfg),
				GroupSize:     groupSize,
				SendInterval:  time.Duration(sendInterval) * time.Second,
				BatchDelay:    time.Duration(batchDelay) * time.Second,
				PauseEvery:    pauseEvery,
				PauseSeconds:  time.Duration(pauseSeconds) * time.Second,
				MaxDimension:  maxDimension,
				MaxBytes:      maxBytes,
				PNGStartLevel: pngStart,
				Retry:         retry,
				ZipPasswords:  zipPasswords,
			}

			notifyCfg := notify.Config{
				Enabled:      notifyEnabled,
				Interval:     time.Duration(notifyInterval) * time.Second,
				NotifyOnIdle: true,
			}

			go watcher.WatchLoop(watchCfg, q)
			go sender.Loop(sendCfg, q, client)
			if notifyCfg.Enabled {
				go notify.Loop(notifyCfg, q, client, cfg.chatID, topicPtr(cfg))
			}

			select {}
		},
	}

	bindCommonFlags(cmd, cfg)
	flags := cmd.Flags()
	flags.StringVar(&watchDir, "watch-dir", "", "Folder to watch")
	flags.StringVar(&queueFile, "queue-file", "queue.jsonl", "Path to JSONL queue file")
	flags.BoolVar(&recursive, "recursive", false, "Enable recursive scan")
	flags.BoolVar(&withImage, "with-image", false, "Send matching images (media groups)")
	flags.BoolVar(&withVideo, "with-video", false, "Send matching videos")
	flags.BoolVar(&withAudio, "with-audio", false, "Send matching audio files")
	flags.BoolVar(&withAll, "all", false, "Send all matching files (images use media groups)")
	flags.Var(includes, "include", "Glob patterns to include (repeatable or comma-separated)")
	flags.Var(excludes, "exclude", "Glob patterns to exclude (repeatable or comma-separated)")
	flags.IntVar(&scanInterval, "scan-interval", 30, "Folder scan interval (seconds)")
	flags.IntVar(&sendInterval, "send-interval", 30, "Queue send interval (seconds)")
	flags.IntVar(&settleSeconds, "settle-seconds", 5, "Seconds to wait for file stability")
	flags.IntVar(&groupSize, "group-size", 4, "Images per media group")
	flags.IntVar(&batchDelay, "batch-delay", 3, "Delay between media groups (seconds)")
	flags.IntVar(&pauseEvery, "pause-every", 0, "Pause after sending this many images (0 disables)")
	flags.IntVar(&pauseSeconds, "pause-seconds", 0, "Pause duration in seconds")
	flags.IntVar(&maxDimension, "max-dimension", 2000, "Maximum image dimension before scaling")
	flags.IntVar(&maxBytes, "max-bytes", 5*1024*1024, "Maximum image size in bytes before PNG compression")
	flags.IntVar(&pngStart, "png-start-level", 8, "Initial PNG compression level (0-9)")
	flags.BoolVar(&notifyEnabled, "notify", false, "Send watch notifications")
	flags.IntVar(&notifyInterval, "notify-interval", 300, "Seconds between status notifications")
	flags.Var(zipPasses, "zip-pass", "Zip password (repeatable or comma-separated)")
	flags.StringVar(&zipPassFile, "zip-pass-file", "", "Path to file with zip passwords (one per line)")
	return cmd
}
