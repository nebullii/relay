package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/relaydev/relay/client"
)

func promptCmd() *cobra.Command {
	var model string
	var system string
	var logJSON bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "prompt <text>",
		Short: "Run a prompt with zero ceremony (auto thread)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrompt(args, model, system, logJSON, verbose)
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "model id (defaults to RELAY_MODEL or gpt-4o)")
	cmd.Flags().StringVar(&system, "system", "", "optional system prompt")
	cmd.Flags().BoolVar(&logJSON, "log-json", true, "append a JSONL telemetry line to ~/.relay/log.jsonl")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "show full telemetry block")

	return cmd
}

func runPrompt(args []string, model, system string, logJSON bool, verbose bool) error {
	loadDotEnv()

	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		// Read stdin if provided
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			b, _ := io.ReadAll(os.Stdin)
			prompt = strings.TrimSpace(string(b))
		}
	}
	if prompt == "" {
		return fmt.Errorf("prompt text required")
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
	res, err := c.Chat(ctx, cfg.DefaultThreadID, prompt)
	if err != nil {
		return err
	}
	// Persist default thread if new
	if cfg.DefaultThreadID == "" && res.ThreadID != "" {
		cfg.DefaultThreadID = res.ThreadID
		_ = SaveConfig(cfg)
	}

	printSavingsSummary(res.ThreadID, res, res.NaiveTokens, res.ActualTokens, res.AvoidedTokens, verbose)

	if logJSON {
		_ = appendTelemetry(cfg, res.ThreadID, res, res.NaiveTokens, res.ActualTokens, res.AvoidedTokens)
	}

	return nil
}

func printSavingsSummary(threadID string, res client.Response, naive, actual, avoided int, verbose bool) {
	if !verbose {
		fmt.Printf("Relay: estimated saved ~%s tokens (vs full context) | prompt %s\n", humanTokens(avoided), humanBytes(res.PromptBytes))
		return
	}
	fmt.Println("────────────────────────")
	fmt.Println("Relay")
	fmt.Printf("Prompt size: %s\n", humanBytes(res.PromptBytes))
	fmt.Printf("Header: %s\n", humanBytes(res.HeaderBytes))
	fmt.Printf("Previews: %d\n", res.PreviewCount)
	fmt.Printf("Estimated saved: %s tokens (vs full context)\n", humanTokens(avoided))
	fmt.Printf("Thread: %s\n", threadID)
	fmt.Println("────────────────────────")
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	kb := float64(n) / 1024.0
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024.0
	return fmt.Sprintf("%.1f MB", mb)
}

func humanTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000.0)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000.0)
}

func appendTelemetry(cfg *Config, threadID string, res client.Response, naive, actual, avoided int) error {
	path := filepath.Join(cfg.BaseDir, "log.jsonl")
	if err := os.MkdirAll(cfg.BaseDir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := map[string]any{
		"ts":             time.Now().UTC().Format(time.RFC3339),
		"thread_id":      threadID,
		"prompt_bytes":   res.PromptBytes,
		"header_bytes":   res.HeaderBytes,
		"preview_bytes":  res.PreviewBytes,
		"preview_count":  res.PreviewCount,
		"naive_tokens":   naive,
		"actual_tokens":  actual,
		"avoided_tokens": avoided,
		"artifact_ref":   res.ArtifactRef,
	}
	data, _ := json.Marshal(entry)
	_, err = f.Write(append(data, '\n'))
	return err
}
