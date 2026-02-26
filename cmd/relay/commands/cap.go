package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func capCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cap",
		Short: "Manage and invoke capabilities",
	}
	cmd.AddCommand(capInvokeCmd(), capListCmd())
	return cmd
}

func capInvokeCmd() *cobra.Command {
	var (
		threadID string
		argsFile string
		argsJSON string
	)
	cmd := &cobra.Command{
		Use:   "invoke <capability>",
		Short: "Invoke a capability",
		Args:  cobra.ExactArgs(1),
		Example: `  relay cap invoke retrieval.search --thread <id> --json '{"query":"hello"}'
  relay cap invoke http.fetch --thread <id> --json '{"url":"https://example.com"}'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}

			var rawArgs []byte
			if argsFile != "" {
				rawArgs, err = os.ReadFile(argsFile)
				if err != nil {
					return fmt.Errorf("read args file: %w", err)
				}
			} else if argsJSON != "" {
				rawArgs = []byte(argsJSON)
			} else {
				rawArgs = []byte("{}")
			}

			// Validate JSON
			var argsVal json.RawMessage = rawArgs
			if !json.Valid(rawArgs) {
				return fmt.Errorf("invalid JSON args")
			}

			req := map[string]any{
				"capability": args[0],
				"thread_id":  threadID,
				"args":       argsVal,
			}

			client := NewClient(cfg)
			var result map[string]any
			if err := client.Post("/cap/invoke", req, &result); err != nil {
				return fmt.Errorf("invoke failed: %w", err)
			}

			if hit, ok := result["cache_hit"].(bool); ok && hit {
				fmt.Printf("  cache_hit     true\n")
			}
			fmt.Printf("  capability    %v\n", result["capability"])
			if ref, ok := result["artifact_ref"].(string); ok && ref != "" {
				fmt.Printf("  artifact_ref  %s\n", ref)
			}
			if ms, ok := result["duration_ms"]; ok {
				fmt.Printf("  duration_ms   %v\n", ms)
			}
			fmt.Printf("\n  preview:\n")
			if preview, ok := result["preview"]; ok {
				data, _ := json.MarshalIndent(preview, "    ", "  ")
				fmt.Println("    " + string(data))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	cmd.Flags().StringVar(&argsFile, "json-file", "", "JSON args file path")
	cmd.Flags().StringVar(&argsJSON, "json", "", "JSON args inline")
	return cmd
}

func capListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			var result struct {
				Capabilities []map[string]any `json:"capabilities"`
			}
			if err := client.Get("/cap/list", &result); err != nil {
				return err
			}

			fmt.Printf("  %-25s  %-8s  %s\n", "NAME", "CACHE", "DESCRIPTION")
			fmt.Printf("  %s  %s  %s\n", pad("-", 25), pad("-", 8), pad("-", 40))
			for _, c := range result.Capabilities {
				name := fmt.Sprintf("%v", c["name"])
				cacheable := "no"
				if c["cacheable"] == true {
					cacheable = "yes"
				}
				desc := fmt.Sprintf("%v", c["description"])
				fmt.Printf("  %-25s  %-8s  %s\n", name, cacheable, desc)
			}
			return nil
		},
	}
}

func pad(s string, n int) string {
	result := ""
	for len(result) < n {
		result += s
	}
	return result[:n]
}
