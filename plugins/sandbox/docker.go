package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

var (
	rootlessCmd  *exec.Cmd
	dockerClient *dockerclient.Client
)

// isRootless returns true if running in rootless mode (non-root user)
func isRootless() bool {
	return os.Getuid() != 0
}

// generateDaemonConfig creates daemon.json for rootless Docker
func generateDaemonConfig() error {
	var configContent string

	if isRootless() {
		log.Println("[Sandbox] Running in rootless mode - using runc runtime")
		configContent = fmt.Sprintf(`{
  "data-root": %q,
  "exec-root": %q,
  "runtimes": {
    "runsc": {
      "path": %q,
      "runtimeArgs": ["--platform=systrap"]
    }
  }
}`, dockerDir, filepath.Join(runDir, "docker"), getRunscPath())
	} else {
		log.Println("[Sandbox] Running as root - using gVisor runsc runtime")
		configContent = fmt.Sprintf(`{
  "data-root": %q,
  "exec-root": %q,
  "runtimes": {
    "runsc": {
      "path": %q,
      "runtimeArgs": ["--platform=systrap"]
    }
  },
  "default-runtime": "runsc"
}`, dockerDir, filepath.Join(runDir, "docker"), getRunscPath())
	}

	daemonPath := filepath.Join(configDir, "daemon.json")
	if err := os.WriteFile(daemonPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write daemon.json: %w", err)
	}
	log.Printf("[Sandbox] Generated daemon.json")

	return nil
}

// ensureRunning ensures the Docker daemon is running and client is connected
func ensureRunning() error {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if daemonRunning && dockerClient != nil {
		// Check if daemon is still alive
		if rootlessCmd != nil && rootlessCmd.Process != nil {
			if err := rootlessCmd.Process.Signal(syscall.Signal(0)); err != nil {
				daemonRunning = false
				dockerClient.Close()
				dockerClient = nil
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
	if err := generateDaemonConfig(); err != nil {
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
		return fmt.Errorf("rootless Docker failed to start: %w", err)
	}
	log.Println("[Sandbox] Rootless Docker daemon started")

	// Create Docker client
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("unix://"+dockerSocket),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	dockerClient = cli

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

	if dockerClient != nil {
		dockerClient.Close()
		dockerClient = nil
	}

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

// getClient returns the Docker client, ensuring daemon is running
func getClient() (*dockerclient.Client, error) {
	if err := ensureRunning(); err != nil {
		return nil, err
	}
	return dockerClient, nil
}

// PullImage pulls a Docker image
func PullImage(ctx context.Context, imageName string) error {
	cli, err := getClient()
	if err != nil {
		return err
	}

	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	// Consume the output (required for pull to complete)
	_, err = io.Copy(io.Discard, reader)
	return err
}

// ContainerConfig holds configuration for running a container
type ContainerConfig struct {
	Image   string
	Command []string
	Env     []string
	Ports   map[string]string // host:container
	Name    string
	Remove  bool
	Detach  bool
	Offline bool
}

// RunContainer creates and starts a container
func RunContainer(ctx context.Context, cfg ContainerConfig) (containerID string, output string, err error) {
	cli, err := getClient()
	if err != nil {
		return "", "", err
	}

	// Build container config
	containerCfg := &container.Config{
		Image: cfg.Image,
		Env:   cfg.Env,
	}
	if len(cfg.Command) > 0 {
		containerCfg.Cmd = cfg.Command
	}

	// Build host config
	hostCfg := &container.HostConfig{
		AutoRemove: cfg.Remove && cfg.Detach, // AutoRemove only works with detach
	}

	// Handle offline mode
	if cfg.Offline {
		hostCfg.NetworkMode = "none"
	}

	// Handle port mappings
	if len(cfg.Ports) > 0 {
		portBindings := nat.PortMap{}
		exposedPorts := nat.PortSet{}
		for hostPort, containerPort := range cfg.Ports {
			port, err := nat.NewPort("tcp", containerPort)
			if err != nil {
				return "", "", fmt.Errorf("invalid port %s: %w", containerPort, err)
			}
			portBindings[port] = []nat.PortBinding{{HostPort: hostPort}}
			exposedPorts[port] = struct{}{}
		}
		hostCfg.PortBindings = portBindings
		containerCfg.ExposedPorts = exposedPorts
	}

	// Create container
	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, cfg.Name)
	if err != nil {
		return "", "", fmt.Errorf("failed to create container: %w", err)
	}
	containerID = resp.ID

	// Start container
	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		// Clean up on failure
		cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
		return "", "", fmt.Errorf("failed to start container: %w", err)
	}

	if cfg.Detach {
		return containerID, "", nil
	}

	// Wait for container to finish
	statusCh, errCh := cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return containerID, "", fmt.Errorf("error waiting for container: %w", err)
		}
	case <-statusCh:
	}

	// Get logs
	logs, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return containerID, "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	logBytes, _ := io.ReadAll(logs)
	output = stripDockerLogHeaders(logBytes)

	// Remove container if requested (and not auto-removed)
	if cfg.Remove {
		cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	}

	return containerID, output, nil
}

// stripDockerLogHeaders removes the 8-byte header from each log line
func stripDockerLogHeaders(data []byte) string {
	var result []byte
	for len(data) >= 8 {
		// Header: [stream_type, 0, 0, 0, size1, size2, size3, size4]
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if len(data) < size {
			break
		}
		result = append(result, data[:size]...)
		data = data[size:]
	}
	return string(result)
}

// ExecInContainer executes a command in a running container
func ExecInContainer(ctx context.Context, containerID string, cmd []string) (string, error) {
	cli, err := getClient()
	if err != nil {
		return "", err
	}

	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach exec: %w", err)
	}
	defer resp.Close()

	output, _ := io.ReadAll(resp.Reader)
	return stripDockerLogHeaders(output), nil
}

// ListImages returns a list of images
func ListImages(ctx context.Context, filter string) ([]image.Summary, error) {
	cli, err := getClient()
	if err != nil {
		return nil, err
	}

	opts := image.ListOptions{}
	if filter != "" {
		opts.Filters.Add("reference", filter)
	}

	return cli.ImageList(ctx, opts)
}

// ListContainers returns a list of containers
func ListContainers(ctx context.Context, all bool) ([]container.Summary, error) {
	cli, err := getClient()
	if err != nil {
		return nil, err
	}

	return cli.ContainerList(ctx, container.ListOptions{All: all})
}

// GetContainerLogs returns logs from a container
func GetContainerLogs(ctx context.Context, containerID string, tail string) (string, error) {
	cli, err := getClient()
	if err != nil {
		return "", err
	}

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}
	if tail != "" {
		opts.Tail = tail
	}

	logs, err := cli.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	logBytes, _ := io.ReadAll(logs)
	return stripDockerLogHeaders(logBytes), nil
}

// StopContainer stops a running container
func StopContainer(ctx context.Context, containerID string) error {
	cli, err := getClient()
	if err != nil {
		return err
	}

	return cli.ContainerStop(ctx, containerID, container.StopOptions{})
}

// RemoveContainer removes a container
func RemoveContainer(ctx context.Context, containerID string, force bool) error {
	cli, err := getClient()
	if err != nil {
		return err
	}

	return cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force})
}

// GetDockerInfo returns Docker daemon info
func GetDockerInfo(ctx context.Context) (string, error) {
	cli, err := getClient()
	if err != nil {
		return "", err
	}

	info, err := cli.Info(ctx)
	if err != nil {
		return "", err
	}

	return info.ServerVersion, nil
}
