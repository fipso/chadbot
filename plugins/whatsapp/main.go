package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"

	_ "github.com/mattn/go-sqlite3"
)

const messagesTable = "messages"

var (
	client    *sdk.Client
	waClient  *whatsmeow.Client
	myJID     types.JID
	container *sqlstore.Container
	storage   *sdk.StorageClient

	// pendingLLM tracks chats waiting for LLM responses
	pendingLLM   = make(map[string]bool)
	pendingLLMMu sync.Mutex
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[WhatsApp] Shutting down...")
		cancel()
	}()

	// Initialize SDK client
	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}
	client = sdk.NewClient("whatsapp", "1.0.0", "WhatsApp messaging integration via whatsmeow")
	client = client.WithSocket(socketPath)

	// Register skills
	registerSkills()

	// Connect to chadbot backend
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("[WhatsApp] Failed to connect to backend: %v", err)
	}
	defer client.Close()

	// Register plugin config
	if err := registerConfig(); err != nil {
		log.Printf("[WhatsApp] Failed to register config: %v", err)
	}

	// Handle config changes
	client.OnConfigChanged(func(key, value string, allValues map[string]string) {
		log.Printf("[WhatsApp] Config changed: %s = %s", key, value)
	})

	// Initialize storage
	storage = client.Storage()
	if err := initStorage(); err != nil {
		log.Fatalf("[WhatsApp] Failed to initialize storage: %v", err)
	}

	// Handle chat LLM responses
	client.OnChatLLMResponse(handleLLMResponse)

	// Initialize WhatsApp
	if err := initWhatsApp(ctx); err != nil {
		log.Fatalf("[WhatsApp] Failed to initialize WhatsApp: %v", err)
	}

	log.Println("[WhatsApp] Plugin started")

	// Run the SDK client event loop
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[WhatsApp] Client error: %v", err)
	}
}

func registerConfig() error {
	return client.RegisterConfig([]sdk.ConfigField{
		{
			Key:          "self_chat_to_llm",
			Label:        "Self-Chat to LLM",
			Description:  "When enabled, messages you send to yourself will be processed by the LLM and respond back",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_BOOL,
			DefaultValue: "false",
		},
	})
}

func registerSkills() {
	// Send message to a contact
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_send_message",
		Description: "Send a WhatsApp message to a contact or group",
		Parameters: []*pb.SkillParameter{
			{Name: "to", Type: "string", Description: "Phone number (with country code) or group JID", Required: true},
			{Name: "message", Type: "string", Description: "Message text to send", Required: true},
		},
	}, handleSendMessage)

	// List contacts
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_list_contacts",
		Description: "List all WhatsApp contacts",
		Parameters:  []*pb.SkillParameter{},
	}, handleListContacts)

	// List groups
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_list_groups",
		Description: "List all WhatsApp groups",
		Parameters:  []*pb.SkillParameter{},
	}, handleListGroups)

	// Force re-login (show QR code)
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_relogin",
		Description: "Force WhatsApp re-login and display QR code",
		Parameters:  []*pb.SkillParameter{},
	}, handleRelogin)

	// Get chat history
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_get_chat_history",
		Description: "Get chat history from a contact or group",
		Parameters: []*pb.SkillParameter{
			{Name: "chat_jid", Type: "string", Description: "Chat JID (phone@s.whatsapp.net or group@g.us)", Required: true},
			{Name: "limit", Type: "number", Description: "Maximum number of messages to return (default: 50)", Required: false},
			{Name: "offset", Type: "number", Description: "Number of messages to skip (default: 0)", Required: false},
		},
	}, handleGetChatHistory)

	// List chats with recent messages
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_list_chats",
		Description: "List all chats with their most recent message and contact name",
		Parameters: []*pb.SkillParameter{
			{Name: "limit", Type: "number", Description: "Maximum number of chats to return (default: 20)", Required: false},
		},
	}, handleListChats)

	// Search contacts by name
	client.RegisterSkill(&pb.Skill{
		Name:        "whatsapp_search_contact",
		Description: "Search for a WhatsApp contact by name (case-insensitive partial match)",
		Parameters: []*pb.SkillParameter{
			{Name: "query", Type: "string", Description: "Name to search for", Required: true},
		},
	}, handleSearchContact)
}

func initWhatsApp(ctx context.Context) error {
	// Initialize SQLite store for WhatsApp session
	dbLog := waLog.Stdout("Database", "WARN", true)
	var err error
	container, err = sqlstore.New(ctx, "sqlite3", "file:whatsapp.db?_foreign_keys=on", dbLog)
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	// Get or create device
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	clientLog := waLog.Stdout("Client", "WARN", true)
	waClient = whatsmeow.NewClient(deviceStore, clientLog)

	// Set up event handlers
	waClient.AddEventHandler(handleWhatsAppEvent)

	// Connect to WhatsApp
	if waClient.Store.ID == nil {
		// No session, need to login with QR code
		log.Println("[WhatsApp] No session found, displaying QR code...")
		qrChan, _ := waClient.GetQRChannel(ctx)
		if err := waClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				log.Println("[WhatsApp] Scan this QR code:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				log.Printf("[WhatsApp] Login event: %s", evt.Event)
			}
		}
	} else {
		if err := waClient.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	myJID = *waClient.Store.ID
	log.Printf("[WhatsApp] Connected as %s", myJID.String())

	return nil
}

func initStorage() error {
	// Create messages table
	return storage.CreateTable(messagesTable, []*pb.ColumnDef{
		{Name: "id", Type: "TEXT", PrimaryKey: true},
		{Name: "chat_jid", Type: "TEXT", NotNull: true},
		{Name: "sender_jid", Type: "TEXT", NotNull: true},
		{Name: "content", Type: "TEXT"},
		{Name: "timestamp", Type: "INTEGER", NotNull: true},
		{Name: "from_me", Type: "INTEGER", NotNull: true},
	}, true)
}

func handleWhatsAppEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		handleIncomingMessage(v)
	case *events.HistorySync:
		handleHistorySync(v)
	case *events.Connected:
		log.Println("[WhatsApp] Connected to WhatsApp")
	case *events.Disconnected:
		log.Println("[WhatsApp] Disconnected from WhatsApp")
	case *events.LoggedOut:
		log.Println("[WhatsApp] Logged out from WhatsApp")
	}
}

func handleHistorySync(evt *events.HistorySync) {
	log.Printf("[WhatsApp] History sync received: %d conversations", len(evt.Data.GetConversations()))

	for _, conv := range evt.Data.GetConversations() {
		chatJID := conv.GetID()
		for _, msg := range conv.GetMessages() {
			histMsg := msg.GetMessage()
			if histMsg == nil {
				continue
			}

			msgInfo := histMsg.GetKey()
			if msgInfo == nil {
				continue
			}

			// Extract text content
			var text string
			waMsg := histMsg.GetMessage()
			if waMsg != nil {
				if waMsg.GetConversation() != "" {
					text = waMsg.GetConversation()
				} else if waMsg.GetExtendedTextMessage() != nil {
					text = waMsg.GetExtendedTextMessage().GetText()
				}
			}

			if text == "" {
				continue
			}

			// Store message
			fromMe := msgInfo.GetFromMe()
			timestamp := histMsg.GetMessageTimestamp()
			senderJID := msgInfo.GetParticipant()
			if senderJID == "" {
				if fromMe {
					senderJID = myJID.String()
				} else {
					senderJID = msgInfo.GetRemoteJID()
				}
			}

			storeMessage(msgInfo.GetID(), chatJID, senderJID, text, int64(timestamp), fromMe)
		}
	}
}

func storeMessage(msgID, chatJID, senderJID, content string, timestamp int64, fromMe bool) {
	fromMeInt := "0"
	if fromMe {
		fromMeInt = "1"
	}

	err := storage.Insert(messagesTable, map[string]string{
		"id":         msgID,
		"chat_jid":   chatJID,
		"sender_jid": senderJID,
		"content":    content,
		"timestamp":  strconv.FormatInt(timestamp, 10),
		"from_me":    fromMeInt,
	})
	if err != nil {
		// Silently ignore duplicate key errors (message already exists from history sync)
		// Only log actual errors
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			log.Printf("[WhatsApp] Failed to store message %s: %v", msgID, err)
		}
	}
}

func handleIncomingMessage(msg *events.Message) {
	// Get message text
	var text string
	if msg.Message.GetConversation() != "" {
		text = msg.Message.GetConversation()
	} else if msg.Message.GetExtendedTextMessage() != nil {
		text = msg.Message.GetExtendedTextMessage().GetText()
	}

	if text == "" {
		return // Skip non-text messages for now
	}

	chatJID := msg.Info.Chat
	isFromMe := msg.Info.IsFromMe
	senderJID := msg.Info.Sender.String()

	log.Printf("[WhatsApp] Message in %s (server: %s): %s (fromMe: %v, sender: %s)", chatJID, chatJID.Server, text, isFromMe, senderJID)

	// Store the message
	storeMessage(msg.Info.ID, chatJID.String(), senderJID, text, msg.Info.Timestamp.Unix(), isFromMe)

	// If message is from self (writing to myself), trigger LLM
	// Self-chat detection: check both regular JID and LID format
	isSelfChat := false
	if isFromMe {
		// Check regular JID match
		if chatJID.ToNonAD().String() == myJID.ToNonAD().String() {
			isSelfChat = true
		}
		// Check LID (Linked ID) - if chat is LID server and sender matches receiver
		// In self-chat on LID, the chat JID is your LID
		if chatJID.Server == "lid" {
			// For LID self-chats, the chat is with yourself
			// We can verify by checking that sender == chat JID
			if msg.Info.Sender.ToNonAD().String() == chatJID.ToNonAD().String() {
				isSelfChat = true
			}
		}
	}

	if isSelfChat {
		// Check if self-chat to LLM is enabled
		if client.GetConfigBool("self_chat_to_llm") {
			log.Printf("[WhatsApp] Self-message detected, triggering LLM...")
			handleSelfMessage(text, chatJID.String())
		} else {
			log.Printf("[WhatsApp] Self-message detected but self_chat_to_llm is disabled")
		}
	}
}

func handleSelfMessage(content, chatJID string) {
	// Get or create linked chat
	resp, err := client.ChatGetOrCreate("whatsapp", chatJID, "WhatsApp Self Chat")
	if err != nil || !resp.Success {
		log.Printf("[WhatsApp] Failed to get/create chat: %v", err)
		return
	}

	// If this is a new chat, sync historical messages
	if resp.Created {
		syncHistoricalMessages(resp.ChatId, chatJID)
	}

	// Add user message
	addResp, err := client.ChatAddMessage(resp.ChatId, "user", content)
	if err != nil || !addResp.Success {
		log.Printf("[WhatsApp] Failed to add message: %v", err)
		return
	}

	// Mark as pending LLM
	pendingLLMMu.Lock()
	pendingLLM[chatJID] = true
	pendingLLMMu.Unlock()

	// Request LLM response
	if err := client.ChatLLMRequest(resp.ChatId, ""); err != nil {
		log.Printf("[WhatsApp] Failed to request LLM: %v", err)
		pendingLLMMu.Lock()
		delete(pendingLLM, chatJID)
		pendingLLMMu.Unlock()
	}
}

func syncHistoricalMessages(chatID, chatJID string) {
	// Load recent messages from our storage (last 50)
	rows, err := storage.Query(messagesTable, nil, "chat_jid = ?", []string{chatJID}, "timestamp ASC", 50, 0)
	if err != nil {
		log.Printf("[WhatsApp] Failed to load historical messages: %v", err)
		return
	}

	log.Printf("[WhatsApp] Syncing %d historical messages to LLM chat", len(rows))

	for _, row := range rows {
		role := "user"
		if row.Values["from_me"] == "1" {
			role = "user" // Messages from me are user messages
		} else {
			role = "assistant" // In self-chat, non-from-me would be previous AI responses or we can skip
			// Actually in self-chat there's only one person, so skip this logic
			continue
		}

		content := row.Values["content"]
		if content == "" {
			continue
		}

		_, err := client.ChatAddMessage(chatID, role, content)
		if err != nil {
			log.Printf("[WhatsApp] Failed to sync message: %v", err)
		}
	}
}

func handleLLMResponse(resp *pb.ChatLLMResponse) {
	if !resp.Success {
		log.Printf("[WhatsApp] LLM error: %s", resp.Error)
		return
	}

	// Find the WhatsApp chat for this response
	// We need to look up by chat ID - for now, send to self
	ctx := context.Background()

	// Send to self chat
	selfJID := myJID.ToNonAD()
	_, err := waClient.SendMessage(ctx, selfJID, &waE2E.Message{
		Conversation: proto.String(resp.Content),
	})
	if err != nil {
		log.Printf("[WhatsApp] Failed to send LLM response: %v", err)
	} else {
		log.Printf("[WhatsApp] Sent LLM response to self chat")
	}
}

func handleSendMessage(ctx context.Context, args map[string]string) (string, error) {
	to := args["to"]
	message := args["message"]

	if to == "" || message == "" {
		return "", fmt.Errorf("'to' and 'message' are required")
	}

	// Parse JID
	var jid types.JID
	var err error

	// Check if it's already a JID format
	if len(to) > 10 && (to[len(to)-5:] == "@s.whatsapp.net" || to[len(to)-5:] == "@g.us") {
		jid, err = types.ParseJID(to)
	} else {
		// Assume it's a phone number
		jid = types.NewJID(to, types.DefaultUserServer)
	}

	if err != nil {
		return "", fmt.Errorf("invalid JID: %w", err)
	}

	// Send message
	_, err = waClient.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	return fmt.Sprintf("Message sent to %s", jid.String()), nil
}

func handleListContacts(ctx context.Context, args map[string]string) (string, error) {
	if waClient == nil || waClient.Store == nil || waClient.Store.Contacts == nil {
		return "", fmt.Errorf("WhatsApp not fully initialized")
	}

	contacts, err := waClient.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get contacts: %w", err)
	}

	type contact struct {
		JID  string `json:"jid"`
		Name string `json:"name"`
	}

	var result []contact
	for jid, info := range contacts {
		name := info.FullName
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			name = info.BusinessName
		}
		result = append(result, contact{
			JID:  jid.String(),
			Name: name,
		})
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleListGroups(ctx context.Context, args map[string]string) (string, error) {
	if waClient == nil {
		return "", fmt.Errorf("WhatsApp not initialized")
	}

	groups, err := waClient.GetJoinedGroups(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get groups: %w", err)
	}

	type group struct {
		JID  string `json:"jid"`
		Name string `json:"name"`
	}

	var result []group
	for _, g := range groups {
		result = append(result, group{
			JID:  g.JID.String(),
			Name: g.Name,
		})
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func handleRelogin(ctx context.Context, args map[string]string) (string, error) {
	// Disconnect and clear session
	waClient.Disconnect()

	// Delete existing device
	if err := waClient.Store.Delete(ctx); err != nil {
		log.Printf("[WhatsApp] Warning: failed to delete store: %v", err)
	}

	// Get new device
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create new device: %w", err)
	}

	clientLog := waLog.Stdout("Client", "WARN", true)
	waClient = whatsmeow.NewClient(deviceStore, clientLog)
	waClient.AddEventHandler(handleWhatsAppEvent)

	// Get QR code
	qrChan, _ := waClient.GetQRChannel(ctx)
	if err := waClient.Connect(); err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}

	// Wait for QR code
	var qrCode string
	timeout := time.After(30 * time.Second)

	select {
	case evt := <-qrChan:
		if evt.Event == "code" {
			qrCode = evt.Code
			log.Println("[WhatsApp] New QR code generated:")
			qrterminal.GenerateHalfBlock(qrCode, qrterminal.L, os.Stdout)
		}
	case <-timeout:
		return "", fmt.Errorf("QR code timeout")
	}

	// Return QR code data (can be rendered in chat)
	return fmt.Sprintf("Scan this QR code to login:\n\n```\n%s\n```\n\n(Also displayed in plugin logs)", qrCode), nil
}

func handleGetChatHistory(ctx context.Context, args map[string]string) (string, error) {
	chatJID := args["chat_jid"]
	if chatJID == "" {
		return "", fmt.Errorf("chat_jid is required")
	}

	// Parse limit and offset
	limit := int32(50)
	offset := int32(0)

	if args["limit"] != "" {
		if l, err := strconv.ParseInt(args["limit"], 10, 32); err == nil {
			limit = int32(l)
		}
	}
	if args["offset"] != "" {
		if o, err := strconv.ParseInt(args["offset"], 10, 32); err == nil {
			offset = int32(o)
		}
	}

	// Query messages from storage
	rows, err := storage.Query(messagesTable, nil, "chat_jid = ?", []string{chatJID}, "timestamp DESC", limit, offset)
	if err != nil {
		return "", fmt.Errorf("failed to query messages: %w", err)
	}

	type message struct {
		ID        string `json:"id"`
		SenderJID string `json:"sender_jid"`
		Content   string `json:"content"`
		Timestamp int64  `json:"timestamp"`
		FromMe    bool   `json:"from_me"`
	}

	type chatHistoryResponse struct {
		ChatJID     string    `json:"chat_jid"`
		ContactName string    `json:"contact_name"`
		Messages    []message `json:"messages"`
	}

	messages := make([]message, 0, len(rows))
	for _, row := range rows {
		ts, _ := strconv.ParseInt(row.Values["timestamp"], 10, 64)
		messages = append(messages, message{
			ID:        row.Values["id"],
			SenderJID: row.Values["sender_jid"],
			Content:   row.Values["content"],
			Timestamp: ts,
			FromMe:    row.Values["from_me"] == "1",
		})
	}

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	response := chatHistoryResponse{
		ChatJID:     chatJID,
		ContactName: getContactName(ctx, chatJID),
		Messages:    messages,
	}

	data, _ := json.Marshal(response)
	return string(data), nil
}

// getContactName looks up a contact name from a JID
func getContactName(ctx context.Context, jidStr string) string {
	if waClient == nil || waClient.Store == nil || waClient.Store.Contacts == nil {
		return ""
	}

	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return ""
	}

	contact, err := waClient.Store.Contacts.GetContact(ctx, jid)
	if err != nil {
		return ""
	}

	if contact.FullName != "" {
		return contact.FullName
	}
	if contact.PushName != "" {
		return contact.PushName
	}
	if contact.BusinessName != "" {
		return contact.BusinessName
	}
	return ""
}

func handleListChats(ctx context.Context, args map[string]string) (string, error) {
	limit := int32(20)
	if args["limit"] != "" {
		if l, err := strconv.ParseInt(args["limit"], 10, 32); err == nil {
			limit = int32(l)
		}
	}

	// Get distinct chat JIDs with their latest message
	// We'll query all messages and aggregate in Go since our storage API is simple
	rows, err := storage.Query(messagesTable, nil, "", nil, "timestamp DESC", 1000, 0)
	if err != nil {
		return "", fmt.Errorf("failed to query messages: %w", err)
	}

	type chatInfo struct {
		ChatJID       string `json:"chat_jid"`
		ContactName   string `json:"contact_name"`
		LastMessage   string `json:"last_message"`
		LastTimestamp int64  `json:"last_timestamp"`
		MessageCount  int    `json:"message_count"`
	}

	// Aggregate by chat_jid
	chatMap := make(map[string]*chatInfo)
	for _, row := range rows {
		jid := row.Values["chat_jid"]
		ts, _ := strconv.ParseInt(row.Values["timestamp"], 10, 64)

		if existing, ok := chatMap[jid]; ok {
			existing.MessageCount++
			// Keep the latest message (rows are already sorted DESC)
		} else {
			chatMap[jid] = &chatInfo{
				ChatJID:       jid,
				ContactName:   getContactName(ctx, jid),
				LastMessage:   row.Values["content"],
				LastTimestamp: ts,
				MessageCount:  1,
			}
		}
	}

	// Convert to slice and sort by last timestamp
	chats := make([]chatInfo, 0, len(chatMap))
	for _, chat := range chatMap {
		chats = append(chats, *chat)
	}

	// Sort by last timestamp descending
	for i := 0; i < len(chats)-1; i++ {
		for j := i + 1; j < len(chats); j++ {
			if chats[j].LastTimestamp > chats[i].LastTimestamp {
				chats[i], chats[j] = chats[j], chats[i]
			}
		}
	}

	// Apply limit
	if int32(len(chats)) > limit {
		chats = chats[:limit]
	}

	data, _ := json.Marshal(chats)
	return string(data), nil
}

func handleSearchContact(ctx context.Context, args map[string]string) (string, error) {
	query := strings.ToLower(args["query"])
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	if waClient == nil || waClient.Store == nil || waClient.Store.Contacts == nil {
		return "", fmt.Errorf("WhatsApp not fully initialized")
	}

	contacts, err := waClient.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get contacts: %w", err)
	}

	type contact struct {
		JID  string `json:"jid"`
		Name string `json:"name"`
	}

	var result []contact
	for jid, info := range contacts {
		name := info.FullName
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			name = info.BusinessName
		}

		// Case-insensitive partial match
		if strings.Contains(strings.ToLower(name), query) {
			result = append(result, contact{
				JID:  jid.String(),
				Name: name,
			})
		}
	}

	if len(result) == 0 {
		return "[]", nil
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}
