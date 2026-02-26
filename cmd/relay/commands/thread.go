package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func threadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Manage threads",
	}
	cmd.AddCommand(threadNewCmd())
	return cmd
}

func threadNewCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new thread",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			var result map[string]any
			if err := client.Post("/threads", map[string]string{"name": name}, &result); err != nil {
				return fmt.Errorf("create thread: %w", err)
			}

			threadID, _ := result["thread_id"].(string)
			fmt.Printf("  thread_id  %s\n", threadID)
			if name != "" {
				fmt.Printf("  name       %s\n", name)
			}
			fmt.Printf("  state_ref  %s\n", result["state_ref"])
			fmt.Printf("\n  relay show %s\n", threadID)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "optional thread name")
	return cmd
}

func runsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs",
		Short: "List recent threads/runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			var result struct {
				Threads []map[string]any `json:"threads"`
			}
			if err := client.Get("/threads", &result); err != nil {
				return err
			}

			if len(result.Threads) == 0 {
				fmt.Println("  No threads yet. Try: relay thread new")
				return nil
			}

			fmt.Printf("  %-38s  %-20s  %-6s  %s\n", "THREAD ID", "NAME", "HOPS", "CREATED")
			fmt.Printf("  %s  %s  %s  %s\n",
				strings.Repeat("-", 38), strings.Repeat("-", 20),
				strings.Repeat("-", 6), strings.Repeat("-", 19))
			for _, t := range result.Threads {
				id := fmt.Sprintf("%v", t["thread_id"])
				name := fmt.Sprintf("%v", t["name"])
				hops := fmt.Sprintf("%v", t["hop_count"])
				createdAt := fmt.Sprintf("%v", t["created_at"])
				if len(createdAt) > 19 {
					createdAt = createdAt[:19]
				}
				if name == "<nil>" || name == "" {
					name = "-"
				}
				fmt.Printf("  %-38s  %-20s  %-6s  %s\n", id, name, hops, createdAt)
			}
			return nil
		},
	}
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <thread_id>",
		Short: "Show a thread summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			threadID := args[0]

			var thread map[string]any
			if err := client.Get("/threads/"+threadID, &thread); err != nil {
				return err
			}

			var header map[string]any
			if err := client.Get("/threads/"+threadID+"/state/header", &header); err != nil {
				return err
			}

			var artResult struct {
				Artifacts []map[string]any `json:"artifacts"`
			}
			_ = client.Get("/threads/"+threadID+"/artifacts", &artResult)

			fmt.Printf("  Thread: %s\n\n", threadID)
			fmt.Printf("  %-20s %v\n", "name", thread["name"])
			fmt.Printf("  %-20s %v\n", "state version", thread["state_version"])
			fmt.Printf("  %-20s %v\n", "hop count", thread["hop_count"])
			fmt.Printf("  %-20s %v\n", "artifacts", thread["artifact_count"])
			fmt.Printf("  %-20s %v\n", "created", thread["created_at"])

			if facts, ok := header["top_facts"].([]any); ok && len(facts) > 0 {
				fmt.Printf("\n  Facts:\n")
				for _, f := range facts {
					if fm, ok := f.(map[string]any); ok {
						fmt.Printf("    %v: %v\n", fm["key"], fm["value"])
					}
				}
			}

			if len(artResult.Artifacts) > 0 {
				fmt.Printf("\n  Artifacts:\n")
				for _, a := range artResult.Artifacts {
					fmt.Printf("    %v  %-12v  %v\n", a["ref"], a["type"], a["name"])
				}
			}

			return nil
		},
	}
}

func tailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tail <thread_id>",
		Short: "Stream events for a thread (like tail -f)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)

			threadID := args[0]
			fmt.Printf("  tailing events for %s (Ctrl-C to stop)\n\n", threadID)

			var lastID string
			for {
				var result struct {
					Events []map[string]any `json:"events"`
				}
				path := "/threads/" + threadID + "/events"
				if lastID != "" {
					path += "?after=" + lastID
				}

				if err := client.Get(path, &result); err != nil {
					fmt.Fprintf(os.Stderr, "  error: %v\n", err)
					time.Sleep(2 * time.Second)
					continue
				}

				for _, ev := range result.Events {
					id := fmt.Sprintf("%v", ev["id"])
					ts := fmt.Sprintf("%v", ev["timestamp"])
					if len(ts) > 19 {
						ts = ts[:19]
					}
					evType := fmt.Sprintf("%v", ev["type"])
					payload := ev["payload"]
					payloadStr := ""
					if payload != nil {
						b, _ := json.Marshal(payload)
						payloadStr = truncateStr(string(b), 80)
					}
					fmt.Printf("  %s  %-30s  %s\n", ts, evType, payloadStr)
					lastID = id
				}

				time.Sleep(1 * time.Second)
			}
		},
	}
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <thread_id>",
		Short: "Open thread in the web UI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			url := fmt.Sprintf("%s/ui/#%s", DaemonURL(cfg), args[0])
			fmt.Printf("  opening %s\n", url)
			return openBrowser(url)
		},
	}
}

func openBrowser(url string) error {
	var openCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		openCmd = exec.Command("open", url)
	case "linux":
		openCmd = exec.Command("xdg-open", url)
	case "windows":
		openCmd = exec.Command("cmd", "/c", "start", url)
	default:
		fmt.Printf("  open %s in your browser\n", url)
		return nil
	}
	return openCmd.Start()
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
