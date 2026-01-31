package main

import (
	"context"
	_ "embed"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"github.com/fipso/chadbot/pkg/sdk"
)

//go:embed PLUGIN.md
var pluginDocumentation string

var (
	client *sdk.Client

	// Paths
	baseDir   string
	binDir    string
	runscDir  string
	dockerDir string
	runDir    string
	configDir string

	// Daemon state
	daemonMu      sync.Mutex
	daemonRunning bool
	dockerSocket  string
)

func init() {
	// Set up paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("Failed to get home directory:", err)
	}

	baseDir = filepath.Join(home, ".local", "share", "chadbot", "sandbox")
	binDir = filepath.Join(baseDir, "bin")
	runscDir = filepath.Join(baseDir, "runsc")
	dockerDir = filepath.Join(baseDir, "docker")
	runDir = filepath.Join(baseDir, "run")
	configDir = filepath.Join(baseDir, "config")
	dockerSocket = filepath.Join(runDir, "docker.sock")
}

func getArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func ensureDirectories() error {
	dirs := []string{binDir, runscDir, dockerDir, runDir, configDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[Sandbox] Shutting down...")
		stopDaemons()
		cancel()
	}()

	// Ensure directories exist
	if err := ensureDirectories(); err != nil {
		log.Fatalf("[Sandbox] Failed to create directories: %v", err)
	}

	// Initialize SDK client
	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}
	client = sdk.NewClient("sandbox", "1.0.0", "Isolated Docker container execution with gVisor security")
	client = client.WithSocket(socketPath)
	client = client.SetDocumentation(pluginDocumentation)

	// Register skills
	registerSkills()

	// Connect to chadbot backend
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("[Sandbox] Failed to connect to backend: %v", err)
	}
	defer client.Close()

	log.Println("[Sandbox] Plugin started")

	// Run the SDK client event loop
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[Sandbox] Client error: %v", err)
	}

	// Cleanup on exit
	stopDaemons()
}
