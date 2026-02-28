package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/relaydev/relay/client"
	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/rpb"
)

func wrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wrap",
		Short: "Wrap an external CLI with Relay prompt bundling",
	}
	cmd.AddCommand(wrapCodexCmd())
	return cmd
}

func wrapCodexCmd() *cobra.Command {
	var threadID string
	var user string
	var system string
	var debug bool

	cmd := &cobra.Command{
		Use:   "codex [-- <args...>]",
		Short: "Run codex with Relay-bounded prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadDotEnv()
			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}
			if user == "" {
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
				System:  system,
				BaseDir: cfg.BaseDir,
			}
			info, err := c.BuildRPB(context.Background(), threadID, user)
			if err != nil {
				return err
			}
			if debug {
				fmt.Fprintf(os.Stderr, "DEBUG rpb_bytes=%d state_header_bytes=%d preview_bytes=%d preview_count=%d\n",
					info.RPBBytes, info.HeaderBytes, info.PreviewBytes, info.PreviewCount)
			}

			prompt := renderPlainPrompt(&info.Bundle)

			cmdArgs := []string{}
			cmdArgs = append(cmdArgs, args...)
			proc := exec.Command("codex", cmdArgs...)
			proc.Stdin = strings.NewReader(prompt)
			stdout, err := proc.StdoutPipe()
			if err != nil {
				return err
			}
			stderr, err := proc.StderrPipe()
			if err != nil {
				return err
			}

			if err := proc.Start(); err != nil {
				return err
			}

			var outBuf bytes.Buffer
			done := make(chan error, 2)

			go func() {
				_, _ = io.Copy(io.MultiWriter(os.Stdout, &outBuf), stdout)
				done <- nil
			}()
			go func() {
				_, _ = io.Copy(os.Stderr, stderr)
				done <- nil
			}()

			<-done
			<-done

			if err := proc.Wait(); err != nil {
				return fmt.Errorf("codex failed: %w", err)
			}

			full := outBuf.String()
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			prov := artifacts.Provenance{CreatedBy: "codex", CreatedAt: time.Now().UTC()}
			art, err := eng.ArtifactPut(info.ThreadID, fmt.Sprintf("codex-%d", time.Now().Unix()), artifacts.TypeText, "text/plain", strings.NewReader(full), prov)
			if err != nil {
				return err
			}
			if art.Ref != "" {
				ops := []map[string]any{
					{"op": "add", "path": "/artifacts/-", "value": map[string]any{"ref": art.Ref, "type": "text"}},
					{"op": "add", "path": "/last_actions/-", "value": map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "description": "codex response", "result_ref": art.Ref}},
				}
				_, _ = eng.PatchState(info.ThreadID, toPatchOps(ops))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&threadID, "thread", "", "thread id")
	cmd.Flags().StringVar(&user, "user", "", "user prompt (or stdin)")
	cmd.Flags().StringVar(&system, "system", "", "optional system prompt")
	cmd.Flags().BoolVar(&debug, "debug", false, "log RPB sizes to stderr")

	return cmd
}

func renderPlainPrompt(b *rpb.Bundle) string {
	var sb strings.Builder
	if b.System != "" {
		sb.WriteString("## System\n")
		sb.WriteString(b.System)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## State Header\n")
	sb.WriteString(b.StateHeader)
	sb.WriteString("\n\n")
	sb.WriteString("## User\n")
	sb.WriteString(b.User)
	sb.WriteString("\n\n")
	if len(b.Previews) > 0 {
		sb.WriteString("## Artifacts\n")
		for _, p := range b.Previews {
			sb.WriteString(fmt.Sprintf("[artifact:%s]\n%s\n\n", p.ArtifactID, p.Excerpt))
		}
	}
	return sb.String()
}
