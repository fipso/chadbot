package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

func registerSkills() {
	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_status",
		Description: "Check sandbox environment status. Returns daemon state and whether binaries are installed.",
		Parameters:  []*pb.SkillParameter{},
	}, handleStatus)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_pull",
		Description: "Pull a Docker image to the sandbox environment.",
		Parameters: []*pb.SkillParameter{
			{Name: "image", Type: "string", Description: "Image to pull (e.g., 'alpine:latest', 'ubuntu:22.04')", Required: true},
		},
	}, handlePull)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_run",
		Description: "Run a container in the sandbox. By default runs in foreground and waits for completion. Use detach=true for long-running services.",
		Parameters: []*pb.SkillParameter{
			{Name: "image", Type: "string", Description: "Image to run (e.g., 'alpine', 'nginx:alpine')", Required: true},
			{Name: "command", Type: "string", Description: "Command as JSON array (e.g., '[\"echo\", \"hello\"]'). If empty, uses image default.", Required: false},
			{Name: "rm", Type: "string", Description: "Remove container after exit: 'true' or 'false' (default: true)", Required: false},
			{Name: "detach", Type: "string", Description: "Run in background: 'true' or 'false' (default: false)", Required: false},
			{Name: "ports", Type: "string", Description: "Port mappings as JSON array (e.g., '[\"8080:80\", \"443:443\"]')", Required: false},
			{Name: "env", Type: "string", Description: "Environment variables as JSON array (e.g., '[\"FOO=bar\", \"DEBUG=1\"]')", Required: false},
			{Name: "name", Type: "string", Description: "Container name", Required: false},
		},
	}, handleRun)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_exec",
		Description: "Execute a command in a running container.",
		Parameters: []*pb.SkillParameter{
			{Name: "container", Type: "string", Description: "Container ID or name", Required: true},
			{Name: "command", Type: "string", Description: "Command as JSON array (e.g., '[\"ls\", \"-la\"]')", Required: true},
		},
	}, handleExec)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_images",
		Description: "List Docker images in the sandbox.",
		Parameters: []*pb.SkillParameter{
			{Name: "filter", Type: "string", Description: "Filter images by reference (e.g., 'alpine')", Required: false},
		},
	}, handleImages)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_ps",
		Description: "List containers in the sandbox.",
		Parameters: []*pb.SkillParameter{
			{Name: "all", Type: "string", Description: "Show all containers including stopped: 'true' or 'false' (default: false)", Required: false},
		},
	}, handlePs)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_logs",
		Description: "Get logs from a container.",
		Parameters: []*pb.SkillParameter{
			{Name: "container", Type: "string", Description: "Container ID or name", Required: true},
			{Name: "tail", Type: "string", Description: "Number of lines to show from end (e.g., '100')", Required: false},
		},
	}, handleLogs)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_stop",
		Description: "Stop a running container.",
		Parameters: []*pb.SkillParameter{
			{Name: "container", Type: "string", Description: "Container ID or name", Required: true},
		},
	}, handleStop)

	client.RegisterSkill(&pb.Skill{
		Name:        "sandbox_rm",
		Description: "Remove a container.",
		Parameters: []*pb.SkillParameter{
			{Name: "container", Type: "string", Description: "Container ID or name", Required: true},
			{Name: "force", Type: "string", Description: "Force remove running container: 'true' or 'false' (default: false)", Required: false},
		},
	}, handleRm)
}

func handleStatus(ctx context.Context, args map[string]string) (string, error) {
	result := map[string]interface{}{
		"base_dir": baseDir,
	}

	// Check binaries
	dockerdExists := false
	if _, err := os.Stat(getBinaryPath("dockerd")); err == nil {
		dockerdExists = true
	}
	runscExists := false
	if _, err := os.Stat(getRunscPath()); err == nil {
		runscExists = true
	}

	result["binaries"] = map[string]bool{
		"dockerd": dockerdExists,
		"runsc":   runscExists,
	}

	// Check daemon status
	daemonMu.Lock()
	result["daemon_running"] = daemonRunning
	daemonMu.Unlock()

	result["docker_socket"] = dockerSocket

	// Try to get Docker info if running
	if daemonRunning {
		if output, err := runDockerCommand(ctx, "info", "--format", "{{.ServerVersion}}"); err == nil {
			result["docker_version"] = strings.TrimSpace(output)
		}
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handlePull(ctx context.Context, args map[string]string) (string, error) {
	image := args["image"]
	if image == "" {
		return "", fmt.Errorf("image is required")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	output, err := runDockerCommand(ctx, "pull", image)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success": true,
		"image":   image,
		"output":  output,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleRun(ctx context.Context, args map[string]string) (string, error) {
	image := args["image"]
	if image == "" {
		return "", fmt.Errorf("image is required")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	// Build docker run command
	dockerArgs := []string{"run"}

	// Handle rm flag (default true)
	rm := args["rm"]
	if rm != "false" {
		dockerArgs = append(dockerArgs, "--rm")
	}

	// Handle detach flag
	detach := args["detach"] == "true"
	if detach {
		dockerArgs = append(dockerArgs, "-d")
	}

	// Handle name
	if name := args["name"]; name != "" {
		dockerArgs = append(dockerArgs, "--name", name)
	}

	// Handle ports
	if portsJSON := args["ports"]; portsJSON != "" {
		var ports []string
		if err := json.Unmarshal([]byte(portsJSON), &ports); err != nil {
			return "", fmt.Errorf("invalid ports JSON: %w", err)
		}
		for _, port := range ports {
			dockerArgs = append(dockerArgs, "-p", port)
		}
	}

	// Handle env
	if envJSON := args["env"]; envJSON != "" {
		var envVars []string
		if err := json.Unmarshal([]byte(envJSON), &envVars); err != nil {
			return "", fmt.Errorf("invalid env JSON: %w", err)
		}
		for _, e := range envVars {
			dockerArgs = append(dockerArgs, "-e", e)
		}
	}

	// Add image
	dockerArgs = append(dockerArgs, image)

	// Handle command
	if cmdJSON := args["command"]; cmdJSON != "" {
		var cmd []string
		if err := json.Unmarshal([]byte(cmdJSON), &cmd); err != nil {
			return "", fmt.Errorf("invalid command JSON: %w", err)
		}
		dockerArgs = append(dockerArgs, cmd...)
	}

	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":  true,
		"image":    image,
		"detached": detach,
		"output":   strings.TrimSpace(output),
	}

	if detach {
		result["container_id"] = strings.TrimSpace(output)
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleExec(ctx context.Context, args map[string]string) (string, error) {
	container := args["container"]
	if container == "" {
		return "", fmt.Errorf("container is required")
	}

	cmdJSON := args["command"]
	if cmdJSON == "" {
		return "", fmt.Errorf("command is required")
	}

	var cmd []string
	if err := json.Unmarshal([]byte(cmdJSON), &cmd); err != nil {
		return "", fmt.Errorf("invalid command JSON: %w", err)
	}

	if len(cmd) == 0 {
		return "", fmt.Errorf("command cannot be empty")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	dockerArgs := append([]string{"exec", container}, cmd...)
	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": container,
		"output":    output,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleImages(ctx context.Context, args map[string]string) (string, error) {
	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	dockerArgs := []string{"images", "--format", "{{json .}}"}

	if filter := args["filter"]; filter != "" {
		dockerArgs = append(dockerArgs, "--filter", "reference="+filter)
	}

	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	// Parse JSON lines output
	var images []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var img map[string]interface{}
		if err := json.Unmarshal([]byte(line), &img); err == nil {
			images = append(images, img)
		}
	}

	result := map[string]interface{}{
		"success": true,
		"count":   len(images),
		"images":  images,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handlePs(ctx context.Context, args map[string]string) (string, error) {
	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	dockerArgs := []string{"ps", "--format", "{{json .}}"}

	if args["all"] == "true" {
		dockerArgs = append(dockerArgs, "-a")
	}

	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	// Parse JSON lines output
	var containers []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var ctr map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ctr); err == nil {
			containers = append(containers, ctr)
		}
	}

	result := map[string]interface{}{
		"success":    true,
		"count":      len(containers),
		"containers": containers,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleLogs(ctx context.Context, args map[string]string) (string, error) {
	container := args["container"]
	if container == "" {
		return "", fmt.Errorf("container is required")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	dockerArgs := []string{"logs", container}

	if tail := args["tail"]; tail != "" {
		dockerArgs = append(dockerArgs, "--tail", tail)
	}

	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": container,
		"logs":      output,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleStop(ctx context.Context, args map[string]string) (string, error) {
	container := args["container"]
	if container == "" {
		return "", fmt.Errorf("container is required")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	output, err := runDockerCommand(ctx, "stop", container)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": container,
		"output":    strings.TrimSpace(output),
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleRm(ctx context.Context, args map[string]string) (string, error) {
	container := args["container"]
	if container == "" {
		return "", fmt.Errorf("container is required")
	}

	if err := ensureRunning(); err != nil {
		return "", fmt.Errorf("failed to start sandbox: %w", err)
	}

	dockerArgs := []string{"rm", container}

	if args["force"] == "true" {
		dockerArgs = append(dockerArgs, "-f")
	}

	output, err := runDockerCommand(ctx, dockerArgs...)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": container,
		"output":    strings.TrimSpace(output),
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}
