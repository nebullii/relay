package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/relaydev/relay/internal/state"
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

			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			header, err := eng.StateHeader(threadID)
			if err != nil {
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
			var ops []state.PatchOp
			if err := json.Unmarshal(rawPatch, &ops); err != nil {
				return fmt.Errorf("invalid patch JSON: %w", err)
			}

			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			next, err := eng.PatchState(threadID, ops)
			if err != nil {
				return fmt.Errorf("patch failed: %w", err)
			}

			fmt.Printf("  state updated\n")
			fmt.Printf("  version    %v\n", next.Version)
			fmt.Printf("  state_ref  v%v\n", next.Version)
			fmt.Printf("  updated_at %v\n", next.UpdatedAt)
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	cmd.Flags().StringVar(&patchFile, "file", "", "JSON patch file path")
	cmd.Flags().StringVar(&patchJSON, "json", "", "JSON patch inline")
	return cmd
}
