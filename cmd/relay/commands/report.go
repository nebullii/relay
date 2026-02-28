package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/events"
	"github.com/relaydev/relay/internal/state"
	"github.com/spf13/cobra"
)

func reportCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "report <thread_id>",
		Short: "Generate a report for a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			threadID := args[0]
			st, err := eng.State(threadID)
			if err != nil {
				return err
			}
			arts, _ := eng.ArtifactList(threadID)
			evs, _ := eng.Events(threadID, nil, 1000)

			naiveTokens := 0
			actualTokens := 0
			for _, a := range arts {
				naiveTokens += int(a.Size) / 4
				actualTokens += len(a.Preview.Text) / 4
			}
			avoided := naiveTokens - actualTokens

			content, mime, atype := buildReport(threadID, format, st, arts, evs, naiveTokens, actualTokens, avoided)
			prov := artifacts.Provenance{CreatedBy: "relay", CreatedAt: st.UpdatedAt}
			art, err := eng.ArtifactPut(threadID, "report."+format, atype, mime, strings.NewReader(content), prov)
			if err != nil {
				return err
			}

			fmt.Printf("  report generated\n")
			fmt.Printf("  artifact_ref  %v\n", art.Ref)
			fmt.Printf("  format        %v\n", format)
			fmt.Printf("  size          %v bytes\n", art.Size)
			fmt.Printf("\n  token savings:\n")
			fmt.Printf("    naive tokens   %v\n", naiveTokens)
			fmt.Printf("    actual tokens  %v\n", actualTokens)
			fmt.Printf("    avoided        %v\n", avoided)
			fmt.Printf("\n  relay artifact get %v --thread %s\n", art.Ref, threadID)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "md", "output format: md or json")
	return cmd
}

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats <thread_id>",
		Short: "Show token statistics for a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			threadID := args[0]
			arts, _ := eng.ArtifactList(threadID)
			evs, _ := eng.Events(threadID, nil, 1000)

			naiveTokens := 0
			actualTokens := 0
			for _, a := range arts {
				naiveTokens += int(a.Size) / 4
				actualTokens += len(a.Preview.Text) / 4
			}
			avoided := naiveTokens - actualTokens
			var reductionPct float64
			if naiveTokens > 0 {
				reductionPct = float64(avoided) / float64(naiveTokens) * 100
			}

			fmt.Printf("  Thread: %s\n\n", threadID)
			fmt.Printf("  %-32s %d\n", "artifacts", len(arts))
			fmt.Printf("  %-32s %d\n", "events", len(evs))
			fmt.Printf("\n  Token savings (computed from artifact data):\n")
			fmt.Printf("  %-32s %d\n", "naive tokens (if pasted)", naiveTokens)
			fmt.Printf("  %-32s %d\n", "actual tokens (previews only)", actualTokens)
			fmt.Printf("  %-32s %d\n", "tokens avoided", avoided)
			fmt.Printf("  %-32s %.1f%%\n", "reduction", reductionPct)
			return nil
		},
	}
}

func buildReport(threadID, format string, st *state.State, arts []*artifacts.Artifact, evs []*events.Event, naive, actual, avoided int) (content string, mime string, atype artifacts.ArtifactType) {
	if format == "json" {
		data, _ := json.MarshalIndent(map[string]any{
			"thread_id":      threadID,
			"state":          st,
			"artifact_count": len(arts),
			"event_count":    len(evs),
			"token_savings": map[string]any{
				"naive_tokens":   naive,
				"actual_tokens":  actual,
				"avoided_tokens": avoided,
			},
		}, "", "  ")
		return string(data), "application/json", artifacts.TypeJSON
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Relay Report: %s\n\n", threadID))
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("## State Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Version: %d\n", st.Version))
	sb.WriteString(fmt.Sprintf("- Facts: %d\n", len(st.Facts)))
	sb.WriteString(fmt.Sprintf("- Constraints: %d\n", len(st.Constraints)))
	sb.WriteString(fmt.Sprintf("- Open Questions: %d\n", len(st.OpenQuestions)))
	sb.WriteString(fmt.Sprintf("- Plan Steps: %d\n", len(st.Plan)))
	sb.WriteString("\n")
	sb.WriteString("## Token Savings\n\n")
	sb.WriteString(fmt.Sprintf("- Naive tokens (if pasted): %d\n", naive))
	sb.WriteString(fmt.Sprintf("- Actual tokens (refs+previews): %d\n", actual))
	sb.WriteString(fmt.Sprintf("- Tokens avoided: %d\n", avoided))
	return sb.String(), "text/markdown", artifacts.TypeMarkdown
}
