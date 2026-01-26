package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
)

const serversTable = "servers"

var (
	client  *sdk.Client
	storage *sdk.StorageClient
	manager *ServerManager
)

// MCPServerConfig stores MCP server configuration
type MCPServerConfig struct {
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	WorkingDir string            `json:"working_dir"`
	Env        map[string]string `json:"env"`
	AutoStart  bool              `json:"auto_start"`
	CreatedAt  int64             `json:"created_at"`
}

// MCPServerState tracks runtime state of a connected server
type MCPServerState struct {
	Name        string
	Config      *MCPServerConfig
	Session     *mcp.ClientSession
	Cmd         *exec.Cmd
	Connected   bool
	ConnectedAt time.Time
	Tools       []*mcp.Tool
	mu          sync.RWMutex
}

// ServerManager manages MCP server connections
type ServerManager struct {
	servers map[string]*MCPServerState
	mu      sync.RWMutex
}

func NewServerManager() *ServerManager {
	return &ServerManager{
		servers: make(map[string]*MCPServerState),
	}
}

func (m *ServerManager) GetServer(name string) *MCPServerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers[name]
}

func (m *ServerManager) SetServer(name string, state *MCPServerState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[name] = state
}

func (m *ServerManager) RemoveServer(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.servers, name)
}

func (m *ServerManager) AllServers() []*MCPServerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	servers := make([]*MCPServerState, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	return servers
}

func (m *ServerManager) Connect(ctx context.Context, config *MCPServerConfig) error {
	// Check if already connected
	if state := m.GetServer(config.Name); state != nil && state.Connected {
		return fmt.Errorf("server %s is already connected", config.Name)
	}

	// Build command
	cmd := exec.Command(config.Command, config.Args...)
	if config.WorkingDir != "" {
		cmd.Dir = config.WorkingDir
	}
	if len(config.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Create MCP client
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "chadbot-mcp",
		Version: "1.0.0",
	}, nil)

	// Create transport with command
	transport := &mcp.CommandTransport{Command: cmd}

	// Get timeout from config
	timeout := 30 * time.Second
	if t := client.GetConfig("connection_timeout"); t != "" {
		var secs int
		if _, err := fmt.Sscanf(t, "%d", &secs); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session, err := mcpClient.Connect(connectCtx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// List available tools
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Create server state
	state := &MCPServerState{
		Name:        config.Name,
		Config:      config,
		Session:     session,
		Cmd:         cmd,
		Connected:   true,
		ConnectedAt: time.Now(),
		Tools:       toolsResult.Tools,
	}
	m.SetServer(config.Name, state)

	log.Printf("[MCP] Connected to server %s with %d tools", config.Name, len(toolsResult.Tools))

	// Emit connected event
	emitServerEvent("mcp.server.connected", config.Name, map[string]interface{}{
		"server":      config.Name,
		"tools_count": len(toolsResult.Tools),
	})

	return nil
}

func (m *ServerManager) Disconnect(name string, reason string) error {
	state := m.GetServer(name)
	if state == nil {
		return fmt.Errorf("server %s not found", name)
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if !state.Connected {
		return fmt.Errorf("server %s is not connected", name)
	}

	// Close session
	if state.Session != nil {
		state.Session.Close()
	}

	state.Connected = false
	state.Session = nil
	state.Tools = nil

	log.Printf("[MCP] Disconnected from server %s: %s", name, reason)

	// Emit disconnected event
	emitServerEvent("mcp.server.disconnected", name, map[string]interface{}{
		"server": name,
		"reason": reason,
	})

	return nil
}

func (m *ServerManager) DisconnectAll() {
	for _, state := range m.AllServers() {
		if state.Connected {
			m.Disconnect(state.Name, "plugin shutdown")
		}
	}
}

func emitServerEvent(eventType, server string, data map[string]interface{}) {
	payload, err := structpb.NewStruct(data)
	if err != nil {
		log.Printf("[MCP] Failed to create event payload: %v", err)
		return
	}

	err = client.Emit(&pb.Event{
		EventType:    eventType,
		SourcePlugin: "mcp",
		Timestamp:    timestamppb.Now(),
		Data: &pb.Event_Generic{
			Generic: &pb.GenericEvent{
				EventName: eventType,
				Payload:   payload,
			},
		},
	})
	if err != nil {
		log.Printf("[MCP] Failed to emit event: %v", err)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[MCP] Shutting down...")
		cancel()
	}()

	// Initialize SDK client
	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}
	client = sdk.NewClient("mcp", "1.0.0", "MCP (Model Context Protocol) server integration - spawn and invoke MCP servers")
	client = client.WithSocket(socketPath)

	// Initialize server manager
	manager = NewServerManager()

	// Register skills
	registerSkills()

	// Connect to chadbot backend
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("[MCP] Failed to connect to backend: %v", err)
	}
	defer client.Close()

	// Register plugin config
	if err := registerConfig(); err != nil {
		log.Printf("[MCP] Failed to register config: %v", err)
	}

	// Initialize storage
	storage = client.Storage()
	if err := initStorage(); err != nil {
		log.Fatalf("[MCP] Failed to initialize storage: %v", err)
	}

	// Auto-start servers
	autoStartServers(ctx)

	log.Println("[MCP] Plugin started")

	// Run the SDK client event loop
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[MCP] Client error: %v", err)
	}

	// Cleanup - disconnect all servers
	manager.DisconnectAll()
}

func registerConfig() error {
	return client.RegisterConfig([]sdk.ConfigField{
		{
			Key:          "connection_timeout",
			Label:        "Connection Timeout",
			Description:  "Timeout for connecting to MCP servers (seconds)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER,
			DefaultValue: "30",
		},
		{
			Key:          "tool_call_timeout",
			Label:        "Tool Call Timeout",
			Description:  "Timeout for tool invocations (seconds)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_NUMBER,
			DefaultValue: "60",
		},
	})
}

func registerSkills() {
	// Add a server
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_add_server",
		Description: "Register a new MCP server configuration",
		Parameters: []*pb.SkillParameter{
			{Name: "name", Type: "string", Description: "Unique identifier for the server", Required: true},
			{Name: "command", Type: "string", Description: "Command to run (e.g., npx, uvx, ./server)", Required: true},
			{Name: "args", Type: "string", Description: "JSON array of command arguments", Required: false},
			{Name: "working_dir", Type: "string", Description: "Working directory for the server", Required: false},
			{Name: "env", Type: "string", Description: "JSON object of environment variables", Required: false},
			{Name: "auto_start", Type: "boolean", Description: "Auto-connect when plugin starts", Required: false},
		},
	}, handleAddServer)

	// Remove a server
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_remove_server",
		Description: "Remove an MCP server configuration and disconnect if connected",
		Parameters: []*pb.SkillParameter{
			{Name: "name", Type: "string", Description: "Server name to remove", Required: true},
		},
	}, handleRemoveServer)

	// Connect to a server
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_connect",
		Description: "Connect to a registered MCP server (spawn subprocess)",
		Parameters: []*pb.SkillParameter{
			{Name: "name", Type: "string", Description: "Server name to connect to", Required: true},
		},
	}, handleConnect)

	// Disconnect from a server
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_disconnect",
		Description: "Disconnect from an MCP server (terminate subprocess)",
		Parameters: []*pb.SkillParameter{
			{Name: "name", Type: "string", Description: "Server name to disconnect from", Required: true},
		},
	}, handleDisconnect)

	// List servers
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_list_servers",
		Description: "List all configured MCP servers with connection status",
		Parameters:  []*pb.SkillParameter{},
	}, handleListServers)

	// List tools
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_list_tools",
		Description: "List available tools from connected MCP servers",
		Parameters: []*pb.SkillParameter{
			{Name: "server", Type: "string", Description: "Filter by server name (optional)", Required: false},
		},
	}, handleListTools)

	// Call a tool
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_call_tool",
		Description: "Call a tool on an MCP server",
		Parameters: []*pb.SkillParameter{
			{Name: "server", Type: "string", Description: "Server name", Required: true},
			{Name: "tool", Type: "string", Description: "Tool name to call", Required: true},
			{Name: "arguments", Type: "string", Description: "JSON object of tool arguments", Required: false},
		},
	}, handleCallTool)

	// Get status
	client.RegisterSkill(&pb.Skill{
		Name:        "mcp_status",
		Description: "Get overall MCP plugin status",
		Parameters:  []*pb.SkillParameter{},
	}, handleStatus)
}

func initStorage() error {
	log.Println("[MCP] Initializing storage...")

	// Create servers table
	if err := storage.CreateTable(serversTable, []*pb.ColumnDef{
		{Name: "name", Type: "TEXT", PrimaryKey: true},
		{Name: "command", Type: "TEXT", NotNull: true},
		{Name: "args", Type: "TEXT", NotNull: true},
		{Name: "working_dir", Type: "TEXT"},
		{Name: "env", Type: "TEXT"},
		{Name: "auto_start", Type: "INTEGER"},
		{Name: "created_at", Type: "INTEGER", NotNull: true},
	}, true); err != nil {
		return err
	}

	return nil
}

func autoStartServers(ctx context.Context) {
	rows, err := storage.Query(serversTable, nil, "auto_start = ?", []string{"1"}, "", 100, 0)
	if err != nil {
		log.Printf("[MCP] Failed to query auto-start servers: %v", err)
		return
	}

	log.Printf("[MCP] Found %d servers to auto-start", len(rows))

	for _, row := range rows {
		config, err := rowToConfig(row)
		if err != nil {
			log.Printf("[MCP] Failed to parse server config: %v", err)
			continue
		}

		log.Printf("[MCP] Auto-starting server: %s", config.Name)
		if err := manager.Connect(ctx, config); err != nil {
			log.Printf("[MCP] Failed to auto-start server %s: %v", config.Name, err)
		}
	}
}

func rowToConfig(row *pb.Row) (*MCPServerConfig, error) {
	config := &MCPServerConfig{
		Name:       row.Values["name"],
		Command:    row.Values["command"],
		WorkingDir: row.Values["working_dir"],
		AutoStart:  row.Values["auto_start"] == "1",
	}

	// Parse args JSON
	if argsStr := row.Values["args"]; argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &config.Args); err != nil {
			return nil, fmt.Errorf("failed to parse args: %w", err)
		}
	}

	// Parse env JSON
	if envStr := row.Values["env"]; envStr != "" {
		if err := json.Unmarshal([]byte(envStr), &config.Env); err != nil {
			return nil, fmt.Errorf("failed to parse env: %w", err)
		}
	}

	// Parse created_at
	fmt.Sscanf(row.Values["created_at"], "%d", &config.CreatedAt)

	return config, nil
}

func loadServerConfig(name string) (*MCPServerConfig, error) {
	rows, err := storage.Query(serversTable, nil, "name = ?", []string{name}, "", 1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("server %s not found", name)
	}
	return rowToConfig(rows[0])
}

// Skill handlers

func handleAddServer(ctx context.Context, args map[string]string) (string, error) {
	name := args["name"]
	command := args["command"]
	if name == "" || command == "" {
		return "", fmt.Errorf("name and command are required")
	}

	// Parse args
	var argsSlice []string
	if argsJSON := args["args"]; argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &argsSlice); err != nil {
			return "", fmt.Errorf("invalid args JSON: %w", err)
		}
	}
	argsJSON, _ := json.Marshal(argsSlice)

	// Parse env
	var envMap map[string]string
	if envJSON := args["env"]; envJSON != "" {
		if err := json.Unmarshal([]byte(envJSON), &envMap); err != nil {
			return "", fmt.Errorf("invalid env JSON: %w", err)
		}
	}
	envJSON, _ := json.Marshal(envMap)

	autoStart := "0"
	if args["auto_start"] == "true" {
		autoStart = "1"
	}

	// Insert or update server config
	err := storage.Insert(serversTable, map[string]string{
		"name":        name,
		"command":     command,
		"args":        string(argsJSON),
		"working_dir": args["working_dir"],
		"env":         string(envJSON),
		"auto_start":  autoStart,
		"created_at":  fmt.Sprintf("%d", time.Now().Unix()),
	})
	if err != nil {
		// Try update if insert fails (already exists)
		updateErr := storage.Update(serversTable, map[string]string{
			"command":     command,
			"args":        string(argsJSON),
			"working_dir": args["working_dir"],
			"env":         string(envJSON),
			"auto_start":  autoStart,
		}, "name = ?", name)
		if updateErr != nil {
			return "", fmt.Errorf("failed to save server: insert=%v, update=%v", err, updateErr)
		}
	}

	return fmt.Sprintf("Server %s added successfully", name), nil
}

func handleRemoveServer(ctx context.Context, args map[string]string) (string, error) {
	name := args["name"]
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	// Disconnect if connected
	if state := manager.GetServer(name); state != nil && state.Connected {
		manager.Disconnect(name, "server removed")
	}
	manager.RemoveServer(name)

	// Delete from storage
	if err := storage.Delete(serversTable, "name = ?", name); err != nil {
		return "", fmt.Errorf("failed to delete server: %w", err)
	}

	return fmt.Sprintf("Server %s removed successfully", name), nil
}

func handleConnect(ctx context.Context, args map[string]string) (string, error) {
	name := args["name"]
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	// Load config from storage
	config, err := loadServerConfig(name)
	if err != nil {
		return "", err
	}

	// Connect
	if err := manager.Connect(ctx, config); err != nil {
		return "", err
	}

	state := manager.GetServer(name)
	return fmt.Sprintf("Connected to %s with %d tools available", name, len(state.Tools)), nil
}

func handleDisconnect(ctx context.Context, args map[string]string) (string, error) {
	name := args["name"]
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	if err := manager.Disconnect(name, "user requested"); err != nil {
		return "", err
	}

	return fmt.Sprintf("Disconnected from %s", name), nil
}

func handleListServers(ctx context.Context, args map[string]string) (string, error) {
	rows, err := storage.Query(serversTable, nil, "", nil, "name ASC", 100, 0)
	if err != nil {
		return "", fmt.Errorf("failed to query servers: %w", err)
	}

	type serverInfo struct {
		Name        string `json:"name"`
		Command     string `json:"command"`
		Args        []string `json:"args"`
		WorkingDir  string `json:"working_dir,omitempty"`
		AutoStart   bool   `json:"auto_start"`
		Connected   bool   `json:"connected"`
		ToolsCount  int    `json:"tools_count,omitempty"`
		ConnectedAt string `json:"connected_at,omitempty"`
	}

	servers := make([]serverInfo, 0, len(rows))
	for _, row := range rows {
		config, err := rowToConfig(row)
		if err != nil {
			continue
		}

		info := serverInfo{
			Name:       config.Name,
			Command:    config.Command,
			Args:       config.Args,
			WorkingDir: config.WorkingDir,
			AutoStart:  config.AutoStart,
		}

		// Check runtime state
		if state := manager.GetServer(config.Name); state != nil && state.Connected {
			info.Connected = true
			info.ToolsCount = len(state.Tools)
			info.ConnectedAt = state.ConnectedAt.Format(time.RFC3339)
		}

		servers = append(servers, info)
	}

	data, _ := json.Marshal(servers)
	return string(data), nil
}

func handleListTools(ctx context.Context, args map[string]string) (string, error) {
	serverFilter := args["server"]

	type toolInfo struct {
		Server      string      `json:"server"`
		Name        string      `json:"name"`
		Description string      `json:"description,omitempty"`
		InputSchema interface{} `json:"input_schema,omitempty"`
	}

	var tools []toolInfo

	for _, state := range manager.AllServers() {
		if !state.Connected {
			continue
		}
		if serverFilter != "" && state.Name != serverFilter {
			continue
		}

		state.mu.RLock()
		for _, tool := range state.Tools {
			info := toolInfo{
				Server:      state.Name,
				Name:        tool.Name,
				Description: tool.Description,
			}
			if tool.InputSchema != nil {
				info.InputSchema = tool.InputSchema
			}
			tools = append(tools, info)
		}
		state.mu.RUnlock()
	}

	if len(tools) == 0 {
		if serverFilter != "" {
			return "", fmt.Errorf("no tools found for server %s (not connected or no tools)", serverFilter)
		}
		return "[]", nil
	}

	data, _ := json.Marshal(tools)
	return string(data), nil
}

func handleCallTool(ctx context.Context, args map[string]string) (string, error) {
	serverName := args["server"]
	toolName := args["tool"]
	if serverName == "" || toolName == "" {
		return "", fmt.Errorf("server and tool are required")
	}

	state := manager.GetServer(serverName)
	if state == nil || !state.Connected {
		return "", fmt.Errorf("server %s is not connected", serverName)
	}

	// Parse arguments - ensure we always have at least an empty map
	toolArgs := make(map[string]interface{})
	if argsJSON := args["arguments"]; argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
			return "", fmt.Errorf("invalid arguments JSON: %w", err)
		}
	}

	// Get timeout from config
	timeout := 60 * time.Second
	if t := client.GetConfig("tool_call_timeout"); t != "" {
		var secs int
		if _, err := fmt.Sscanf(t, "%d", &secs); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	// Log the call for debugging
	argsDebug, _ := json.Marshal(toolArgs)
	log.Printf("[MCP] Calling tool %s on %s with args: %s", toolName, serverName, string(argsDebug))

	// Call tool with timeout
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := state.Session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: toolArgs,
	})
	if err != nil {
		log.Printf("[MCP] Tool call error: %v", err)
		return "", fmt.Errorf("tool call failed: %w", err)
	}
	log.Printf("[MCP] Tool call completed, isError=%v, content count=%d", result.IsError, len(result.Content))

	// If the tool returned an error, extract and return it
	if result.IsError {
		var errorText string
		for _, content := range result.Content {
			if tc, ok := content.(*mcp.TextContent); ok {
				errorText += tc.Text + "\n"
			}
		}
		if errorText == "" {
			errorText = "unknown error from tool"
		}
		log.Printf("[MCP] Tool %s returned error: %s", toolName, errorText)
		return "", fmt.Errorf("tool error: %s", errorText)
	}

	// Extract content from result
	var contents []interface{}
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			contents = append(contents, map[string]interface{}{
				"type": "text",
				"text": c.Text,
			})
		case *mcp.ImageContent:
			contents = append(contents, map[string]interface{}{
				"type":      "image",
				"data":      c.Data,
				"mime_type": c.MIMEType,
			})
		case *mcp.AudioContent:
			contents = append(contents, map[string]interface{}{
				"type":      "audio",
				"data":      c.Data,
				"mime_type": c.MIMEType,
			})
		case *mcp.EmbeddedResource:
			contents = append(contents, map[string]interface{}{
				"type":      "resource",
				"uri":       c.Resource.URI,
				"mime_type": c.Resource.MIMEType,
			})
		default:
			contents = append(contents, content)
		}
	}

	response := map[string]interface{}{
		"success": true,
		"content": contents,
	}

	data, _ := json.Marshal(response)
	log.Printf("[MCP] Tool response: %s", string(data))
	return string(data), nil
}

func handleStatus(ctx context.Context, args map[string]string) (string, error) {
	var connectedCount, totalTools int
	connectedServers := make([]string, 0)

	for _, state := range manager.AllServers() {
		if state.Connected {
			connectedCount++
			connectedServers = append(connectedServers, state.Name)
			totalTools += len(state.Tools)
		}
	}

	// Count total configured servers
	rows, _ := storage.Query(serversTable, nil, "", nil, "", 100, 0)

	status := map[string]interface{}{
		"configured_servers":  len(rows),
		"connected_servers":   connectedCount,
		"connected_names":     connectedServers,
		"total_tools":         totalTools,
		"connection_timeout":  client.GetConfig("connection_timeout"),
		"tool_call_timeout":   client.GetConfig("tool_call_timeout"),
	}

	data, _ := json.Marshal(status)
	return string(data), nil
}
