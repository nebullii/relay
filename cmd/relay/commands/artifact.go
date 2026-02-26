package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

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

			// Build multipart form
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)

			part, err := writer.CreateFormFile("file", filepath.Base(filePath))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, f); err != nil {
				return err
			}
			if artType != "" {
				writer.WriteField("type", artType)
			}
			writer.Close()

			// Post with multipart
			url := fmt.Sprintf("%s/threads/%s/artifacts", DaemonURL(cfg), threadID)
			req, err := http.NewRequest("POST", url, &body)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if cfg.APIToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
			}

			httpClient := &http.Client{Timeout: 60 * time.Second}
			resp, err := httpClient.Do(req)
			if err != nil {
				return fmt.Errorf("upload: %w", err)
			}
			defer resp.Body.Close()

			respData, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 400 {
				return fmt.Errorf("upload failed: %s", string(respData))
			}

			var result map[string]any
			if err := json.Unmarshal(respData, &result); err != nil {
				fmt.Printf("  uploaded: %s\n", string(respData))
				return nil
			}

			fmt.Printf("  artifact_ref  %v\n", result["ref"])
			fmt.Printf("  type          %v\n", result["type"])
			fmt.Printf("  size          %v bytes\n", result["size"])
			fmt.Printf("  hash          %v\n", result["hash"])
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
			url := fmt.Sprintf("%s/threads/%s/artifacts/%s?raw=1", DaemonURL(cfg), threadID, ref)

			req, _ := http.NewRequest("GET", url, nil)
			if cfg.APIToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
			}

			httpClient := &http.Client{Timeout: 60 * time.Second}
			resp, err := httpClient.Do(req)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				data, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("download failed: %s", string(data))
			}

			var out io.Writer = os.Stdout
			if outPath != "" {
				outFile, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer outFile.Close()
				out = outFile
			}

			n, err := io.Copy(out, resp.Body)
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
