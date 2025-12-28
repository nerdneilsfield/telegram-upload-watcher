package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/config"
	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/spf13/cobra"
)

type commonFlags struct {
	configPath     string
	botToken       string
	apiURL         string
	chatID         string
	topicID        int
	validateTokens bool
	maxRetries     int
	retryDelaySec  int
	retryDelay     time.Duration
}

func bindCommonFlags(cmd *cobra.Command, cfg *commonFlags) {
	flags := cmd.Flags()
	flags.StringVar(&cfg.configPath, "config", "", "Path to INI config file")
	flags.StringVar(&cfg.botToken, "bot-token", "", "Telegram bot token(s), comma-separated")
	flags.StringVar(&cfg.apiURL, "api-url", "", "Telegram API URL(s), comma-separated")
	flags.StringVar(&cfg.chatID, "chat-id", "", "Target chat ID (channel/group/user)")
	flags.IntVar(&cfg.topicID, "topic-id", 0, "Topic/thread ID inside group/channel")
	flags.BoolVar(&cfg.validateTokens, "validate-tokens", false, "Validate tokens via getMe before sending")
	flags.IntVar(&cfg.maxRetries, "max-retries", 3, "Maximum retries for Telegram API calls")
	flags.IntVar(&cfg.retryDelaySec, "retry-delay", 3, "Delay between retries (seconds)")
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

func loadZipPasswords(values []string, filePath string) ([]string, error) {
	passwords := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			passwords = append(passwords, value)
		}
	}
	if filePath == "" {
		return passwords, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			passwords = append(passwords, line)
		}
	}
	return passwords, nil
}

type stringSlice struct {
	values []string
}

func (s *stringSlice) String() string {
	return strings.Join(s.values, ",")
}

func (s *stringSlice) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			s.values = append(s.values, part)
		}
	}
	return nil
}

func (s *stringSlice) Type() string {
	return "stringSlice"
}

func (s *stringSlice) Values() []string {
	return s.values
}
