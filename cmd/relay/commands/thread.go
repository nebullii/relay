package commands

import (
	"fmt"
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
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			t, st, err := eng.CreateThread(name)
			if err != nil {
				return fmt.Errorf("create thread: %w", err)
			}

			fmt.Printf("  thread_id  %s\n", t.ID)
			if t.Name != "" {
				fmt.Printf("  name       %s\n", t.Name)
			}
			fmt.Printf("  state_ref  v%d\n", st.Version)
			fmt.Printf("\n  relay show %s\n", t.ID)
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
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			threads, err := eng.ListThreads(100)
			if err != nil {
				return err
			}
			if len(threads) == 0 {
				fmt.Println("  No threads yet. Try: relay thread new")
				return nil
			}

			fmt.Printf("  %-38s  %-20s  %-6s  %s\n", "THREAD ID", "NAME", "HOPS", "CREATED")
			fmt.Printf("  %s  %s  %s  %s\n",
				strings.Repeat("-", 38), strings.Repeat("-", 20),
				strings.Repeat("-", 6), strings.Repeat("-", 19))
			for _, t := range threads {
				name := t.Name
				if name == "" {
					name = "-"
				}
				createdAt := t.CreatedAt.Format("2006-01-02T15:04:05")
				fmt.Printf("  %-38s  %-20s  %-6d  %s\n", t.ID, name, t.HopCount, createdAt)
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
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			threadID := args[0]
			t, err := eng.GetThread(threadID)
			if err != nil {
				return err
			}
			header, err := eng.StateHeader(threadID)
			if err != nil {
				return err
			}
			arts, err := eng.ArtifactList(threadID)
			if err != nil {
				return err
			}
			evs, err := eng.Events(threadID, nil, 1000)
			if err != nil {
				return err
			}

			fmt.Printf("  Thread: %s\n\n", t.ID)
			fmt.Printf("  %-20s %v\n", "name", t.Name)
			fmt.Printf("  %-20s %v\n", "created", t.CreatedAt.Format(time.RFC3339))
			fmt.Printf("  %-20s %v\n", "hop count", t.HopCount)
			fmt.Printf("  %-20s %v\n", "artifacts", len(arts))
			fmt.Printf("  %-20s %v\n", "events", len(evs))
			if header != nil && header.Truncated {
				fmt.Printf("  %-20s %v\n", "header", "truncated")
			}

			if len(arts) > 0 {
				fmt.Printf("\n  Artifacts:\n")
				for _, a := range arts {
					fmt.Printf("    %s  %-12s  %s\n", a.Ref, a.Type, a.Name)
				}
			}
			return nil
		},
	}
}
