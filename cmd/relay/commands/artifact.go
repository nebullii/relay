package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/relaydev/relay/internal/artifacts"
	"github.com/spf13/cobra"
)

func artifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Manage artifacts",
	}
	cmd.AddCommand(artifactPutCmd(), artifactGetCmd())
	return cmd
}

func artifactPutCmd() *cobra.Command {
	var (
		threadID string
		artType  string
	)
	cmd := &cobra.Command{
		Use:   "put <file>",
		Short: "Upload a file as an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}

			filePath := args[0]
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()

			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			atype := artifacts.ArtifactType(artType)
			if atype == "" {
				atype = artifacts.TypeBinary
			}
			mime := "application/octet-stream"
			if atype != artifacts.TypeBinary {
				mime = "text/plain"
			}

			prov := artifacts.Provenance{
				CreatedBy: "cli",
				CreatedAt: time.Now().UTC(),
			}
			art, err := eng.ArtifactPut(threadID, filepath.Base(filePath), atype, mime, f, prov)
			if err != nil {
				return err
			}

			fmt.Printf("  artifact_ref  %v\n", art.Ref)
			fmt.Printf("  type          %v\n", art.Type)
			fmt.Printf("  size          %v bytes\n", art.Size)
			fmt.Printf("  hash          %v\n", art.Hash)
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	cmd.Flags().StringVar(&artType, "type", "", "artifact type (text, markdown, json, html, binary)")
	return cmd
}

func artifactGetCmd() *cobra.Command {
	var (
		threadID string
		outPath  string
	)
	cmd := &cobra.Command{
		Use:   "get <artifact_ref>",
		Short: "Download an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			if threadID == "" {
				return fmt.Errorf("--thread is required")
			}

			ref := args[0]
			eng, err := openEngine(cfg)
			if err != nil {
				return err
			}
			defer eng.Close()

			var out io.Writer = os.Stdout
			if outPath != "" {
				outFile, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer outFile.Close()
				out = outFile
			}

			data, err := eng.ArtifactContent(threadID, ref)
			if err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			n, err := out.Write(data)
			if err != nil {
				return fmt.Errorf("write output: %w", err)
			}

			if outPath != "" {
				fmt.Printf("  saved %d bytes to %s\n", n, outPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "thread ID (required)")
	cmd.Flags().StringVar(&outPath, "out", "", "output file path (default: stdout)")
	return cmd
}
