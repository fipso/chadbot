package config

import (
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches files/directories and calls a handler on changes with debouncing
type FileWatcher struct {
	watcher       *fsnotify.Watcher
	handler       func()
	filter        func(name string) bool // Returns true if file should trigger handler
	debounceDelay time.Duration
	debounceTimer *time.Timer
	debounceMu    sync.Mutex
	stopChan      chan struct{}
	prefix        string // Log prefix
}

// WatcherConfig configures the file watcher
type WatcherConfig struct {
	Handler       func()
	Filter        func(name string) bool
	DebounceDelay time.Duration
	LogPrefix     string
}

// NewFileWatcher creates a new file watcher
func NewFileWatcher(cfg WatcherConfig) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if cfg.DebounceDelay == 0 {
		cfg.DebounceDelay = 100 * time.Millisecond
	}
	if cfg.LogPrefix == "" {
		cfg.LogPrefix = "FileWatcher"
	}

	fw := &FileWatcher{
		watcher:       watcher,
		handler:       cfg.Handler,
		filter:        cfg.Filter,
		debounceDelay: cfg.DebounceDelay,
		stopChan:      make(chan struct{}),
		prefix:        cfg.LogPrefix,
	}

	go fw.watchLoop()
	return fw, nil
}

// Add adds a path to watch
func (fw *FileWatcher) Add(path string) error {
	return fw.watcher.Add(path)
}

// Stop stops the watcher
func (fw *FileWatcher) Stop() {
	close(fw.stopChan)
	fw.watcher.Close()
}

func (fw *FileWatcher) watchLoop() {
	for {
		select {
		case <-fw.stopChan:
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			// Check filter
			if fw.filter != nil && !fw.filter(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				// Debounce: reset timer on each event
				fw.debounceMu.Lock()
				if fw.debounceTimer != nil {
					fw.debounceTimer.Stop()
				}
				fw.debounceTimer = time.AfterFunc(fw.debounceDelay, func() {
					log.Printf("[%s] Files changed, reloading...", fw.prefix)
					if fw.handler != nil {
						fw.handler()
					}
				})
				fw.debounceMu.Unlock()
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[%s] Watcher error: %v", fw.prefix, err)
		}
	}
}
