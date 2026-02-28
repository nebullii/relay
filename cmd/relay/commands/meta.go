package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("relay version %s\n", Version)
			return nil
		},
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize config and storage directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			if err := SaveConfig(cfg); err != nil {
				return err
			}
			if err := os.MkdirAll(cfg.BaseDir, 0755); err != nil {
				return err
			}
			fmt.Printf("  relay initialized\n")
			fmt.Printf("  storage: %s\n", cfg.BaseDir)
			return nil
		},
	}
}
