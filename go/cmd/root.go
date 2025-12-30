package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var verbose bool

func newRootCmd(version string, buildTime string, gitCommit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telegram-send-go",
		Short: "telegram-send-go is a Telegram upload watcher CLI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	cmd.AddCommand(newSendMessageCmd())
	cmd.AddCommand(newSendImagesCmd())
	cmd.AddCommand(newSendFileCmd())
	cmd.AddCommand(newSendVideoCmd())
	cmd.AddCommand(newSendAudioCmd())
	cmd.AddCommand(newSendMixedCmd())
	cmd.AddCommand(newWatchCmd())
	cmd.AddCommand(newVersionCmd(version, buildTime, gitCommit))
	return cmd
}

func Execute(version string, buildTime string, gitCommit string) error {
	if err := newRootCmd(version, buildTime, gitCommit).Execute(); err != nil {
		return fmt.Errorf("error executing root command: %w", err)
	}
	return nil
}
