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
  relay up            Start the daemon
  relay thread new    Create a new thread
  relay open <id>     Open thread in browser UI`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Register all subcommands
	rootCmd.AddCommand(
		versionCmd(),
		initCmd(),
		upCmd(),
		downCmd(),
		statusCmd(),
		doctorCmd(),
		threadCmd(),
		artifactCmd(),
		stateCmd(),
		capCmd(),
		runsCmd(),
		showCmd(),
		tailCmd(),
		reportCmd(),
		openCmd(),
		exportCmd(),
		importCmd(),
		statsCmd(),
		proxyCmd(),
		DaemonRunCmd(),
	)

	return rootCmd
}
