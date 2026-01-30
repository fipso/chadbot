package souls

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fipso/chadbot/internal/config"
)

// Soul represents a system prompt profile
type Soul struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ChangeHandler is called when souls change
type ChangeHandler func()

// Manager handles soul files in the souls/ directory
type Manager struct {
	dir           string
	mu            sync.RWMutex
	watcher       *config.FileWatcher
	changeHandler ChangeHandler
}

// NewManager creates a new souls manager
func NewManager() (*Manager, error) {
	dir, err := config.SoulsDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get souls directory: %w", err)
	}

	m := &Manager{dir: dir}

	// Create default soul if none exist
	souls, _ := m.List()
	if len(souls) == 0 {
		defaultSoul := &Soul{
			Name: "default",
			Content: `You are a helpful AI assistant. You have access to tools/skills provided by plugins.
IMPORTANT: Only use the tools that are explicitly provided to you. Do not make up or hallucinate tools that don't exist.
If asked what tools you have, list ONLY the ones provided in the current conversation - nothing else.

When using tools to extract or gather data, be efficient with token usage:
- Extract only the specific information needed, not entire pages or datasets
- Summarize large results before requesting more data
- Avoid redundant tool calls - plan your approach before executing
- If a tool returns a large response, focus on the relevant parts in your answer`,
		}
		m.Save(defaultSoul)
	}

	// Create file watcher
	watcher, err := config.NewFileWatcher(config.WatcherConfig{
		Handler: func() {
			m.mu.RLock()
			handler := m.changeHandler
			m.mu.RUnlock()
			if handler != nil {
				handler()
			}
		},
		Filter: func(name string) bool {
			return strings.HasSuffix(name, ".md")
		},
		LogPrefix: "Souls",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	if err := watcher.Add(dir); err != nil {
		return nil, fmt.Errorf("failed to watch souls directory: %w", err)
	}

	m.watcher = watcher
	log.Printf("[Souls] Watching directory: %s", dir)

	return m, nil
}

// OnChange sets the handler called when souls change
func (m *Manager) OnChange(handler ChangeHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeHandler = handler
}

// Stop stops the file watcher
func (m *Manager) Stop() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
}

// List returns all available souls
func (m *Manager) List() ([]*Soul, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}

	var souls []*Soul
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		content, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}

		souls = append(souls, &Soul{
			Name:    name,
			Content: string(content),
		})
	}

	return souls, nil
}

// Get returns a soul by name
func (m *Manager) Get(name string) (*Soul, error) {
	path := filepath.Join(m.dir, name+".md")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("soul not found: %s", name)
	}

	return &Soul{
		Name:    name,
		Content: string(content),
	}, nil
}

// GetSystemPrompt returns the system prompt for a soul by name
// Falls back to default prompt if soul not found
func (m *Manager) GetSystemPrompt(name string) string {
	if name == "" {
		name = "default"
	}
	soul, err := m.Get(name)
	if err != nil {
		// Fallback to default prompt
		return `You are a helpful AI assistant.`
	}
	return soul.Content
}

// Save creates or updates a soul
func (m *Manager) Save(soul *Soul) error {
	// Sanitize name - only allow alphanumeric, dash, underscore
	name := sanitizeName(soul.Name)
	if name == "" {
		return fmt.Errorf("invalid soul name")
	}

	path := filepath.Join(m.dir, name+".md")
	return os.WriteFile(path, []byte(soul.Content), 0644)
}

// Delete removes a soul
func (m *Manager) Delete(name string) error {
	if name == "" {
		return fmt.Errorf("invalid soul name")
	}

	// Don't allow deleting the default soul
	if name == "default" {
		return fmt.Errorf("cannot delete the default soul")
	}

	path := filepath.Join(m.dir, name+".md")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete soul: %w", err)
	}

	return nil
}

func sanitizeName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	return result.String()
}
