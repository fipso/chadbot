package config

import (
	"os"
	"path/filepath"
)

// Dir returns the chadbot config directory (~/.config/chadbot)
// Creates it if it doesn't exist
func Dir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".config", "chadbot")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}

	return configDir, nil
}

// SoulsDir returns the souls directory (~/.config/chadbot/souls)
// Creates it if it doesn't exist
func SoulsDir() (string, error) {
	configDir, err := Dir()
	if err != nil {
		return "", err
	}

	soulsDir := filepath.Join(configDir, "souls")
	if err := os.MkdirAll(soulsDir, 0755); err != nil {
		return "", err
	}

	return soulsDir, nil
}
