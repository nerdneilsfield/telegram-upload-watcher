package cmd

import (
	"fmt"
	"time"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/internal/telegram"
	"github.com/spf13/cobra"
)

func newSendMessageCmd() *cobra.Command {
	cfg := &commonFlags{}
	var message string

	cmd := &cobra.Command{
		Use:          "send-message",
		Short:        "Send a text message",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryDelay = time.Duration(cfg.retryDelaySec) * time.Second
			if cfg.chatID == "" || message == "" {
				return fmt.Errorf("chat-id and message are required")
			}

			apiURLs, tokens, err := resolveConfig(cfg)
			if err != nil {
				return err
			}
			client, _, _, err := buildClient(cfg, apiURLs, tokens)
			if err != nil {
				return err
			}

			retry := telegram.RetryConfig{MaxRetries: cfg.maxRetries, Delay: cfg.retryDelay}
			return client.SendMessage(cfg.chatID, message, topicPtr(cfg), retry)
		},
	}

	bindCommonFlags(cmd, cfg)
	cmd.Flags().StringVar(&message, "message", "", "Message text to send")
	return cmd
}
