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
			{Name: "offline", Type: "string", Description: "Disable network access: 'true' or 'false' (default: false)", Required: false},
			{Name: "ports", Type: "string", Description: "Port mappings as JSON object (e.g., '{\"8080\": \"80\", \"443\": \"443\"}')", Required: false},
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
	dockerdExists := fileExists(getBinaryPath("dockerd"))
	runscExists := fileExists(getRunscPath())

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
		if version, err := GetDockerInfo(ctx); err == nil {
			result["docker_version"] = version
		}
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handlePull(ctx context.Context, args map[string]string) (string, error) {
	imageName := args["image"]
	if imageName == "" {
		return "", fmt.Errorf("image is required")
	}

	if err := PullImage(ctx, imageName); err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success": true,
		"image":   imageName,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleRun(ctx context.Context, args map[string]string) (string, error) {
	imageName := args["image"]
	if imageName == "" {
		return "", fmt.Errorf("image is required")
	}

	cfg := ContainerConfig{
		Image:   imageName,
		Remove:  args["rm"] != "false",
		Detach:  args["detach"] == "true",
		Offline: args["offline"] == "true",
		Name:    args["name"],
	}

	// Parse command
	if cmdJSON := args["command"]; cmdJSON != "" {
		var cmd []string
		if err := json.Unmarshal([]byte(cmdJSON), &cmd); err != nil {
			return "", fmt.Errorf("invalid command JSON: %w", err)
		}
		cfg.Command = cmd
	}

	// Parse env
	if envJSON := args["env"]; envJSON != "" {
		var envVars []string
		if err := json.Unmarshal([]byte(envJSON), &envVars); err != nil {
			return "", fmt.Errorf("invalid env JSON: %w", err)
		}
		cfg.Env = envVars
	}

	// Parse ports (now a map)
	if portsJSON := args["ports"]; portsJSON != "" {
		var ports map[string]string
		if err := json.Unmarshal([]byte(portsJSON), &ports); err != nil {
			return "", fmt.Errorf("invalid ports JSON: %w", err)
		}
		cfg.Ports = ports
	}

	containerID, output, err := RunContainer(ctx, cfg)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":      true,
		"image":        imageName,
		"container_id": containerID,
		"detached":     cfg.Detach,
	}

	if !cfg.Detach {
		result["output"] = strings.TrimSpace(output)
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleExec(ctx context.Context, args map[string]string) (string, error) {
	containerID := args["container"]
	if containerID == "" {
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

	output, err := ExecInContainer(ctx, containerID, cmd)
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": containerID,
		"output":    output,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleImages(ctx context.Context, args map[string]string) (string, error) {
	images, err := ListImages(ctx, args["filter"])
	if err != nil {
		return "", err
	}

	// Convert to simpler format
	type imageInfo struct {
		ID      string   `json:"id"`
		Tags    []string `json:"tags"`
		Size    int64    `json:"size"`
		Created int64    `json:"created"`
	}

	var imageList []imageInfo
	for _, img := range images {
		imageList = append(imageList, imageInfo{
			ID:      img.ID,
			Tags:    img.RepoTags,
			Size:    img.Size,
			Created: img.Created,
		})
	}

	result := map[string]interface{}{
		"success": true,
		"count":   len(imageList),
		"images":  imageList,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handlePs(ctx context.Context, args map[string]string) (string, error) {
	containers, err := ListContainers(ctx, args["all"] == "true")
	if err != nil {
		return "", err
	}

	// Convert to simpler format
	type containerInfo struct {
		ID      string   `json:"id"`
		Names   []string `json:"names"`
		Image   string   `json:"image"`
		State   string   `json:"state"`
		Status  string   `json:"status"`
		Created int64    `json:"created"`
	}

	var containerList []containerInfo
	for _, ctr := range containers {
		containerList = append(containerList, containerInfo{
			ID:      ctr.ID,
			Names:   ctr.Names,
			Image:   ctr.Image,
			State:   ctr.State,
			Status:  ctr.Status,
			Created: ctr.Created,
		})
	}

	result := map[string]interface{}{
		"success":    true,
		"count":      len(containerList),
		"containers": containerList,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleLogs(ctx context.Context, args map[string]string) (string, error) {
	containerID := args["container"]
	if containerID == "" {
		return "", fmt.Errorf("container is required")
	}

	logs, err := GetContainerLogs(ctx, containerID, args["tail"])
	if err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": containerID,
		"logs":      logs,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleStop(ctx context.Context, args map[string]string) (string, error) {
	containerID := args["container"]
	if containerID == "" {
		return "", fmt.Errorf("container is required")
	}

	if err := StopContainer(ctx, containerID); err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": containerID,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleRm(ctx context.Context, args map[string]string) (string, error) {
	containerID := args["container"]
	if containerID == "" {
		return "", fmt.Errorf("container is required")
	}

	if err := RemoveContainer(ctx, containerID, args["force"] == "true"); err != nil {
		return "", err
	}

	result := map[string]interface{}{
		"success":   true,
		"container": containerID,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
