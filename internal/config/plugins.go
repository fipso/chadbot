package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// PluginConfigManager manages plugin configurations stored in config.toml
type PluginConfigManager struct {
	path          string
	mu            sync.RWMutex
	data          map[string]map[string]string // plugin -> key -> value
	watcher       *FileWatcher
	changeHandler func(pluginName, key, value string)
}

// PluginConfigChangeHandler is called when a plugin config changes
type PluginConfigChangeHandler func(pluginName, key, value string)

// NewPluginConfigManager creates a new plugin config manager
func NewPluginConfigManager() (*PluginConfigManager, error) {
	configDir, err := Dir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(configDir, "config.toml")
	m := &PluginConfigManager{
		path: path,
		data: make(map[string]map[string]string),
	}

	// Load existing config
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create file watcher
	watcher, err := NewFileWatcher(WatcherConfig{
		Handler: func() {
			oldData := m.cloneData()
			if err := m.load(); err != nil {
				log.Printf("[PluginConfig] Failed to reload: %v", err)
				return
			}
			m.notifyChanges(oldData)
		},
		Filter: func(name string) bool {
			return filepath.Base(name) == "config.toml"
		},
		LogPrefix: "PluginConfig",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	if err := watcher.Add(configDir); err != nil {
		return nil, fmt.Errorf("failed to watch config directory: %w", err)
	}

	m.watcher = watcher
	log.Printf("[PluginConfig] Watching file: %s", path)

	return m, nil
}

// OnChange sets the handler called when plugin configs change
func (m *PluginConfigManager) OnChange(handler PluginConfigChangeHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeHandler = handler
}

// Stop stops the file watcher
func (m *PluginConfigManager) Stop() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
}

func (m *PluginConfigManager) cloneData() map[string]map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	clone := make(map[string]map[string]string)
	for plugin, values := range m.data {
		clone[plugin] = make(map[string]string)
		for k, v := range values {
			clone[plugin][k] = v
		}
	}
	return clone
}

func (m *PluginConfigManager) notifyChanges(oldData map[string]map[string]string) {
	m.mu.RLock()
	handler := m.changeHandler
	newData := m.data
	m.mu.RUnlock()

	if handler == nil {
		return
	}

	// Find changed values
	for plugin, values := range newData {
		oldValues := oldData[plugin]
		for key, value := range values {
			if oldValues == nil || oldValues[key] != value {
				handler(plugin, key, value)
			}
		}
	}
}

// TOMLConfig represents the TOML config structure
type TOMLConfig struct {
	Plugins map[string]map[string]any `toml:"plugins"`
}

func (m *PluginConfigManager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	var cfg TOMLConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]map[string]string)
	for pluginName, values := range cfg.Plugins {
		m.data[pluginName] = make(map[string]string)
		for key, value := range values {
			m.data[pluginName][key] = convertValueFromTOML(value)
		}
	}

	return nil
}

func (m *PluginConfigManager) save() error {
	m.mu.RLock()
	cfg := TOMLConfig{
		Plugins: make(map[string]map[string]any),
	}
	for pluginName, values := range m.data {
		cfg.Plugins[pluginName] = make(map[string]any)
		for key, value := range values {
			cfg.Plugins[pluginName][key] = convertValueForTOML(value)
		}
	}
	m.mu.RUnlock()

	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0644)
}

// GetPluginConfig gets a single config value for a plugin
func (m *PluginConfigManager) GetPluginConfig(pluginName, key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if values, ok := m.data[pluginName]; ok {
		return values[key]
	}
	return ""
}

// GetPluginConfigs gets all config values for a plugin
func (m *PluginConfigManager) GetPluginConfigs(pluginName string) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string)
	if values, ok := m.data[pluginName]; ok {
		for k, v := range values {
			result[k] = v
		}
	}
	return result
}

// SetPluginConfig sets a config value for a plugin
func (m *PluginConfigManager) SetPluginConfig(pluginName, key, value string) error {
	m.mu.Lock()
	if m.data[pluginName] == nil {
		m.data[pluginName] = make(map[string]string)
	}
	m.data[pluginName][key] = value
	m.mu.Unlock()

	return m.save()
}

// GetAllPluginConfigs returns all plugin configs
func (m *PluginConfigManager) GetAllPluginConfigs() map[string]map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]map[string]string)
	for plugin, values := range m.data {
		result[plugin] = make(map[string]string)
		for k, v := range values {
			result[plugin][k] = v
		}
	}
	return result
}

// SetPluginConfigs batch sets config values for a plugin
func (m *PluginConfigManager) SetPluginConfigs(pluginName string, values map[string]string) error {
	m.mu.Lock()
	if m.data[pluginName] == nil {
		m.data[pluginName] = make(map[string]string)
	}
	for k, v := range values {
		m.data[pluginName][k] = v
	}
	m.mu.Unlock()

	return m.save()
}

// ExportToTOML exports all plugin configs to TOML format
func (m *PluginConfigManager) ExportToTOML() ([]byte, error) {
	m.mu.RLock()
	cfg := TOMLConfig{
		Plugins: make(map[string]map[string]any),
	}
	for pluginName, values := range m.data {
		cfg.Plugins[pluginName] = make(map[string]any)
		for key, value := range values {
			cfg.Plugins[pluginName][key] = convertValueForTOML(value)
		}
	}
	m.mu.RUnlock()

	return toml.Marshal(cfg)
}

// ImportFromTOML imports plugin configs from TOML data
func (m *PluginConfigManager) ImportFromTOML(data []byte) error {
	var cfg TOMLConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	m.mu.Lock()
	for pluginName, values := range cfg.Plugins {
		if m.data[pluginName] == nil {
			m.data[pluginName] = make(map[string]string)
		}
		for key, value := range values {
			m.data[pluginName][key] = convertValueFromTOML(value)
		}
	}
	m.mu.Unlock()

	return m.save()
}

// convertValueForTOML converts a stored string value to appropriate TOML type
func convertValueForTOML(value string) any {
	// Try to parse as JSON array
	var arr []string
	if err := json.Unmarshal([]byte(value), &arr); err == nil {
		return arr
	}

	// Check for boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Return as string
	return value
}

// convertValueFromTOML converts a TOML value to stored string format
func convertValueFromTOML(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		// Convert to JSON string array
		strArr := make([]string, len(v))
		for i, item := range v {
			if s, ok := item.(string); ok {
				strArr[i] = s
			}
		}
		data, _ := json.Marshal(strArr)
		return string(data)
	case string:
		return v
	default:
		// For other types (numbers, etc.), convert to string
		data, _ := json.Marshal(v)
		return string(data)
	}
}
