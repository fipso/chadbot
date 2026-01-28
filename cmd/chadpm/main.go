package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ANSI color codes
const (
	colorReset         = "\033[0m"
	colorBoldWhite     = "\033[1;37m"
	colorCyan          = "\033[36m"
	colorGreen         = "\033[32m"
	colorYellow        = "\033[33m"
	colorMagenta       = "\033[35m"
	colorBlue          = "\033[34m"
	colorRed           = "\033[31m"
	colorBrightGreen   = "\033[92m"
	colorBrightYellow  = "\033[93m"
	colorBrightMagenta = "\033[95m"
	colorBrightCyan    = "\033[96m"
)

var pluginColors = []string{
	colorGreen,
	colorYellow,
	colorMagenta,
	colorBlue,
	colorRed,
	colorBrightGreen,
	colorBrightYellow,
	colorBrightMagenta,
	colorBrightCyan,
}

// ColorWriter wraps output with colored prefixes
type ColorWriter struct {
	prefix string
	color  string
	mu     *sync.Mutex
}

func (cw *ColorWriter) Write(p []byte) (n int, err error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Printf("%s[%s]%s %s\n", cw.color, cw.prefix, colorReset, line)
	}
	return len(p), nil
}

// Process represents a managed process
type Process struct {
	Name     string
	Cmd      *exec.Cmd
	Color    string
	done     chan struct{}
	mu       sync.Mutex
}

func (p *Process) Wait() {
	<-p.done
}

// ProcessManager manages chadbot and plugin processes
type ProcessManager struct {
	chadbot     *Process
	plugins     map[string]*Process
	mu          sync.Mutex
	outputMu    sync.Mutex
	socketPath  string
	httpAddr    string
	binDir      string
	pluginsDir  string
	colorIndex  int
}

func NewProcessManager(socketPath, httpAddr, binDir, pluginsDir string) *ProcessManager {
	return &ProcessManager{
		plugins:    make(map[string]*Process),
		socketPath: socketPath,
		httpAddr:   httpAddr,
		binDir:     binDir,
		pluginsDir: pluginsDir,
	}
}

func (pm *ProcessManager) nextColor() string {
	color := pluginColors[pm.colorIndex%len(pluginColors)]
	pm.colorIndex++
	return color
}

func (pm *ProcessManager) logPM(format string, args ...interface{}) {
	pm.outputMu.Lock()
	defer pm.outputMu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[chadpm]%s %s\n", colorBoldWhite, colorReset, msg)
}

func (pm *ProcessManager) startProcess(name, binPath string, args []string, color string, env []string) (*Process, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), env...)

	writer := &ColorWriter{
		prefix: name,
		color:  color,
		mu:     &pm.outputMu,
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	proc := &Process{
		Name:  name,
		Cmd:   cmd,
		Color: color,
		done:  make(chan struct{}),
	}

	// Stream output
	go io.Copy(writer, stdout)
	go io.Copy(writer, stderr)

	// Wait for process to exit
	go func() {
		cmd.Wait()
		close(proc.done)
	}()

	return proc, nil
}

func (pm *ProcessManager) StartChadbot() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	binPath := filepath.Join(pm.binDir, "chadbot")
	args := []string{
		"-socket", pm.socketPath,
		"-http", pm.httpAddr,
	}

	proc, err := pm.startProcess("chadbot", binPath, args, colorCyan, nil)
	if err != nil {
		return err
	}

	pm.chadbot = proc

	// Wait for socket to be ready
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(pm.socketPath); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for socket %s", pm.socketPath)
}

func (pm *ProcessManager) StartPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	binPath := filepath.Join(pm.binDir, "plugins", name)
	env := []string{
		fmt.Sprintf("CHADBOT_SOCKET=%s", pm.socketPath),
	}

	proc, err := pm.startProcess(name, binPath, nil, pm.nextColor(), env)
	if err != nil {
		return err
	}

	pm.plugins[name] = proc
	return nil
}

func (pm *ProcessManager) StopPlugin(name string) error {
	pm.mu.Lock()
	proc, ok := pm.plugins[name]
	if !ok {
		pm.mu.Unlock()
		return nil
	}
	delete(pm.plugins, name)
	pm.mu.Unlock()

	return pm.stopProcess(proc)
}

func (pm *ProcessManager) stopProcess(proc *Process) error {
	if proc == nil || proc.Cmd == nil || proc.Cmd.Process == nil {
		return nil
	}

	// Send SIGTERM
	proc.Cmd.Process.Signal(syscall.SIGTERM)

	// Wait up to 5 seconds
	select {
	case <-proc.done:
		return nil
	case <-time.After(5 * time.Second):
		// Force kill
		proc.Cmd.Process.Kill()
		<-proc.done
		return nil
	}
}

func (pm *ProcessManager) StopAllPlugins() error {
	pm.mu.Lock()
	plugins := make([]*Process, 0, len(pm.plugins))
	for _, p := range pm.plugins {
		plugins = append(plugins, p)
	}
	pm.plugins = make(map[string]*Process)
	pm.mu.Unlock()

	var wg sync.WaitGroup
	for _, p := range plugins {
		wg.Add(1)
		go func(proc *Process) {
			defer wg.Done()
			pm.stopProcess(proc)
		}(p)
	}
	wg.Wait()
	return nil
}

func (pm *ProcessManager) StopChadbot() error {
	pm.mu.Lock()
	proc := pm.chadbot
	pm.chadbot = nil
	pm.mu.Unlock()

	if err := pm.stopProcess(proc); err != nil {
		return err
	}

	// Clean up socket file
	os.Remove(pm.socketPath)
	return nil
}

func (pm *ProcessManager) StopAll() error {
	if err := pm.StopAllPlugins(); err != nil {
		return err
	}
	return pm.StopChadbot()
}

// discoverPlugins finds all plugin directories containing main.go
func discoverPlugins(pluginsDir string) ([]string, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, err
	}

	var plugins []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mainPath := filepath.Join(pluginsDir, entry.Name(), "main.go")
		if _, err := os.Stat(mainPath); err == nil {
			plugins = append(plugins, entry.Name())
		}
	}
	return plugins, nil
}

// buildChadbot compiles the chadbot binary
func buildChadbot(binDir string) error {
	binPath := filepath.Join(binDir, "chadbot")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/chadbot")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildPlugin compiles a single plugin
func buildPlugin(name, binDir, pluginsDir string) error {
	binPath := filepath.Join(binDir, "plugins", name)
	// Clean the path and ensure it's a valid Go package path
	srcPath := filepath.Clean(filepath.Join(pluginsDir, name))
	if !strings.HasPrefix(srcPath, ".") {
		srcPath = "./" + srcPath
	}
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FileWatcher watches for file changes
type FileWatcher struct {
	watcher    *fsnotify.Watcher
	pm         *ProcessManager
	pluginsDir string
	debounce   map[string]time.Time
	debounceMu sync.Mutex
}

func NewFileWatcher(pm *ProcessManager, pluginsDir string) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FileWatcher{
		watcher:    w,
		pm:         pm,
		pluginsDir: pluginsDir,
		debounce:   make(map[string]time.Time),
	}, nil
}

func (fw *FileWatcher) shouldProcess(path string) bool {
	fw.debounceMu.Lock()
	defer fw.debounceMu.Unlock()

	now := time.Now()
	if last, ok := fw.debounce[path]; ok && now.Sub(last) < 500*time.Millisecond {
		return false
	}
	fw.debounce[path] = now
	return true
}

func (fw *FileWatcher) addDirRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden directories and common non-source directories
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return fw.watcher.Add(path)
		}
		return nil
	})
}

func (fw *FileWatcher) Start() error {
	// Watch core directories
	dirs := []string{"cmd/chadbot", "internal", "pkg"}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			if err := fw.addDirRecursive(dir); err != nil {
				return err
			}
		}
	}

	// Watch plugins directory
	if err := fw.addDirRecursive(fw.pluginsDir); err != nil {
		return err
	}

	go fw.watch()
	return nil
}

func (fw *FileWatcher) watch() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Only care about .go files
			if !strings.HasSuffix(event.Name, ".go") {
				continue
			}

			// Only care about write/create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			if !fw.shouldProcess(event.Name) {
				continue
			}

			fw.handleChange(event.Name)

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (fw *FileWatcher) handleChange(path string) {
	fw.pm.logPM("Change detected: %s", path)

	// Normalize paths for comparison
	cleanPath := filepath.Clean(path)
	cleanPluginsDir := filepath.Clean(fw.pluginsDir)

	// Check if it's a plugin change
	if strings.HasPrefix(cleanPath, cleanPluginsDir+string(filepath.Separator)) {
		relPath := strings.TrimPrefix(cleanPath, cleanPluginsDir+string(filepath.Separator))
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) > 0 {
			pluginName := parts[0]
			fw.restartPlugin(pluginName)
			return
		}
	}

	// Core change - restart everything
	fw.restartAll()
}

func (fw *FileWatcher) restartPlugin(name string) {
	fw.pm.logPM("Restarting plugin: %s", name)

	if err := fw.pm.StopPlugin(name); err != nil {
		fw.pm.logPM("Error stopping plugin %s: %v", name, err)
	}

	fw.pm.logPM("Building plugin: %s", name)
	if err := buildPlugin(name, fw.pm.binDir, fw.pluginsDir); err != nil {
		fw.pm.logPM("Error building plugin %s: %v", name, err)
		return
	}

	if err := fw.pm.StartPlugin(name); err != nil {
		fw.pm.logPM("Error starting plugin %s: %v", name, err)
	}
}

func (fw *FileWatcher) restartAll() {
	fw.pm.logPM("Core change detected, restarting all...")

	// Stop all plugins
	fw.pm.StopAllPlugins()

	// Stop chadbot
	fw.pm.StopChadbot()

	// Rebuild chadbot
	fw.pm.logPM("Building chadbot...")
	if err := buildChadbot(fw.pm.binDir); err != nil {
		fw.pm.logPM("Error building chadbot: %v", err)
		return
	}

	// Start chadbot
	fw.pm.logPM("Starting chadbot...")
	if err := fw.pm.StartChadbot(); err != nil {
		fw.pm.logPM("Error starting chadbot: %v", err)
		return
	}

	// Rebuild and start all plugins
	plugins, err := discoverPlugins(fw.pluginsDir)
	if err != nil {
		fw.pm.logPM("Error discovering plugins: %v", err)
		return
	}

	for _, plugin := range plugins {
		fw.pm.logPM("Building plugin: %s", plugin)
		if err := buildPlugin(plugin, fw.pm.binDir, fw.pluginsDir); err != nil {
			fw.pm.logPM("Error building plugin %s: %v", plugin, err)
			continue
		}

		if err := fw.pm.StartPlugin(plugin); err != nil {
			fw.pm.logPM("Error starting plugin %s: %v", plugin, err)
		}
	}
}

func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}

func main() {
	watchFlag := flag.Bool("watch", false, "Watch for file changes and auto-reload")
	flag.BoolVar(watchFlag, "w", false, "Watch for file changes and auto-reload (shorthand)")
	socketPath := flag.String("socket", "/tmp/chadbot.sock", "Socket path for chadbot")
	httpAddr := flag.String("http", ":8080", "HTTP address for chadbot")
	pluginsDir := flag.String("plugins", "./plugins", "Plugins directory")
	flag.Parse()

	binDir := "./bin"

	// Create bin directories
	os.MkdirAll(filepath.Join(binDir, "plugins"), 0755)

	pm := NewProcessManager(*socketPath, *httpAddr, binDir, *pluginsDir)

	// Discover plugins
	plugins, err := discoverPlugins(*pluginsDir)
	if err != nil {
		log.Fatalf("Failed to discover plugins: %v", err)
	}

	pm.logPM("Discovered plugins: %s", strings.Join(plugins, ", "))

	// Build chadbot
	pm.logPM("Building chadbot...")
	if err := buildChadbot(binDir); err != nil {
		log.Fatalf("Failed to build chadbot: %v", err)
	}

	// Build all plugins
	for _, plugin := range plugins {
		pm.logPM("Building plugin: %s", plugin)
		if err := buildPlugin(plugin, binDir, *pluginsDir); err != nil {
			log.Fatalf("Failed to build plugin %s: %v", plugin, err)
		}
	}

	// Start chadbot
	pm.logPM("Starting chadbot...")
	if err := pm.StartChadbot(); err != nil {
		log.Fatalf("Failed to start chadbot: %v", err)
	}

	// Start all plugins
	pm.logPM("Starting plugins...")
	for _, plugin := range plugins {
		if err := pm.StartPlugin(plugin); err != nil {
			pm.logPM("Failed to start plugin %s: %v", plugin, err)
		}
	}

	// Set up file watcher if enabled
	var fw *FileWatcher
	if *watchFlag {
		fw, err = NewFileWatcher(pm, *pluginsDir)
		if err != nil {
			log.Fatalf("Failed to create file watcher: %v", err)
		}
		if err := fw.Start(); err != nil {
			log.Fatalf("Failed to start file watcher: %v", err)
		}
		pm.logPM("Watching for changes... (Ctrl+C to stop)")
	} else {
		pm.logPM("Running... (Ctrl+C to stop)")
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	pm.logPM("Shutting down...")

	if fw != nil {
		fw.Close()
	}

	pm.StopAll()
	pm.logPM("Goodbye!")
}
