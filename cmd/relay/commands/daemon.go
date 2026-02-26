package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/relaydev/relay/internal/daemon"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize relay config and storage directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := DefaultConfig()
			if err := os.MkdirAll(cfg.BaseDir, 0755); err != nil {
				return fmt.Errorf("create base dir: %w", err)
			}
			// Create threads dir
			if err := os.MkdirAll(filepath.Join(cfg.BaseDir, "threads"), 0755); err != nil {
				return fmt.Errorf("create threads dir: %w", err)
			}
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("  relay initialized\n")
			fmt.Printf("  storage: %s\n", cfg.BaseDir)
			fmt.Printf("  config:  %s\n\n", ConfigPath())
			fmt.Printf("  Next: relay up\n")
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the relay daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			// Check if already running
			if pid := readPID(cfg); pid > 0 {
				if isRunning(pid) {
					fmt.Printf("  relay daemon already running (pid %d)\n", pid)
					fmt.Printf("  url: %s\n", DaemonURL(cfg))
					return nil
				}
			}

			// Determine port, handle conflicts
			port := cfg.Port
			for i := 0; i < 10; i++ {
				if !portInUse(port) {
					break
				}
				port++
			}
			cfg.Port = port

			// Start daemon in background
			self, err := os.Executable()
			if err != nil {
				self = os.Args[0]
			}

			logPath := filepath.Join(cfg.BaseDir, "daemon.log")

			daemonCmd := exec.Command(self, "daemon-run",
				"--port", strconv.Itoa(port),
				"--base-dir", cfg.BaseDir,
			)
			daemonCmd.Stdout, _ = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			daemonCmd.Stderr = daemonCmd.Stdout

			if err := daemonCmd.Start(); err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}

			// Write PID
			writePID(cfg, daemonCmd.Process.Pid)

			// Wait for daemon to be ready
			url := DaemonURL(cfg)
			ready := waitForDaemon(url, 10*time.Second)
			if !ready {
				return fmt.Errorf("daemon did not start within 10s — check %s", logPath)
			}

			fmt.Printf("  relay daemon started\n")
			fmt.Printf("  url:     %s\n", url)
			fmt.Printf("  pid:     %d\n", daemonCmd.Process.Pid)
			fmt.Printf("  storage: %s\n", cfg.BaseDir)
			fmt.Printf("  log:     %s\n\n", logPath)
			fmt.Printf("  relay thread new   # create a thread\n")
			fmt.Printf("  relay down         # stop the daemon\n")
			return nil
		},
	}
	return cmd
}

// daemonRunCmd is the internal command that actually runs the daemon (not exposed to users).
func DaemonRunCmd() *cobra.Command {
	var port int
	var baseDir string

	cmd := &cobra.Command{
		Use:    "daemon-run",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				cfg = DefaultConfig()
			}
			if port > 0 {
				cfg.Port = port
			}
			if baseDir != "" {
				cfg.BaseDir = baseDir
			}

			db, err := daemon.OpenDB(cfg.BaseDir)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()

			srv, err := daemon.New(db, daemon.Config{
				BaseDir:  cfg.BaseDir,
				APIToken: cfg.APIToken,
			})
			if err != nil {
				return fmt.Errorf("create server: %w", err)
			}

			addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
			httpSrv := &http.Server{
				Addr:    addr,
				Handler: srv,
			}

			// Graceful shutdown
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			go func() {
				<-ctx.Done()
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				httpSrv.Shutdown(shutCtx)
			}()

			fmt.Printf("relay daemon listening on %s\n", addr)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("listen: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", DefaultPort, "port to listen on")
	cmd.Flags().StringVar(&baseDir, "base-dir", "", "base directory for storage")
	return cmd
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the relay daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			pid := readPID(cfg)
			if pid <= 0 {
				fmt.Println("  relay daemon is not running")
				return nil
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				fmt.Println("  relay daemon is not running")
				clearPID(cfg)
				return nil
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				fmt.Printf("  failed to stop daemon (pid %d): %v\n", pid, err)
				return nil
			}

			clearPID(cfg)
			fmt.Printf("  relay daemon stopped (pid %d)\n", pid)
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status, version, and storage info",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			fmt.Printf("  relay status\n\n")
			fmt.Printf("  %-16s %s\n", "version", Version)
			fmt.Printf("  %-16s %s\n", "storage", cfg.BaseDir)
			fmt.Printf("  %-16s %s\n", "url", DaemonURL(cfg))
			fmt.Printf("  %-16s %d\n", "port", cfg.Port)

			pid := readPID(cfg)
			if pid > 0 && isRunning(pid) {
				fmt.Printf("  %-16s running (pid %d)\n", "daemon", pid)
			} else {
				fmt.Printf("  %-16s stopped\n", "daemon")
			}

			// Try to ping
			client := NewClient(cfg)
			if err := client.Health(); err != nil {
				fmt.Printf("  %-16s unreachable\n", "health")
			} else {
				fmt.Printf("  %-16s ok\n", "health")
			}

			return nil
		},
	}
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics: ports, permissions, versions, storage health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				cfg = DefaultConfig()
			}

			fmt.Printf("  relay doctor\n\n")

			check := func(label string, ok bool, detail string) {
				status := "  PASS"
				if !ok {
					status = "  FAIL"
				}
				fmt.Printf("  %s  %-40s %s\n", status, label, detail)
			}

			// Go version
			check("runtime", true, runtime.Version())

			// Config path
			_, cfgErr := os.Stat(ConfigPath())
			check("config file", cfgErr == nil, ConfigPath())

			// Storage dir
			_, dirErr := os.Stat(cfg.BaseDir)
			check("storage dir", dirErr == nil, cfg.BaseDir)

			// Storage write permission
			testFile := filepath.Join(cfg.BaseDir, ".write-test")
			writeErr := os.WriteFile(testFile, []byte("test"), 0644)
			if writeErr == nil {
				os.Remove(testFile)
			}
			check("storage writable", writeErr == nil, "")

			// Daemon running (check before port)
			pid := readPID(cfg)
			daemonRunning := pid > 0 && isRunning(pid)
			check("daemon running", daemonRunning, fmt.Sprintf("pid %d", pid))

			// Port: if daemon is running, port being in use is expected/good
			portUsed := portInUse(cfg.Port)
			if daemonRunning {
				check("port in use (daemon)", portUsed, fmt.Sprintf(":%d", cfg.Port))
			} else {
				check("port available", !portUsed, fmt.Sprintf(":%d", cfg.Port))
			}

			// Health endpoint
			if daemonRunning {
				client := NewClient(cfg)
				healthErr := client.Health()
				check("daemon health", healthErr == nil, DaemonURL(cfg))
			}

			// DB file
			dbPath := filepath.Join(cfg.BaseDir, "relay.db")
			_, dbErr := os.Stat(dbPath)
			check("database", dbErr == nil, dbPath)

			fmt.Println()
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("relay version %s\n", Version)
			fmt.Printf("built for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

// --- PID file helpers ---

func writePID(cfg *Config, pid int) {
	os.WriteFile(PIDFile(cfg), []byte(strconv.Itoa(pid)), 0644)
}

func readPID(cfg *Config) int {
	data, err := os.ReadFile(PIDFile(cfg))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func clearPID(cfg *Config) {
	os.Remove(PIDFile(cfg))
}

func isRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func portInUse(port int) bool {
	addr := fmt.Sprintf("localhost:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func waitForDaemon(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "/health")
		if err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
