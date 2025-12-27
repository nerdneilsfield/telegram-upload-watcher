package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/config"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/notify"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/queue"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/sender"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/watcher"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/pkgs/constants"
)

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "send-message":
		runSendMessage(os.Args[2:])
	case "send-images":
		runSendImages(os.Args[2:])
	case "watch":
		runWatch(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  telegram-send-go send-message [options]")
	fmt.Println("  telegram-send-go send-images [options]")
	fmt.Println("  telegram-send-go watch [options]")
}

type commonFlags struct {
	configPath     string
	botToken       string
	apiURL         string
	chatID         string
	topicID        int
	validateTokens bool
	maxRetries     int
	retryDelay     time.Duration
	retryDelaySec  *int
}

func bindCommon(fs *flag.FlagSet) *commonFlags {
	cfg := &commonFlags{}
	fs.StringVar(&cfg.configPath, "config", "", "Path to INI config file")
	fs.StringVar(&cfg.botToken, "bot-token", "", "Telegram bot token(s), comma-separated")
	fs.StringVar(&cfg.apiURL, "api-url", "", "Telegram API URL(s), comma-separated")
	fs.StringVar(&cfg.chatID, "chat-id", "", "Target chat ID (channel/group/user)")
	fs.IntVar(&cfg.topicID, "topic-id", 0, "Topic/thread ID inside group/channel")
	fs.BoolVar(&cfg.validateTokens, "validate-tokens", false, "Validate tokens via getMe before sending")
	fs.IntVar(&cfg.maxRetries, "max-retries", 3, "Maximum retries for Telegram API calls")
	cfg.retryDelaySec = fs.Int("retry-delay", 3, "Delay between retries (seconds)")
	return cfg
}

func resolveConfig(cfg *commonFlags) ([]string, []string, error) {
	apiURLs := []string{}
	tokens := []string{}

	if cfg.configPath != "" {
		loadedURLs, loadedTokens, err := config.LoadConfig(cfg.configPath)
		if err != nil {
			return nil, nil, err
		}
		apiURLs = append(apiURLs, loadedURLs...)
		tokens = append(tokens, loadedTokens...)
	}

	if cfg.apiURL != "" {
		for _, entry := range strings.Split(cfg.apiURL, ",") {
			value := config.NormalizeAPIURL(entry)
			if value != "" {
				apiURLs = append(apiURLs, value)
			}
		}
	}

	if cfg.botToken != "" {
		for _, entry := range strings.Split(cfg.botToken, ",") {
			value := strings.TrimSpace(entry)
			if value != "" {
				tokens = append(tokens, value)
			}
		}
	}

	if len(apiURLs) == 0 {
		apiURLs = append(apiURLs, "https://api.telegram.org")
	}
	if len(tokens) == 0 {
		return nil, nil, fmt.Errorf("no bot token provided")
	}
	return apiURLs, tokens, nil
}

func buildClient(cfg *commonFlags, apiURLs []string, tokens []string) (*telegram.Client, *telegram.URLPool, *telegram.TokenPool, error) {
	urlPool := telegram.NewURLPool(apiURLs)
	tokenPool := telegram.NewTokenPool(tokens)
	client := telegram.NewClient(urlPool, tokenPool)

	if cfg.validateTokens {
		valid := []string{}
		for _, token := range tokens {
			apiURL := urlPool.Get()
			if apiURL == "" {
				continue
			}
			ok := client.TestToken(apiURL, token)
			urlPool.Increment(apiURL)
			if ok {
				valid = append(valid, token)
			}
		}
		if len(valid) == 0 {
			return nil, nil, nil, fmt.Errorf("no valid tokens after validation")
		}
		tokenPool = telegram.NewTokenPool(valid)
		client = telegram.NewClient(urlPool, tokenPool)
	}

	return client, urlPool, tokenPool, nil
}

func topicPtr(cfg *commonFlags) *int {
	if cfg.topicID == 0 {
		return nil
	}
	return &cfg.topicID
}

func runSendMessage(args []string) {
	fs := flag.NewFlagSet("send-message", flag.ExitOnError)
	cfg := bindCommon(fs)
	message := fs.String("message", "", "Message text to send")
	fs.Parse(args)
	cfg.retryDelay = time.Duration(*cfg.retryDelaySec) * time.Second

	if cfg.chatID == "" || *message == "" {
		log.Fatal("chat-id and message are required")
	}

	apiURLs, tokens, err := resolveConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	client, _, _, err := buildClient(cfg, apiURLs, tokens)
	if err != nil {
		log.Fatal(err)
	}

	retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
	if err := client.SendMessage(cfg.chatID, *message, topicPtr(cfg), retry); err != nil {
		log.Fatal(err)
	}
}

func runSendImages(args []string) {
	fs := flag.NewFlagSet("send-images", flag.ExitOnError)
	cfg := bindCommon(fs)
	imageDir := fs.String("image-dir", "", "Image directory")
	zipFile := fs.String("zip-file", "", "Zip file path")
	groupSize := fs.Int("group-size", 4, "Images per media group")
	startIndex := fs.Int("start-index", 0, "Start group index (0-based)")
	endIndex := fs.Int("end-index", 0, "End group index (0 for no limit)")
	batchDelay := fs.Int("batch-delay", 3, "Delay between media groups (seconds)")
	fs.Parse(args)
	cfg.retryDelay = time.Duration(*cfg.retryDelaySec) * time.Second

	if cfg.chatID == "" {
		log.Fatal("chat-id is required")
	}
	if *imageDir == "" && *zipFile == "" {
		log.Fatal("image-dir or zip-file is required")
	}

	apiURLs, tokens, err := resolveConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	client, _, _, err := buildClient(cfg, apiURLs, tokens)
	if err != nil {
		log.Fatal(err)
	}

	retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
	if *imageDir != "" {
		sendImagesFromDir(client, cfg.chatID, topicPtr(cfg), *imageDir, *groupSize, *startIndex, *endIndex, time.Duration(*batchDelay)*time.Second, retry)
	}
	if *zipFile != "" {
		sendImagesFromZip(client, cfg.chatID, topicPtr(cfg), *zipFile, *groupSize, *startIndex, *endIndex, time.Duration(*batchDelay)*time.Second, retry)
	}
}

func runWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	cfg := bindCommon(fs)
	watchDir := fs.String("watch-dir", "", "Folder to watch")
	queueFile := fs.String("queue-file", "queue.jsonl", "Path to JSONL queue file")
	recursive := fs.Bool("recursive", false, "Enable recursive scan")
	excludes := fs.String("exclude", "", "Glob patterns to exclude (comma-separated)")
	scanInterval := fs.Int("scan-interval", 30, "Folder scan interval (seconds)")
	sendInterval := fs.Int("send-interval", 30, "Queue send interval (seconds)")
	settleSeconds := fs.Int("settle-seconds", 5, "Seconds to wait for file stability")
	groupSize := fs.Int("group-size", 4, "Images per media group")
	batchDelay := fs.Int("batch-delay", 3, "Delay between media groups (seconds)")
	pauseEvery := fs.Int("pause-every", 0, "Pause after sending this many images (0 disables)")
	pauseSeconds := fs.Int("pause-seconds", 0, "Pause duration in seconds")
	maxDimension := fs.Int("max-dimension", 2000, "Maximum image dimension before scaling")
	maxBytes := fs.Int("max-bytes", 5*1024*1024, "Maximum image size in bytes before PNG compression")
	pngStart := fs.Int("png-start-level", 8, "Initial PNG compression level (0-9)")
	notifyEnabled := fs.Bool("notify", false, "Send watch notifications")
	notifyInterval := fs.Int("notify-interval", 300, "Seconds between status notifications")
	fs.Parse(args)
	cfg.retryDelay = time.Duration(*cfg.retryDelaySec) * time.Second

	if cfg.chatID == "" {
		log.Fatal("chat-id is required")
	}
	if *watchDir == "" {
		log.Fatal("watch-dir is required")
	}

	apiURLs, tokens, err := resolveConfig(cfg)
	if err != nil {
		log.Fatal(err)
	}
	client, _, _, err := buildClient(cfg, apiURLs, tokens)
	if err != nil {
		log.Fatal(err)
	}

	q, err := queue.New(*queueFile)
	if err != nil {
		log.Fatal(err)
	}

	watchCfg := watcher.Config{
		Root:          *watchDir,
		Recursive:     *recursive,
		ExcludeGlobs:  splitPatterns(*excludes),
		ScanInterval:  time.Duration(*scanInterval) * time.Second,
		SettleSeconds: *settleSeconds,
	}

	retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
	sendCfg := sender.Config{
		ChatID:        cfg.chatID,
		TopicID:       topicPtr(cfg),
		GroupSize:     *groupSize,
		SendInterval:  time.Duration(*sendInterval) * time.Second,
		BatchDelay:    time.Duration(*batchDelay) * time.Second,
		PauseEvery:    *pauseEvery,
		PauseSeconds:  time.Duration(*pauseSeconds) * time.Second,
		MaxDimension:  *maxDimension,
		MaxBytes:      *maxBytes,
		PNGStartLevel: *pngStart,
		Retry:         retry,
	}

	notifyCfg := notify.Config{
		Enabled:      *notifyEnabled,
		Interval:     time.Duration(*notifyInterval) * time.Second,
		NotifyOnIdle: true,
	}

	go watcher.WatchLoop(watchCfg, q)
	go sender.Loop(sendCfg, q, client)
	if notifyCfg.Enabled {
		go notify.Loop(notifyCfg, q, client, cfg.chatID, topicPtr(cfg))
	}

	select {}
}

func sendImagesFromDir(client *telegram.Client, chatID string, topicID *int, dir string, groupSize int, startIndex int, endIndex int, delay time.Duration, retry telegram.RetryConfig) {
	files := []string{}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if isImage(path) {
			files = append(files, path)
		}
		return nil
	})

	if len(files) == 0 {
		log.Printf("no images found in %s", dir)
		return
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Starting image upload: %d file(s)", len(files)), topicID, retry)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize

	media := []telegram.MediaFile{}
	for idx, path := range files {
		if idx < minIndex {
			continue
		}
		if endIndex > 0 && idx >= maxIndex {
			break
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		media = append(media, telegram.MediaFile{Filename: filepath.Base(path), Data: data})
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
			}
			media = media[:0]
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
		}
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed image upload from %s", dir), topicID, retry)
}

func sendImagesFromZip(client *telegram.Client, chatID string, topicID *int, zipPath string, groupSize int, startIndex int, endIndex int, delay time.Duration, retry telegram.RetryConfig) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		log.Printf("invalid zip: %s", zipPath)
		return
	}
	defer archive.Close()

	names := []string{}
	filesByName := map[string]*zip.File{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if isImage(file.Name) {
			names = append(names, file.Name)
			filesByName[file.Name] = file
		}
	}
	if len(names) == 0 {
		log.Printf("no images found in %s", zipPath)
		return
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Starting image upload: %d file(s)", len(names)), topicID, retry)

	minIndex := startIndex * groupSize
	maxIndex := endIndex * groupSize
	media := []telegram.MediaFile{}

	for idx, name := range names {
		if idx < minIndex {
			continue
		}
		if endIndex > 0 && idx >= maxIndex {
			break
		}
		file := filesByName[name]
		if file == nil {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(handle)
		handle.Close()
		if err != nil {
			continue
		}
		media = append(media, telegram.MediaFile{Filename: filepath.Base(name), Data: data})
		if len(media) >= groupSize {
			if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
				log.Printf("send media group failed: %v", err)
			}
			media = media[:0]
			time.Sleep(delay)
		}
	}

	if len(media) > 0 {
		if err := client.SendMediaGroup(chatID, media, topicID, retry); err != nil {
			log.Printf("send media group failed: %v", err)
		}
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("Completed image upload from %s", filepath.Base(zipPath)), topicID, retry)
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

func splitPatterns(input string) []string {
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	patterns := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			patterns = append(patterns, part)
		}
	}
	return patterns
}
