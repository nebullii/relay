package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func stateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Inspect and update thread state",
	}
	cmd.AddCommand(stateHeaderCmd(), statePatchCmd())
	return cmd
}

func stateHeaderCmd() *cobra.Command {
	var threadID string
	cmd := &cobra.Command{
		Use:   "header",
		Short: "Get the bounded state header for a thread",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}

			client := NewClient(cfg)
			var header map[string]any
			if err := client.Get("/threads/"+threadID+"/state/header", &header); err != nil {
				return err
			}

			data, err := json.MarshalIndent(header, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	return cmd
}

func statePatchCmd() *cobra.Command {
	var (
		threadID  string
		patchFile string
		patchJSON string
	)
	cmd := &cobra.Command{
		Use:   "patch",
		Short: "Apply a JSON Patch to thread state",
		Example: `  relay state patch --thread <id> --file patch.json
  relay state patch --thread <id> --json '[{"op":"add","path":"/facts/-","value":{"id":"f1","key":"status","value":"ready"}}]'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}

			var rawPatch []byte
			if patchFile != "" {
				rawPatch, err = os.ReadFile(patchFile)
				if err != nil {
					return fmt.Errorf("read patch file: %w", err)
				}
			} else if patchJSON != "" {
				rawPatch = []byte(patchJSON)
			} else {
				return fmt.Errorf("either --file or --json is required")
			}

			// Validate JSON
			var ops []map[string]any
			if err := json.Unmarshal(rawPatch, &ops); err != nil {
				return fmt.Errorf("invalid patch JSON: %w", err)
			}

			client := NewClient(cfg)
			var result map[string]any
			if err := client.Post("/threads/"+threadID+"/state/patch", ops, &result); err != nil {
				return fmt.Errorf("patch failed: %w", err)
			}

			fmt.Printf("  state updated\n")
			fmt.Printf("  version    %v\n", result["version"])
			fmt.Printf("  state_ref  %v\n", result["state_ref"])
			fmt.Printf("  updated_at %v\n", result["updated_at"])
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	cmd.Flags().StringVar(&patchFile, "file", "", "JSON patch file path")
	cmd.Flags().StringVar(&patchJSON, "json", "", "JSON patch inline")
	return cmd
}
