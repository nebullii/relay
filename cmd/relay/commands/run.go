package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/relaydev/relay/client"
)

func runCmd() *cobra.Command {
	var threadID string
	var model string
	var system string
	var user string
	var debug bool
	var dryRun bool
	var dumpRPB string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a bounded Relay call (uses client.Chat)",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadDotEnv()
			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}
			if model == "" {
				model = "gpt-5"
			}
			if user == "" {
				// Read stdin if provided
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					b, _ := io.ReadAll(os.Stdin)
					user = strings.TrimSpace(string(b))
				}
			}
			if user == "" {
				return fmt.Errorf("--user or stdin required")
			}

			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			c := &client.Client{
				Model:   model,
				System:  system,
				BaseDir: cfg.BaseDir,
			}

			ctx := context.Background()
			info, err := c.BuildRPB(ctx, threadID, user)
			if err != nil {
				return err
			}

			if debug {
				fmt.Fprintf(os.Stderr, "DEBUG rpb_bytes=%d state_header_bytes=%d preview_bytes=%d preview_count=%d\n",
					info.RPBBytes, info.HeaderBytes, info.PreviewBytes, info.PreviewCount)
			}
			if dumpRPB != "" {
				data, _ := json.Marshal(info.Bundle)
				if err := os.WriteFile(dumpRPB, data, 0644); err != nil {
					return fmt.Errorf("write rpb: %w", err)
				}
			}
			if dryRun {
				return nil
			}

			_, err = c.ChatWithRPB(ctx, info, user)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&threadID, "thread", "", "thread id")
	cmd.Flags().StringVar(&model, "model", "gpt-5", "model id")
	cmd.Flags().StringVar(&system, "system", "", "optional system prompt")
	cmd.Flags().StringVar(&user, "user", "", "user prompt (or stdin)")
	cmd.Flags().BoolVar(&debug, "debug", false, "log RPB sizes to stderr")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "build RPB and exit without calling OpenAI or mutating state")
	cmd.Flags().StringVar(&dumpRPB, "dump-rpb", "", "write RPB JSON to path")

	return cmd
}

// loadDotEnv loads key=value pairs from .env in the current directory if present.
// It does not override existing environment variables.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
}
