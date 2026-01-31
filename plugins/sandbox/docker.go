package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

var (
	rootlessCmd *exec.Cmd
)

// daemonJSON is the Docker daemon configuration
type daemonJSON struct {
	DataRoot       string                   `json:"data-root"`
	ExecRoot       string                   `json:"exec-root"`
	Runtimes       map[string]runtimeConfig `json:"runtimes"`
	DefaultRuntime string                   `json:"default-runtime"`
}

type runtimeConfig struct {
	Path        string   `json:"path"`
	RuntimeArgs []string `json:"runtimeArgs,omitempty"`
}

// isRootless returns true if running in rootless mode (non-root user)
func isRootless() bool {
	return os.Getuid() != 0
}

// generateConfigs creates daemon.json for rootless Docker
func generateConfigs() error {
	// Generate daemon.json
	// In rootless mode, gVisor's runsc doesn't support OCI create lifecycle
	// So we use runc (which still benefits from user namespace isolation via rootlesskit)
	// When running as root, we can use runsc for full gVisor protection
	var daemonConfig daemonJSON

	if isRootless() {
		// Rootless mode: use runc (gVisor doesn't support rootless OCI create)
		// User namespace isolation is still provided by rootlesskit
		log.Println("[Sandbox] Running in rootless mode - using runc runtime")
		daemonConfig = daemonJSON{
			DataRoot: dockerDir,
			ExecRoot: filepath.Join(runDir, "docker"),
			Runtimes: map[string]runtimeConfig{
				"runsc": {
					Path:        getRunscPath(),
					RuntimeArgs: []string{"--platform=systrap"},
				},
			},
			// Use default runc for rootless
			DefaultRuntime: "",
		}
	} else {
		// Root mode: use gVisor runsc for full syscall interception
		log.Println("[Sandbox] Running as root - using gVisor runsc runtime")
		daemonConfig = daemonJSON{
			DataRoot: dockerDir,
			ExecRoot: filepath.Join(runDir, "docker"),
			Runtimes: map[string]runtimeConfig{
				"runsc": {
					Path:        getRunscPath(),
					RuntimeArgs: []string{"--platform=systrap"},
				},
			},
			DefaultRuntime: "runsc",
		}
	}

	daemonPath := filepath.Join(configDir, "daemon.json")
	data, err := json.MarshalIndent(daemonConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal daemon.json: %w", err)
	}

	if err := os.WriteFile(daemonPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write daemon.json: %w", err)
	}
	log.Printf("[Sandbox] Generated daemon.json")

	return nil
}

// ensureRunning ensures the Docker daemon is running in rootless mode
func ensureRunning() error {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if daemonRunning {
		// Check if daemon is still alive
		if rootlessCmd != nil && rootlessCmd.Process != nil {
			if err := rootlessCmd.Process.Signal(syscall.Signal(0)); err != nil {
				daemonRunning = false
			}
		}
		if daemonRunning {
			return nil
		}
	}

	// Ensure binaries are downloaded
	if err := ensureBinaries(); err != nil {
		return err
	}

	// Generate configs
	if err := generateConfigs(); err != nil {
		return err
	}

	// Ensure runtime directory exists
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	// Start dockerd-rootless.sh
	log.Println("[Sandbox] Starting rootless Docker daemon...")
	rootlessScript := getBinaryPath("dockerd-rootless.sh")

	rootlessCmd = exec.Command(rootlessScript)

	// Set up environment for rootless mode
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("PATH=%s:%s:%s", binDir, runscDir, os.Getenv("PATH")),
		fmt.Sprintf("XDG_RUNTIME_DIR=%s", runDir),
		fmt.Sprintf("DOCKER_HOST=unix://%s", dockerSocket),
	)
	rootlessCmd.Env = env

	// Pass daemon config via environment
	rootlessCmd.Args = append(rootlessCmd.Args,
		"--config-file", filepath.Join(configDir, "daemon.json"),
	)

	rootlessCmd.Stdout = os.Stdout
	rootlessCmd.Stderr = os.Stderr

	if err := rootlessCmd.Start(); err != nil {
		return fmt.Errorf("failed to start rootless Docker: %w", err)
	}

	// Wait for docker socket
	if err := waitForSocket(dockerSocket, 60*time.Second); err != nil {
		// Try to get more info about the failure
		return fmt.Errorf("rootless Docker failed to start: %w", err)
	}
	log.Println("[Sandbox] Rootless Docker daemon started")

	daemonRunning = true
	return nil
}

// waitForSocket waits for a Unix socket to become available
func waitForSocket(socketPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for socket %s", socketPath)
		case <-ticker.C:
			conn, err := net.DialTimeout("unix", socketPath, time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}
}

// stopDaemons stops the Docker daemon gracefully
func stopDaemons() {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if rootlessCmd != nil && rootlessCmd.Process != nil {
		log.Println("[Sandbox] Stopping rootless Docker daemon...")

		// First try SIGTERM
		rootlessCmd.Process.Signal(syscall.SIGTERM)

		done := make(chan error, 1)
		go func() { done <- rootlessCmd.Wait() }()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			// Force kill if SIGTERM didn't work
			rootlessCmd.Process.Kill()
			<-done
		}
		rootlessCmd = nil
	}

	daemonRunning = false

	// Clean up socket files
	os.Remove(dockerSocket)
}

// getDockerCLI returns the path to docker CLI and sets up environment
func getDockerCLI() (string, []string) {
	return getBinaryPath("docker"), []string{
		fmt.Sprintf("DOCKER_HOST=unix://%s", dockerSocket),
		fmt.Sprintf("PATH=%s:%s", binDir, os.Getenv("PATH")),
	}
}

// runDockerCommand runs a docker command and returns the output
func runDockerCommand(ctx context.Context, args ...string) (string, error) {
	dockerPath, env := getDockerCLI()

	cmd := exec.CommandContext(ctx, dockerPath, args...)
	cmd.Env = append(os.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker command failed: %w: %s", err, string(output))
	}

	return string(output), nil
}
