package commands

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/relaydev/relay/internal/proxy"
	"github.com/spf13/cobra"
)

func proxyCmd() *cobra.Command {
	var port int
	var threadID string

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "HTTPS forward proxy — intercepts all traffic and stores responses as artifacts",
		Long: `Start an HTTP/HTTPS forward proxy. All traffic is intercepted, responses
are stored as relay artifacts, and requests pass through transparently.

  relay proxy

Then set these environment variables once in your shell:
  export HTTP_PROXY=http://localhost:7475
  export HTTPS_PROXY=http://localhost:7475

Every HTTP and HTTPS call made by any process (OpenClaw, curl, Python, Node)
will be intercepted and stored. Run "relay stats <thread_id>" to see savings.

First run: relay generates a local CA certificate. Add it to your system trust
store once and HTTPS interception works for all future sessions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForwardProxy(port, threadID)
		},
	}

	cmd.Flags().IntVar(&port, "port", 7475, "Proxy port")
	cmd.Flags().StringVar(&threadID, "thread", "", "Relay thread ID (auto-created if not set)")

	return cmd
}

func runForwardProxy(port int, existingThreadID string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	client := NewClient(cfg)

	if err := client.Health(); err != nil {
		return fmt.Errorf("relay daemon not running — start it with: relay up")
	}

	// Ensure CA cert exists.
	caDir := cfg.BaseDir
	if caDir == "" {
		home, _ := os.UserHomeDir()
		caDir = filepath.Join(home, ".relay")
	}
	if err := os.MkdirAll(caDir, 0755); err != nil {
		return err
	}
	certPath := filepath.Join(caDir, "ca.crt")
	keyPath := filepath.Join(caDir, "ca.key")

	ca, err := proxy.LoadOrCreateCA(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("setup CA: %w", err)
	}

	// Create or reuse relay thread.
	tid := existingThreadID
	if tid == "" {
		var thread struct {
			ThreadID string `json:"thread_id"`
		}
		if err := client.Post("/threads", map[string]string{"name": "proxy"}, &thread); err != nil {
			return fmt.Errorf("create thread: %w", err)
		}
		tid = thread.ThreadID
	}

	// Stats counters.
	var requests, stored atomic.Int64
	var naiveBytes, previewBytes atomic.Int64

	interceptor := &proxy.Interceptor{
		CA: ca,
		OnResponse: func(evt proxy.ResponseEvent) {
			requests.Add(1)

			name := strings.TrimPrefix(evt.Path, "/")
			if name == "" {
				name = "response"
			}
			artType, artMime := "json", "application/json"
			if strings.Contains(strings.ToLower(evt.ContentType), "text/") {
				artType, artMime = "text", "text/plain"
			}

			var artResult struct {
				Preview struct {
					Text string `json:"text"`
				} `json:"preview"`
			}
			err := client.Post(fmt.Sprintf("/threads/%s/artifacts", tid), map[string]any{
				"name":    evt.Host + "/" + name,
				"type":    artType,
				"mime":    artMime,
				"content": string(evt.Body),
			}, &artResult)

			if err == nil {
				stored.Add(1)
				nb := int64(len(evt.Body))
				pb := int64(len(artResult.Preview.Text))
				naiveBytes.Add(nb)
				previewBytes.Add(pb)

				naiveTok := nb / 4
				actualTok := pb / 4
				avoided := naiveTok - actualTok
				if naiveTok > 0 {
					pct := float64(avoided) / float64(naiveTok) * 100
					fmt.Printf("  ✓  %-50s  %6d bytes  →  %5d tokens avoided  (%.0f%%)\n",
						evt.Host+evt.Path, len(evt.Body), avoided, pct)
				}
			}
		},
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: interceptor,
	}

	// Print startup info.
	fmt.Printf("relay proxy\n\n")
	fmt.Printf("  listen    http://localhost:%d\n", port)
	fmt.Printf("  thread    %s\n\n", tid)
	fmt.Printf("Set once in your shell:\n")
	fmt.Printf("  export HTTP_PROXY=http://localhost:%d\n", port)
	fmt.Printf("  export HTTPS_PROXY=http://localhost:%d\n\n", port)

	// CA trust instructions (one-time, per platform).
	if _, err := os.Stat(certPath); err == nil {
		fmt.Printf("CA cert: %s\n", certPath)
		fmt.Printf("Trust it once (required for HTTPS):\n")
		switch runtime.GOOS {
		case "darwin":
			fmt.Printf("  sudo security add-trusted-cert -d -r trustRoot \\\n")
			fmt.Printf("    -k /Library/Keychains/System.keychain %s\n", certPath)
		case "linux":
			fmt.Printf("  sudo cp %s /usr/local/share/ca-certificates/relay-ca.crt\n", certPath)
			fmt.Printf("  sudo update-ca-certificates\n")
		case "windows":
			fmt.Printf("  certutil -addstore -f ROOT %s\n", certPath)
		}
		fmt.Printf("\nCtrl+C to stop.\n\n")
	}

	// Print summary on exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Printf("\n\n── session summary ──\n")
		fmt.Printf("  requests intercepted  : %d\n", requests.Load())
		fmt.Printf("  artifacts stored      : %d\n", stored.Load())
		nb := naiveBytes.Load()
		pb := previewBytes.Load()
		if nb > 0 {
			naiveTok := nb / 4
			actualTok := pb / 4
			avoided := naiveTok - actualTok
			pct := float64(avoided) / float64(naiveTok) * 100
			fmt.Printf("  naive tokens          : %d\n", naiveTok)
			fmt.Printf("  actual tokens         : %d\n", actualTok)
			fmt.Printf("  tokens avoided        : %d (%.1f%%)\n", avoided, pct)
		}
		fmt.Printf("\n  relay stats %s\n", tid)
		srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
