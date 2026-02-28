package commands

import (
	"github.com/spf13/cobra"
)

var rootCmd *cobra.Command

func Root() *cobra.Command {
	if rootCmd != nil {
		return rootCmd
	}

	rootCmd = &cobra.Command{
		Use:   "relay",
		Short: "relay — zero-overhead agent memory and coordination",
		Long: `relay reduces LLM/agent token exchange by replacing prose with
durable state refs, artifact refs, typed schemas, and caching.

Agents never re-send memory. Everything is stored once and referenced by ID.

Quick start:
  relay init          Initialize config and storage
  relay thread new    Create a new thread
  relay "<prompt>"    Run a prompt with bounded memory`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runPrompt(args, "", "", true, false)
		},
	}

	// Register all subcommands
	rootCmd.AddCommand(
		versionCmd(),
		initCmd(),
		threadCmd(),
		runCmd(),
		wrapCmd(),
		promptCmd(),
		artifactCmd(),
		stateCmd(),
		runsCmd(),
		showCmd(),
		reportCmd(),
		statsCmd(),
	)

	return rootCmd
}
