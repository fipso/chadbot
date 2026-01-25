package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
	"github.com/google/uuid"
)

const hooksTable = "hooks"

var (
	client  *sdk.Client
	storage *sdk.StorageClient

	// Track active subscriptions
	subscriptionMu sync.RWMutex
	subscriptions  = make(map[string]bool) // event type -> subscribed

	// Evaluation chat - used for processing hooks
	evalChatID string
)

// Hook represents a user-defined automation hook
type Hook struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Events    []string `json:"events"` // Event types to listen for
	Body      string   `json:"body"`   // Natural language instructions
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func main() {
	log.SetPrefix("[TextHooks] ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}

	// Create SDK client
	client = sdk.NewClient("texthooks", "0.1.0", "User-defined automation hooks with natural language instructions")
	client = client.WithSocket(socketPath)

	// Register skills before connecting
	registerSkills()

	// Set up event handler
	client.OnEvent(handleEvent)

	// Connect to backend
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	log.Println("Plugin registered successfully")

	// Get storage client
	storage = client.Storage()

	// Initialize database table
	if err := initStorage(); err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Subscribe to events for enabled hooks
	if err := updateEventSubscriptions(); err != nil {
		log.Printf("Warning: Failed to update subscriptions: %v", err)
	}

	// Initialize evaluation chat
	if err := initEvalChat(); err != nil {
		log.Printf("Warning: Failed to initialize eval chat: %v", err)
	}

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Run until shutdown
	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("Client error: %v", err)
	}
	client.Close()
}

func initStorage() error {
	// Create hooks table
	columns := []*pb.ColumnDef{
		{Name: "id", Type: "TEXT", PrimaryKey: true},
		{Name: "name", Type: "TEXT", NotNull: true},
		{Name: "events", Type: "TEXT", NotNull: true},    // JSON array
		{Name: "body", Type: "TEXT", NotNull: true},
		{Name: "enabled", Type: "TEXT", NotNull: true},   // "true" or "false"
		{Name: "created_at", Type: "TEXT", NotNull: true},
		{Name: "updated_at", Type: "TEXT", NotNull: true},
	}

	if err := storage.CreateTable(hooksTable, columns, true); err != nil {
		return fmt.Errorf("failed to create hooks table: %w", err)
	}

	log.Println("Storage initialized")
	return nil
}

func initEvalChat() error {
	// Create or get a chat for hook evaluations
	resp, err := client.ChatGetOrCreate("texthooks", "hook-eval", "Hook Evaluations")
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("failed to create eval chat: %s", resp.Error)
	}
	evalChatID = resp.ChatId
	log.Printf("Using eval chat: %s", evalChatID)
	return nil
}

func registerSkills() {
	skills := []*pb.Skill{
		{
			Name:        "hooks_create",
			Description: "Create a new automation hook that triggers on specified events",
			Parameters: []*pb.SkillParameter{
				{Name: "name", Type: "string", Description: "A descriptive name for the hook", Required: true},
				{Name: "events", Type: "string", Description: "Comma-separated list of event types to trigger on (e.g., 'chat.message.received,whatsapp.message')", Required: true},
				{Name: "body", Type: "string", Description: "Natural language instructions describing when to trigger and what actions to take", Required: true},
			},
		},
		{
			Name:        "hooks_list",
			Description: "List all automation hooks",
			Parameters:  []*pb.SkillParameter{},
		},
		{
			Name:        "hooks_get",
			Description: "Get details of a specific hook",
			Parameters: []*pb.SkillParameter{
				{Name: "hook_id", Type: "string", Description: "The ID of the hook to retrieve", Required: true},
			},
		},
		{
			Name:        "hooks_update",
			Description: "Update an existing hook",
			Parameters: []*pb.SkillParameter{
				{Name: "hook_id", Type: "string", Description: "The ID of the hook to update", Required: true},
				{Name: "name", Type: "string", Description: "New name for the hook (optional)", Required: false},
				{Name: "events", Type: "string", Description: "New comma-separated list of event types (optional)", Required: false},
				{Name: "body", Type: "string", Description: "New instructions for the hook (optional)", Required: false},
			},
		},
		{
			Name:        "hooks_delete",
			Description: "Delete an automation hook",
			Parameters: []*pb.SkillParameter{
				{Name: "hook_id", Type: "string", Description: "The ID of the hook to delete", Required: true},
			},
		},
		{
			Name:        "hooks_enable",
			Description: "Enable a disabled hook",
			Parameters: []*pb.SkillParameter{
				{Name: "hook_id", Type: "string", Description: "The ID of the hook to enable", Required: true},
			},
		},
		{
			Name:        "hooks_disable",
			Description: "Disable a hook without deleting it",
			Parameters: []*pb.SkillParameter{
				{Name: "hook_id", Type: "string", Description: "The ID of the hook to disable", Required: true},
			},
		},
	}

	for _, skill := range skills {
		client.RegisterSkill(skill, makeSkillHandler(skill.Name))
		log.Printf("Registered skill: %s", skill.Name)
	}
}

func makeSkillHandler(skillName string) sdk.SkillHandler {
	return func(ctx context.Context, args map[string]string) (string, error) {
		switch skillName {
		case "hooks_create":
			return handleCreate(ctx, args)
		case "hooks_list":
			return handleList(ctx, args)
		case "hooks_get":
			return handleGet(ctx, args)
		case "hooks_update":
			return handleUpdate(ctx, args)
		case "hooks_delete":
			return handleDelete(ctx, args)
		case "hooks_enable":
			return handleEnable(ctx, args)
		case "hooks_disable":
			return handleDisable(ctx, args)
		default:
			return "", fmt.Errorf("unknown skill: %s", skillName)
		}
	}
}

func handleCreate(ctx context.Context, args map[string]string) (string, error) {
	name := args["name"]
	eventsStr := args["events"]
	body := args["body"]

	if name == "" || eventsStr == "" || body == "" {
		return "", fmt.Errorf("name, events, and body are required")
	}

	// Parse events
	events := strings.Split(eventsStr, ",")
	for i := range events {
		events[i] = strings.TrimSpace(events[i])
	}

	eventsJSON, _ := json.Marshal(events)
	now := time.Now().Format(time.RFC3339)
	id := uuid.New().String()

	if err := storage.Insert(hooksTable, map[string]string{
		"id":         id,
		"name":       name,
		"events":     string(eventsJSON),
		"body":       body,
		"enabled":    "true",
		"created_at": now,
		"updated_at": now,
	}); err != nil {
		return "", fmt.Errorf("failed to create hook: %w", err)
	}

	// Update subscriptions
	if err := updateEventSubscriptions(); err != nil {
		log.Printf("Warning: Failed to update subscriptions: %v", err)
	}

	return fmt.Sprintf("Created hook '%s' (ID: %s) listening for events: %s", name, id, eventsStr), nil
}

func handleList(ctx context.Context, args map[string]string) (string, error) {
	hooks, err := getAllHooks()
	if err != nil {
		return "", fmt.Errorf("failed to list hooks: %w", err)
	}

	if len(hooks) == 0 {
		return "No hooks configured", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d hooks:\n\n", len(hooks)))

	for _, h := range hooks {
		status := "enabled"
		if !h.Enabled {
			status = "disabled"
		}
		sb.WriteString(fmt.Sprintf("**%s** (ID: %s) [%s]\n", h.Name, h.ID[:8], status))
		sb.WriteString(fmt.Sprintf("  Events: %s\n", strings.Join(h.Events, ", ")))
		sb.WriteString(fmt.Sprintf("  Body: %s\n\n", truncate(h.Body, 100)))
	}

	return sb.String(), nil
}

func handleGet(ctx context.Context, args map[string]string) (string, error) {
	hookID := args["hook_id"]
	if hookID == "" {
		return "", fmt.Errorf("hook_id is required")
	}

	hook, err := findHook(hookID)
	if err != nil {
		return "", err
	}

	status := "enabled"
	if !hook.Enabled {
		status = "disabled"
	}

	return fmt.Sprintf(`**%s** [%s]
ID: %s
Events: %s
Created: %s
Updated: %s

Instructions:
%s`, hook.Name, status, hook.ID, strings.Join(hook.Events, ", "),
		hook.CreatedAt, hook.UpdatedAt, hook.Body), nil
}

func handleUpdate(ctx context.Context, args map[string]string) (string, error) {
	hookID := args["hook_id"]
	if hookID == "" {
		return "", fmt.Errorf("hook_id is required")
	}

	hook, err := findHook(hookID)
	if err != nil {
		return "", err
	}

	updates := make(map[string]string)

	if name, ok := args["name"]; ok && name != "" {
		updates["name"] = name
	}

	if eventsStr, ok := args["events"]; ok && eventsStr != "" {
		events := strings.Split(eventsStr, ",")
		for i := range events {
			events[i] = strings.TrimSpace(events[i])
		}
		eventsJSON, _ := json.Marshal(events)
		updates["events"] = string(eventsJSON)
	}

	if body, ok := args["body"]; ok && body != "" {
		updates["body"] = body
	}

	if len(updates) == 0 {
		return "No updates provided", nil
	}

	updates["updated_at"] = time.Now().Format(time.RFC3339)

	if err := storage.Update(hooksTable, updates, "id = ?", hook.ID); err != nil {
		return "", fmt.Errorf("failed to update hook: %w", err)
	}

	// Update subscriptions in case events changed
	if err := updateEventSubscriptions(); err != nil {
		log.Printf("Warning: Failed to update subscriptions: %v", err)
	}

	return fmt.Sprintf("Updated hook '%s'", hook.Name), nil
}

func handleDelete(ctx context.Context, args map[string]string) (string, error) {
	hookID := args["hook_id"]
	if hookID == "" {
		return "", fmt.Errorf("hook_id is required")
	}

	hook, err := findHook(hookID)
	if err != nil {
		return "", err
	}

	name := hook.Name
	if err := storage.Delete(hooksTable, "id = ?", hook.ID); err != nil {
		return "", fmt.Errorf("failed to delete hook: %w", err)
	}

	// Update subscriptions
	if err := updateEventSubscriptions(); err != nil {
		log.Printf("Warning: Failed to update subscriptions: %v", err)
	}

	return fmt.Sprintf("Deleted hook '%s'", name), nil
}

func handleEnable(ctx context.Context, args map[string]string) (string, error) {
	hookID := args["hook_id"]
	if hookID == "" {
		return "", fmt.Errorf("hook_id is required")
	}

	hook, err := findHook(hookID)
	if err != nil {
		return "", err
	}

	if hook.Enabled {
		return fmt.Sprintf("Hook '%s' is already enabled", hook.Name), nil
	}

	if err := storage.Update(hooksTable, map[string]string{
		"enabled":    "true",
		"updated_at": time.Now().Format(time.RFC3339),
	}, "id = ?", hook.ID); err != nil {
		return "", fmt.Errorf("failed to enable hook: %w", err)
	}

	// Update subscriptions
	if err := updateEventSubscriptions(); err != nil {
		log.Printf("Warning: Failed to update subscriptions: %v", err)
	}

	return fmt.Sprintf("Enabled hook '%s'", hook.Name), nil
}

func handleDisable(ctx context.Context, args map[string]string) (string, error) {
	hookID := args["hook_id"]
	if hookID == "" {
		return "", fmt.Errorf("hook_id is required")
	}

	hook, err := findHook(hookID)
	if err != nil {
		return "", err
	}

	if !hook.Enabled {
		return fmt.Sprintf("Hook '%s' is already disabled", hook.Name), nil
	}

	if err := storage.Update(hooksTable, map[string]string{
		"enabled":    "false",
		"updated_at": time.Now().Format(time.RFC3339),
	}, "id = ?", hook.ID); err != nil {
		return "", fmt.Errorf("failed to disable hook: %w", err)
	}

	return fmt.Sprintf("Disabled hook '%s'", hook.Name), nil
}

// rowToHook converts a storage row to a Hook struct
func rowToHook(row *pb.Row) (*Hook, error) {
	var events []string
	if err := json.Unmarshal([]byte(row.Values["events"]), &events); err != nil {
		return nil, fmt.Errorf("failed to parse events: %w", err)
	}

	return &Hook{
		ID:        row.Values["id"],
		Name:      row.Values["name"],
		Events:    events,
		Body:      row.Values["body"],
		Enabled:   row.Values["enabled"] == "true",
		CreatedAt: row.Values["created_at"],
		UpdatedAt: row.Values["updated_at"],
	}, nil
}

func getAllHooks() ([]Hook, error) {
	rows, err := storage.Query(hooksTable, nil, "", nil, "created_at DESC", 0, 0)
	if err != nil {
		return nil, err
	}

	hooks := make([]Hook, 0, len(rows))
	for _, row := range rows {
		hook, err := rowToHook(row)
		if err != nil {
			log.Printf("Warning: skipping malformed hook: %v", err)
			continue
		}
		hooks = append(hooks, *hook)
	}

	return hooks, nil
}

func getEnabledHooks() ([]Hook, error) {
	rows, err := storage.Query(hooksTable, nil, "enabled = ?", []string{"true"}, "", 0, 0)
	if err != nil {
		return nil, err
	}

	hooks := make([]Hook, 0, len(rows))
	for _, row := range rows {
		hook, err := rowToHook(row)
		if err != nil {
			log.Printf("Warning: skipping malformed hook: %v", err)
			continue
		}
		hooks = append(hooks, *hook)
	}

	return hooks, nil
}

func findHook(idOrPrefix string) (*Hook, error) {
	// Try exact match first
	rows, err := storage.Query(hooksTable, nil, "id = ?", []string{idOrPrefix}, "", 1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rowToHook(rows[0])
	}

	// Try prefix match
	rows, err = storage.Query(hooksTable, nil, "id LIKE ?", []string{idOrPrefix + "%"}, "", 1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rowToHook(rows[0])
	}

	return nil, fmt.Errorf("hook not found: %s", idOrPrefix)
}

func updateEventSubscriptions() error {
	hooks, err := getEnabledHooks()
	if err != nil {
		return err
	}

	neededEvents := make(map[string]bool)
	for _, h := range hooks {
		for _, e := range h.Events {
			neededEvents[e] = true
		}
	}

	subscriptionMu.Lock()
	defer subscriptionMu.Unlock()

	// Collect events to subscribe to
	var toSubscribe []string
	for eventType := range neededEvents {
		if !subscriptions[eventType] {
			toSubscribe = append(toSubscribe, eventType)
			subscriptions[eventType] = true
		}
	}

	// Subscribe to all new events at once
	if len(toSubscribe) > 0 {
		if err := client.Subscribe(toSubscribe); err != nil {
			return fmt.Errorf("failed to subscribe: %w", err)
		}
		log.Printf("Subscribed to events: %v", toSubscribe)
	}

	return nil
}

func handleEvent(event *pb.Event) {
	log.Printf("Received event: %s", event.EventType)

	// Find all enabled hooks that match this event type
	hooks, err := getEnabledHooks()
	if err != nil {
		log.Printf("Failed to query hooks: %v", err)
		return
	}

	for _, hook := range hooks {
		for _, e := range hook.Events {
			if e == event.EventType || matchesWildcard(e, event.EventType) {
				go processHook(hook, event)
				break
			}
		}
	}
}

func matchesWildcard(pattern, eventType string) bool {
	// Support wildcards like "chat.*" or "whatsapp.*"
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(eventType, prefix+".")
	}
	return false
}

func processHook(hook Hook, event *pb.Event) {
	log.Printf("Processing hook '%s' for event %s", hook.Name, event.EventType)

	if evalChatID == "" {
		log.Printf("No eval chat available, skipping hook")
		return
	}

	// Format the event data nicely
	var prettyData string
	switch d := event.Data.(type) {
	case *pb.Event_ChatMessage:
		data := map[string]interface{}{
			"platform":     d.ChatMessage.Platform,
			"chat_id":      d.ChatMessage.ChatId,
			"message_id":   d.ChatMessage.MessageId,
			"sender_id":    d.ChatMessage.SenderId,
			"sender_name":  d.ChatMessage.SenderName,
			"content":      d.ChatMessage.Content,
			"content_type": d.ChatMessage.ContentType,
			"reply_to":     d.ChatMessage.ReplyTo,
			"metadata":     d.ChatMessage.Metadata,
		}
		pretty, _ := json.MarshalIndent(data, "", "  ")
		prettyData = string(pretty)
	case *pb.Event_Generic:
		if d.Generic != nil && d.Generic.Payload != nil {
			pretty, _ := json.MarshalIndent(d.Generic.Payload.AsMap(), "", "  ")
			prettyData = string(pretty)
		} else {
			prettyData = "{}"
		}
	default:
		prettyData = "{}"
	}

	// Build the prompt for the LLM
	prompt := fmt.Sprintf(`You are processing an automation hook. Evaluate the conditions and execute actions as needed.

**Hook Name:** %s

**Event Type:** %s

**Event Data:**
%s

**Hook Instructions:**
%s

---

Based on the hook instructions above, evaluate if the conditions are met for this event. If so, execute the appropriate actions using available skills. If the conditions are not met, simply respond with "Hook conditions not met, no action taken."

Be concise in your response. If you execute actions, briefly describe what you did.`, hook.Name, event.EventType, prettyData, hook.Body)

	// Add message to eval chat
	addResp, err := client.ChatAddMessage(evalChatID, "user", prompt)
	if err != nil || !addResp.Success {
		log.Printf("Failed to add message: %v", err)
		return
	}

	// Request LLM response synchronously
	llmResp, err := client.ChatLLMRequestSync(evalChatID, "", 60*time.Second)
	if err != nil {
		log.Printf("Failed to process hook '%s': %v", hook.Name, err)
		return
	}

	log.Printf("Hook '%s' result: %s", hook.Name, truncate(llmResp.Content, 200))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
