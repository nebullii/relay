package commands

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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

			client := NewClient(cfg)
			var result map[string]any
			if err := client.Post("/reports/"+args[0], map[string]string{"format": format}, &result); err != nil {
				return fmt.Errorf("generate report: %w", err)
			}

			fmt.Printf("  report generated\n")
			fmt.Printf("  artifact_ref  %v\n", result["artifact_ref"])
			fmt.Printf("  format        %v\n", result["format"])
			fmt.Printf("  size          %v bytes\n", result["size"])
			if savings, ok := result["token_savings"].(map[string]any); ok {
				fmt.Printf("\n  token savings:\n")
				fmt.Printf("    naive tokens   %v\n", savings["naive_tokens"])
				fmt.Printf("    actual tokens  %v\n", savings["actual_tokens"])
				fmt.Printf("    avoided        %v\n", savings["avoided_tokens"])
			}
			fmt.Printf("\n  relay artifact get %v --thread %s\n", result["artifact_ref"], args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "md", "output format: md or json")
	return cmd
}

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats <thread_id>",
		Short: "Show token and cache statistics for a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			client := NewClient(cfg)
			threadID := args[0]

			var header map[string]any
			if err := client.Get("/threads/"+threadID+"/state/header", &header); err != nil {
				return err
			}

			var artResult struct {
				Artifacts []map[string]any `json:"artifacts"`
			}
			_ = client.Get("/threads/"+threadID+"/artifacts", &artResult)

			var evResult struct {
				Events []map[string]any `json:"events"`
			}
			_ = client.Get("/threads/"+threadID+"/events", &evResult)

			// Calculate token stats
			naiveTokens := 0
			for _, a := range artResult.Artifacts {
				if size, ok := a["size"].(float64); ok {
					naiveTokens += int(size) / 4
				}
			}

			cacheHits := 0
			for _, ev := range evResult.Events {
				if ev["type"] == "capability.invoked" {
					if payload, ok := ev["payload"].(map[string]any); ok {
						if hit, _ := payload["cache_hit"].(bool); hit {
							cacheHits++
						}
					}
				}
			}

			metrics := map[string]any{}
			if m, ok := header["metrics"].(map[string]any); ok {
				metrics = m
			}

			fmt.Printf("  Thread: %s\n\n", threadID)
			fmt.Printf("  %-30s %d\n", "artifacts", len(artResult.Artifacts))
			fmt.Printf("  %-30s %d\n", "events", len(evResult.Events))
			fmt.Printf("  %-30s %d\n", "naive tokens (if pasted)", naiveTokens)
			fmt.Printf("  %-30s %v\n", "cache hits", metrics["cache_hits"])
			fmt.Printf("  %-30s %v\n", "cache misses", metrics["cache_misses"])
			fmt.Printf("  %-30s %v\n", "tokens avoided", metrics["tokens_avoided"])
			fmt.Printf("  %-30s %d\n", "session cache hits", cacheHits)
			return nil
		},
	}
}

func exportCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "export <thread_id>",
		Short: "Export a thread as a zip bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			threadID := args[0]
			if outPath == "" {
				outPath = fmt.Sprintf("relay-export-%s.zip", threadID[:8])
			}

			// Gather thread data
			client := NewClient(cfg)

			var thread map[string]any
			if err := client.Get("/threads/"+threadID, &thread); err != nil {
				return fmt.Errorf("get thread: %w", err)
			}

			var state map[string]any
			if err := client.Get("/threads/"+threadID+"/state", &state); err != nil {
				return fmt.Errorf("get state: %w", err)
			}

			var artResult struct {
				Artifacts []map[string]any `json:"artifacts"`
			}
			_ = client.Get("/threads/"+threadID+"/artifacts", &artResult)

			var evResult struct {
				Events []map[string]any `json:"events"`
			}
			_ = client.Get("/threads/"+threadID+"/events", &evResult)

			// Create zip
			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create zip: %w", err)
			}
			defer f.Close()

			zw := zip.NewWriter(f)
			defer zw.Close()

			// Write manifest
			manifest := map[string]any{
				"relay_version": Version,
				"thread_id":     threadID,
				"exported_at":   time.Now().UTC().Format(time.RFC3339),
				"artifact_count": len(artResult.Artifacts),
				"event_count":   len(evResult.Events),
			}
			writeZipJSON(zw, "manifest.json", manifest)
			writeZipJSON(zw, "thread.json", thread)
			writeZipJSON(zw, "state.json", state)
			writeZipJSON(zw, "events.json", evResult.Events)

			// Download and include artifacts
			for _, art := range artResult.Artifacts {
				ref := fmt.Sprintf("%v", art["ref"])
				artURL := fmt.Sprintf("%s/threads/%s/artifacts/%s?raw=1", DaemonURL(cfg), threadID, ref)

				req, _ := http.NewRequest("GET", artURL, nil)
				if cfg.APIToken != "" {
					req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
				}
				httpClient := &http.Client{Timeout: 30 * time.Second}
				resp, err := httpClient.Do(req)
				if err != nil {
					continue
				}

				fname := fmt.Sprintf("artifacts/%s", ref)
				w, err := zw.Create(fname)
				if err != nil {
					resp.Body.Close()
					continue
				}
				io.Copy(w, resp.Body)
				resp.Body.Close()
			}

			fmt.Printf("  exported %s\n", outPath)
			fmt.Printf("  artifacts: %d\n", len(artResult.Artifacts))
			fmt.Printf("  events:    %d\n", len(evResult.Events))
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "", "output zip path")
	return cmd
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <bundle.zip>",
		Short: "Import a thread bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			zipPath := args[0]
			zr, err := zip.OpenReader(zipPath)
			if err != nil {
				return fmt.Errorf("open zip: %w", err)
			}
			defer zr.Close()

			// Read manifest
			var manifest map[string]any
			for _, f := range zr.File {
				if f.Name == "manifest.json" {
					rc, _ := f.Open()
					data, _ := io.ReadAll(rc)
					rc.Close()
					json.Unmarshal(data, &manifest)
					break
				}
			}

			if manifest == nil {
				return fmt.Errorf("invalid bundle: no manifest.json")
			}

			origThreadID := fmt.Sprintf("%v", manifest["thread_id"])

			// Create new thread
			client := NewClient(cfg)
			var threadResult map[string]any
			if err := client.Post("/threads", map[string]string{
				"name": fmt.Sprintf("import of %s", origThreadID[:8]),
			}, &threadResult); err != nil {
				return fmt.Errorf("create thread: %w", err)
			}
			newThreadID := fmt.Sprintf("%v", threadResult["thread_id"])

			// Import state
			for _, f := range zr.File {
				if f.Name == "state.json" {
					rc, _ := f.Open()
					data, _ := io.ReadAll(rc)
					rc.Close()

					var state map[string]any
					if err := json.Unmarshal(data, &state); err != nil {
						continue
					}

					// Build patch to set facts, constraints etc from exported state
					patches := buildImportPatches(state)
					if len(patches) > 0 {
						client.Post("/threads/"+newThreadID+"/state/patch", patches, nil)
					}
					break
				}
			}

			// Import artifacts
			artCount := 0
			for _, f := range zr.File {
				if !strings.HasPrefix(f.Name, "artifacts/") {
					continue
				}

				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(rc)
				rc.Close()

				// Upload artifact
				uploadURL := fmt.Sprintf("%s/threads/%s/artifacts", DaemonURL(cfg), newThreadID)
				req, _ := http.NewRequest("POST", uploadURL, strings.NewReader(
					fmt.Sprintf(`{"name":"%s","type":"binary","mime":"application/octet-stream","content":%s}`,
						filepath.Base(f.Name), jsonStringify(data)),
				))
				req.Header.Set("Content-Type", "application/json")
				if cfg.APIToken != "" {
					req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
				}
				httpClient := &http.Client{Timeout: 30 * time.Second}
				resp, err := httpClient.Do(req)
				if err == nil {
					resp.Body.Close()
					artCount++
				}
			}

			fmt.Printf("  imported as thread %s\n", newThreadID)
			fmt.Printf("  artifacts: %d\n", artCount)
			fmt.Printf("\n  relay show %s\n", newThreadID)
			return nil
		},
	}
}

func writeZipJSON(zw *zip.Writer, name string, v any) {
	w, err := zw.Create(name)
	if err != nil {
		return
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	w.Write(data)
}

func buildImportPatches(state map[string]any) []map[string]any {
	var patches []map[string]any
	for _, field := range []string{"facts", "constraints", "open_questions", "decisions", "plan"} {
		if val, ok := state[field]; ok {
			data, _ := json.Marshal(val)
			patches = append(patches, map[string]any{
				"op":    "replace",
				"path":  "/" + field,
				"value": json.RawMessage(data),
			})
		}
	}
	return patches
}

func jsonStringify(data []byte) string {
	encoded, _ := json.Marshal(string(data))
	return string(encoded)
}
