package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// Docker static binary version
	dockerVersion = "27.4.1"

	// gVisor uses "latest" release channel
	gvisorRelease = "latest"
)

// getDockerURL returns the download URL for Docker static binaries
func getDockerURL() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	} else if arch == "arm64" {
		arch = "aarch64"
	}
	return fmt.Sprintf("https://download.docker.com/linux/static/stable/%s/docker-%s.tgz", arch, dockerVersion)
}

// getDockerRootlessURL returns the download URL for Docker rootless extras
func getDockerRootlessURL() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	} else if arch == "arm64" {
		arch = "aarch64"
	}
	return fmt.Sprintf("https://download.docker.com/linux/static/stable/%s/docker-rootless-extras-%s.tgz", arch, dockerVersion)
}

// getGVisorURLs returns download URLs for gVisor components
func getGVisorURLs() (runscURL, shimURL string) {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	} else if arch == "arm64" {
		arch = "aarch64"
	}
	baseURL := fmt.Sprintf("https://storage.googleapis.com/gvisor/releases/release/%s/%s", gvisorRelease, arch)
	return baseURL + "/runsc", baseURL + "/containerd-shim-runsc-v1"
}

// ensureBinaries downloads and installs all required binaries if not present
func ensureBinaries() error {
	// Check if Docker binaries exist
	dockerdPath := filepath.Join(binDir, "dockerd")
	if _, err := os.Stat(dockerdPath); os.IsNotExist(err) {
		log.Println("[Sandbox] Downloading Docker binaries...")
		if err := downloadDocker(); err != nil {
			return fmt.Errorf("failed to download Docker: %w", err)
		}
	}

	// Check if rootless extras exist
	rootlesskitPath := filepath.Join(binDir, "rootlesskit")
	if _, err := os.Stat(rootlesskitPath); os.IsNotExist(err) {
		log.Println("[Sandbox] Downloading Docker rootless extras...")
		if err := downloadDockerRootless(); err != nil {
			return fmt.Errorf("failed to download Docker rootless extras: %w", err)
		}
	}

	// Check if gVisor runsc exists
	runscPath := filepath.Join(runscDir, "runsc")
	if _, err := os.Stat(runscPath); os.IsNotExist(err) {
		log.Println("[Sandbox] Downloading gVisor runsc...")
		if err := downloadGVisor(); err != nil {
			return fmt.Errorf("failed to download gVisor: %w", err)
		}
	}

	return nil
}

// downloadDocker downloads and extracts Docker static binaries
func downloadDocker() error {
	url := getDockerURL()
	log.Printf("[Sandbox] Downloading Docker from %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download Docker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download Docker: HTTP %d", resp.StatusCode)
	}

	// Create gzip reader
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Only extract regular files from docker/ directory
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Strip "docker/" prefix
		name := strings.TrimPrefix(header.Name, "docker/")
		if name == header.Name {
			continue // Not in docker/ directory
		}

		destPath := filepath.Join(binDir, name)

		// Create file
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", destPath, err)
		}

		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return fmt.Errorf("failed to extract %s: %w", name, err)
		}
		f.Close()

		log.Printf("[Sandbox] Extracted %s", name)
	}

	return nil
}

// downloadDockerRootless downloads and extracts Docker rootless extras
func downloadDockerRootless() error {
	url := getDockerRootlessURL()
	log.Printf("[Sandbox] Downloading Docker rootless extras from %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download Docker rootless extras: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download Docker rootless extras: HTTP %d", resp.StatusCode)
	}

	// Create gzip reader
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Only extract regular files from docker-rootless-extras/ directory
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Strip "docker-rootless-extras/" prefix
		name := strings.TrimPrefix(header.Name, "docker-rootless-extras/")
		if name == header.Name {
			continue // Not in docker-rootless-extras/ directory
		}

		destPath := filepath.Join(binDir, name)

		// Create file
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", destPath, err)
		}

		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return fmt.Errorf("failed to extract %s: %w", name, err)
		}
		f.Close()

		log.Printf("[Sandbox] Extracted %s", name)
	}

	return nil
}

// downloadGVisor downloads gVisor runsc and containerd shim
func downloadGVisor() error {
	runscURL, shimURL := getGVisorURLs()

	// Download runsc
	runscPath := filepath.Join(runscDir, "runsc")
	if err := downloadFile(runscURL, runscPath); err != nil {
		return fmt.Errorf("failed to download runsc: %w", err)
	}
	log.Printf("[Sandbox] Downloaded runsc")

	// Download containerd-shim-runsc-v1
	shimPath := filepath.Join(runscDir, "containerd-shim-runsc-v1")
	if err := downloadFile(shimURL, shimPath); err != nil {
		return fmt.Errorf("failed to download containerd shim: %w", err)
	}
	log.Printf("[Sandbox] Downloaded containerd-shim-runsc-v1")

	return nil
}

// downloadFile downloads a file from URL to destination path
func downloadFile(url, destPath string) error {
	log.Printf("[Sandbox] Downloading %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// getBinaryPath returns the full path to a binary
func getBinaryPath(name string) string {
	return filepath.Join(binDir, name)
}

// getRunscPath returns the full path to runsc
func getRunscPath() string {
	return filepath.Join(runscDir, "runsc")
}

// getShimPath returns the full path to containerd-shim-runsc-v1
func getShimPath() string {
	return filepath.Join(runscDir, "containerd-shim-runsc-v1")
}
