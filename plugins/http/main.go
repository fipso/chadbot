package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
)

//go:embed PLUGIN.md
var pluginDocumentation string

var (
	client          *sdk.Client
	httpClient      *http.Client
	allowedPatterns []*regexp.Regexp
	patternsMu      sync.RWMutex
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[HTTP] Shutting down...")
		cancel()
	}()

	// Initialize HTTP client
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	// Initialize SDK client
	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}
	client = sdk.NewClient("http", "1.0.0", "HTTP request plugin - make HTTP requests to allowed URLs")
	client = client.WithSocket(socketPath)
	client = client.SetDocumentation(pluginDocumentation)

	// Register skills
	registerSkills()

	// Connect to chadbot backend
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("[HTTP] Failed to connect to backend: %v", err)
	}
	defer client.Close()

	// Register plugin config
	if err := registerConfig(); err != nil {
		log.Printf("[HTTP] Failed to register config: %v", err)
	}

	// Handle config changes
	client.OnConfigChanged(handleConfigChanged)

	// Load initial config values
	loadPatterns()
	loadTimeout()

	log.Println("[HTTP] Plugin started")

	// Run the SDK client event loop
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[HTTP] Client error: %v", err)
	}
}

func registerConfig() error {
	return client.RegisterConfig([]sdk.ConfigField{
		{
			Key:          "allowed_patterns",
			Label:        "Allowed URL Patterns",
			Description:  "Regex patterns for allowed URLs (e.g., ^https://api\\.example\\.com/.*)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING_ARRAY,
			DefaultValue: "[]",
		},
		{
			Key:          "timeout_seconds",
			Label:        "Request Timeout",
			Description:  "HTTP request timeout in seconds (default: 30)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER,
			DefaultValue: "30",
		},
		{
			Key:          "max_response_size",
			Label:        "Max Response Size",
			Description:  "Maximum response body size in bytes (default: 1048576 = 1MB)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER,
			DefaultValue: "1048576",
		},
	})
}

func registerSkills() {
	client.RegisterSkill(&pb.Skill{
		Name:        "http_request",
		Description: "Make an HTTP request to an allowed URL. The URL must match one of the configured allowed patterns.",
		Parameters: []*pb.SkillParameter{
			{Name: "url", Type: "string", Description: "The URL to request (must match allowed patterns)", Required: true},
			{Name: "method", Type: "string", Description: "HTTP method: GET, POST, PUT, PATCH, DELETE, HEAD (default: GET)", Required: false},
			{Name: "body", Type: "string", Description: "Request body (for POST, PUT, PATCH)", Required: false},
			{Name: "headers", Type: "string", Description: "JSON object of headers, e.g., {\"Content-Type\": \"application/json\"}", Required: false},
		},
	}, handleRequest)

	client.RegisterSkill(&pb.Skill{
		Name:        "http_list_patterns",
		Description: "List the currently configured allowed URL patterns",
		Parameters:  []*pb.SkillParameter{},
	}, handleListPatterns)
}

func handleConfigChanged(key, value string, allValues map[string]string) {
	log.Printf("[HTTP] Config changed: %s", key)

	if key == "allowed_patterns" {
		loadPatterns()
	} else if key == "timeout_seconds" {
		timeout := 30
		if value != "" {
			fmt.Sscanf(value, "%d", &timeout)
		}
		httpClient.Timeout = time.Duration(timeout) * time.Second
		log.Printf("[HTTP] Updated timeout to %d seconds", timeout)
	}
}

func loadTimeout() {
	timeout := 30
	if value := client.GetConfig("timeout_seconds"); value != "" {
		fmt.Sscanf(value, "%d", &timeout)
	}
	httpClient.Timeout = time.Duration(timeout) * time.Second
	log.Printf("[HTTP] Timeout set to %d seconds", timeout)
}

func loadPatterns() {
	patterns := client.GetConfigStringArray("allowed_patterns")

	patternsMu.Lock()
	defer patternsMu.Unlock()

	allowedPatterns = nil
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Printf("[HTTP] Invalid regex pattern '%s': %v", pattern, err)
			continue
		}
		allowedPatterns = append(allowedPatterns, re)
		log.Printf("[HTTP] Loaded pattern: %s", pattern)
	}

	log.Printf("[HTTP] Loaded %d URL patterns", len(allowedPatterns))
}

func isURLAllowed(url string) bool {
	patternsMu.RLock()
	defer patternsMu.RUnlock()

	if len(allowedPatterns) == 0 {
		return false
	}

	for _, re := range allowedPatterns {
		if re.MatchString(url) {
			return true
		}
	}
	return false
}

func handleRequest(ctx context.Context, args map[string]string) (string, error) {
	url := args["url"]
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	// Check if URL is allowed
	if !isURLAllowed(url) {
		patternsMu.RLock()
		patternCount := len(allowedPatterns)
		patternsMu.RUnlock()

		if patternCount == 0 {
			return "", fmt.Errorf("no URL patterns configured - configure allowed_patterns in plugin settings")
		}
		return "", fmt.Errorf("URL not allowed: %s does not match any configured patterns", url)
	}

	// Parse method
	method := strings.ToUpper(args["method"])
	if method == "" {
		method = "GET"
	}
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true,
		"PATCH": true, "DELETE": true, "HEAD": true,
	}
	if !validMethods[method] {
		return "", fmt.Errorf("invalid HTTP method: %s", method)
	}

	// Create request
	var bodyReader io.Reader
	if body := args["body"]; body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Parse and set headers
	if headersStr := args["headers"]; headersStr != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(headersStr), &headers); err != nil {
			return "", fmt.Errorf("invalid headers JSON: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	// Make request
	log.Printf("[HTTP] %s %s", method, url)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Get max response size
	maxSize := int64(1048576) // 1MB default
	if maxSizeStr := client.GetConfig("max_response_size"); maxSizeStr != "" {
		fmt.Sscanf(maxSizeStr, "%d", &maxSize)
	}

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, maxSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	truncated := false
	if int64(len(body)) > maxSize {
		body = body[:maxSize]
		truncated = true
	}

	// Build response
	result := map[string]interface{}{
		"status":      resp.Status,
		"status_code": resp.StatusCode,
		"headers":     resp.Header,
		"body":        string(body),
	}
	if truncated {
		result["truncated"] = true
		result["note"] = fmt.Sprintf("Response truncated to %d bytes", maxSize)
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleListPatterns(ctx context.Context, args map[string]string) (string, error) {
	patternsMu.RLock()
	defer patternsMu.RUnlock()

	patterns := make([]string, 0, len(allowedPatterns))
	for _, re := range allowedPatterns {
		patterns = append(patterns, re.String())
	}

	result := map[string]interface{}{
		"pattern_count": len(patterns),
		"patterns":      patterns,
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}
