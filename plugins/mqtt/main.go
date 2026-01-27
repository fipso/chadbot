package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
)

//go:embed PLUGIN.md
var pluginDocumentation string

const (
	subscriptionsTable = "subscriptions"
	messagesTable      = "messages"
)

var (
	client       *sdk.Client
	mqttClient   mqtt.Client
	storage      *sdk.StorageClient
	mu           sync.RWMutex
	connected    bool
	activeTopics = make(map[string]byte) // topic -> QoS
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[MQTT] Shutting down...")
		cancel()
	}()

	// Initialize SDK client
	socketPath := os.Getenv("CHADBOT_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/chadbot.sock"
	}
	client = sdk.NewClient("mqtt", "1.0.0", "MQTT broker integration - subscribe to topics and receive messages as events")
	client = client.WithSocket(socketPath)
	client = client.SetDocumentation(pluginDocumentation)

	// Register skills
	registerSkills()

	// Connect to chadbot backend
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("[MQTT] Failed to connect to backend: %v", err)
	}
	defer client.Close()

	// Register plugin config
	if err := registerConfig(); err != nil {
		log.Printf("[MQTT] Failed to register config: %v", err)
	}

	// Handle config changes
	client.OnConfigChanged(handleConfigChanged)

	// Initialize storage
	storage = client.Storage()
	if err := initStorage(); err != nil {
		log.Fatalf("[MQTT] Failed to initialize storage: %v", err)
	}

	// Try to connect to MQTT broker with current config
	connectMQTT()

	log.Println("[MQTT] Plugin started")

	// Run the SDK client event loop
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[MQTT] Client error: %v", err)
	}

	// Cleanup
	if mqttClient != nil && mqttClient.IsConnected() {
		mqttClient.Disconnect(1000)
	}
}

func registerConfig() error {
	return client.RegisterConfig([]sdk.ConfigField{
		{
			Key:          "broker_url",
			Label:        "Broker URL",
			Description:  "MQTT broker URL (e.g., tcp://localhost:1883 or ssl://broker.example.com:8883)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING,
			DefaultValue: "",
		},
		{
			Key:          "username",
			Label:        "Username",
			Description:  "MQTT broker username (optional)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING,
			DefaultValue: "",
		},
		{
			Key:          "password",
			Label:        "Password",
			Description:  "MQTT broker password (optional)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING,
			DefaultValue: "",
		},
		{
			Key:          "client_id",
			Label:        "Client ID",
			Description:  "MQTT client ID (leave empty for auto-generated)",
			Type:         pb.ConfigFieldType_CONFIG_FIELD_TYPE_STRING,
			DefaultValue: "",
		},
	})
}

func registerSkills() {
	// Publish a message
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_publish",
		Description: "Publish a message to an MQTT topic",
		Parameters: []*pb.SkillParameter{
			{Name: "topic", Type: "string", Description: "The topic to publish to", Required: true},
			{Name: "payload", Type: "string", Description: "The message payload", Required: true},
			{Name: "qos", Type: "number", Description: "Quality of Service (0, 1, or 2). Default: 0", Required: false},
			{Name: "retain", Type: "boolean", Description: "Retain the message on the broker. Default: false", Required: false},
		},
	}, handlePublish)

	// Subscribe to a topic
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_subscribe",
		Description: "Subscribe to an MQTT topic to receive messages",
		Parameters: []*pb.SkillParameter{
			{Name: "topic", Type: "string", Description: "The topic pattern to subscribe to (supports + and # wildcards)", Required: true},
			{Name: "qos", Type: "number", Description: "Quality of Service (0, 1, or 2). Default: 0", Required: false},
		},
	}, handleSubscribe)

	// Unsubscribe from a topic
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_unsubscribe",
		Description: "Unsubscribe from an MQTT topic",
		Parameters: []*pb.SkillParameter{
			{Name: "topic", Type: "string", Description: "The topic to unsubscribe from", Required: true},
		},
	}, handleUnsubscribe)

	// List subscriptions
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_list_subscriptions",
		Description: "List all active MQTT topic subscriptions",
		Parameters:  []*pb.SkillParameter{},
	}, handleListSubscriptions)

	// Get connection status
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_status",
		Description: "Get MQTT connection status",
		Parameters:  []*pb.SkillParameter{},
	}, handleStatus)

	// Reconnect
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_reconnect",
		Description: "Reconnect to the MQTT broker",
		Parameters:  []*pb.SkillParameter{},
	}, handleReconnect)

	// Get recent messages
	client.RegisterSkill(&pb.Skill{
		Name:        "mqtt_get_messages",
		Description: "Get the last N messages received on a topic",
		Parameters: []*pb.SkillParameter{
			{Name: "topic", Type: "string", Description: "The topic to get messages for (exact match)", Required: true},
			{Name: "limit", Type: "number", Description: "Maximum number of messages to return (default: 10)", Required: false},
		},
	}, handleGetMessages)
}

func initStorage() error {
	log.Println("[MQTT] Initializing storage...")

	// Create subscriptions table
	if err := storage.CreateTable(subscriptionsTable, []*pb.ColumnDef{
		{Name: "topic", Type: "TEXT", PrimaryKey: true},
		{Name: "qos", Type: "INTEGER", NotNull: true},
		{Name: "created_at", Type: "INTEGER", NotNull: true},
	}, true); err != nil {
		return err
	}

	// Create messages table
	if err := storage.CreateTable(messagesTable, []*pb.ColumnDef{
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "topic", Type: "TEXT", NotNull: true},
		{Name: "payload", Type: "TEXT", NotNull: true},
		{Name: "qos", Type: "INTEGER", NotNull: true},
		{Name: "retained", Type: "INTEGER", NotNull: true},
		{Name: "timestamp", Type: "INTEGER", NotNull: true},
	}, true); err != nil {
		return err
	}

	// Check existing subscriptions
	rows, err := storage.Query(subscriptionsTable, nil, "", nil, "", 1000, 0)
	if err != nil {
		log.Printf("[MQTT] Failed to query existing subscriptions: %v", err)
	} else {
		log.Printf("[MQTT] Found %d existing subscriptions in storage", len(rows))
	}

	return nil
}

func handleConfigChanged(key, value string, allValues map[string]string) {
	log.Printf("[MQTT] Config changed: %s", key)

	// Reconnect if connection-related config changed
	if key == "broker_url" || key == "username" || key == "password" || key == "client_id" {
		log.Println("[MQTT] Connection config changed, reconnecting...")
		connectMQTT()
	}
}

func connectMQTT() {
	brokerURL := client.GetConfig("broker_url")
	if brokerURL == "" {
		log.Println("[MQTT] No broker URL configured, skipping connection")
		return
	}

	// Disconnect existing client
	if mqttClient != nil && mqttClient.IsConnected() {
		mqttClient.Disconnect(500)
	}

	clientID := client.GetConfig("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("chadbot-mqtt-%d", time.Now().UnixNano())
	}

	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(onConnect).
		SetConnectionLostHandler(onConnectionLost).
		SetReconnectingHandler(onReconnecting)

	// Add auth if configured
	username := client.GetConfig("username")
	password := client.GetConfig("password")
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	mqttClient = mqtt.NewClient(opts)

	log.Printf("[MQTT] Connecting to %s...", brokerURL)
	token := mqttClient.Connect()
	if token.WaitTimeout(10 * time.Second) {
		if token.Error() != nil {
			log.Printf("[MQTT] Connection failed: %v", token.Error())
		}
	} else {
		log.Println("[MQTT] Connection timeout, will retry in background")
	}
}

func onConnect(c mqtt.Client) {
	mu.Lock()
	connected = true
	mu.Unlock()

	log.Println("[MQTT] Connected to broker")

	// Resubscribe to stored topics
	resubscribeAll()
}

func onConnectionLost(c mqtt.Client, err error) {
	mu.Lock()
	connected = false
	mu.Unlock()
	log.Printf("[MQTT] Connection lost: %v", err)
}

func onReconnecting(c mqtt.Client, opts *mqtt.ClientOptions) {
	log.Println("[MQTT] Reconnecting...")
}

func resubscribeAll() {
	rows, err := storage.Query(subscriptionsTable, nil, "", nil, "", 1000, 0)
	if err != nil {
		log.Printf("[MQTT] Failed to load subscriptions: %v", err)
		return
	}

	log.Printf("[MQTT] Resubscribing to %d stored topics", len(rows))

	for _, row := range rows {
		topic := row.Values["topic"]
		qos := byte(0)
		if q, ok := row.Values["qos"]; ok && q == "1" {
			qos = 1
		} else if q == "2" {
			qos = 2
		}
		log.Printf("[MQTT] Restoring subscription: %s (QoS %d)", topic, qos)
		subscribeToTopic(topic, qos)
	}
}

func subscribeToTopic(topic string, qos byte) {
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Printf("[MQTT] Cannot subscribe to %s: not connected", topic)
		return
	}

	token := mqttClient.Subscribe(topic, qos, messageHandler)
	if token.WaitTimeout(5 * time.Second) {
		if token.Error() != nil {
			log.Printf("[MQTT] Failed to subscribe to %s: %v", topic, token.Error())
			return
		}
	} else {
		log.Printf("[MQTT] Subscribe timeout for %s", topic)
		return
	}

	mu.Lock()
	activeTopics[topic] = qos
	mu.Unlock()

	log.Printf("[MQTT] Subscribed to %s (QoS %d)", topic, qos)
}

func messageHandler(c mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())
	now := time.Now().Unix()

	log.Printf("[MQTT] Received message on %s: %s", topic, truncate(payload, 100))

	// Store message in database
	retained := "0"
	if msg.Retained() {
		retained = "1"
	}
	err := storage.Insert(messagesTable, map[string]string{
		"topic":     topic,
		"payload":   payload,
		"qos":       fmt.Sprintf("%d", msg.Qos()),
		"retained":  retained,
		"timestamp": fmt.Sprintf("%d", now),
	})
	if err != nil {
		log.Printf("[MQTT] Failed to store message: %v", err)
	}

	// Build payload struct
	payloadStruct, err := structpb.NewStruct(map[string]interface{}{
		"topic":      topic,
		"payload":    payload,
		"qos":        int(msg.Qos()),
		"retained":   msg.Retained(),
		"message_id": int(msg.MessageID()),
	})
	if err != nil {
		log.Printf("[MQTT] Failed to create payload struct: %v", err)
		return
	}

	// Emit event
	err = client.Emit(&pb.Event{
		EventType:    "mqtt.message.received",
		SourcePlugin: "mqtt",
		Timestamp:    timestamppb.Now(),
		Data: &pb.Event_Generic{
			Generic: &pb.GenericEvent{
				EventName: "message_received",
				Payload:   payloadStruct,
			},
		},
	})
	if err != nil {
		log.Printf("[MQTT] Failed to emit event: %v", err)
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// Skill handlers

func handlePublish(ctx context.Context, args map[string]string) (string, error) {
	if mqttClient == nil || !mqttClient.IsConnected() {
		return "", fmt.Errorf("not connected to MQTT broker")
	}

	topic := args["topic"]
	payload := args["payload"]
	if topic == "" || payload == "" {
		return "", fmt.Errorf("topic and payload are required")
	}

	qos := byte(0)
	if q, ok := args["qos"]; ok {
		switch q {
		case "1":
			qos = 1
		case "2":
			qos = 2
		}
	}

	retain := args["retain"] == "true"

	token := mqttClient.Publish(topic, qos, retain, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return "", fmt.Errorf("publish timeout")
	}
	if token.Error() != nil {
		return "", fmt.Errorf("publish failed: %w", token.Error())
	}

	return fmt.Sprintf("Published to %s (QoS %d, retain: %t)", topic, qos, retain), nil
}

func handleSubscribe(ctx context.Context, args map[string]string) (string, error) {
	topic := args["topic"]
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}

	qos := byte(0)
	if q, ok := args["qos"]; ok {
		switch q {
		case "1":
			qos = 1
		case "2":
			qos = 2
		}
	}

	// Store in DB
	err := storage.Insert(subscriptionsTable, map[string]string{
		"topic":      topic,
		"qos":        fmt.Sprintf("%d", qos),
		"created_at": fmt.Sprintf("%d", time.Now().Unix()),
	})
	if err != nil {
		// May already exist, try update
		updateErr := storage.Update(subscriptionsTable, map[string]string{
			"qos": fmt.Sprintf("%d", qos),
		}, "topic = ?", topic)
		if updateErr != nil {
			log.Printf("[MQTT] Failed to store subscription: insert=%v, update=%v", err, updateErr)
		}
	}
	log.Printf("[MQTT] Stored subscription: %s", topic)

	// Subscribe if connected
	if mqttClient != nil && mqttClient.IsConnected() {
		subscribeToTopic(topic, qos)
	}

	return fmt.Sprintf("Subscribed to %s (QoS %d)", topic, qos), nil
}

func handleUnsubscribe(ctx context.Context, args map[string]string) (string, error) {
	topic := args["topic"]
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}

	// Remove from DB
	if err := storage.Delete(subscriptionsTable, "topic = ?", topic); err != nil {
		log.Printf("[MQTT] Failed to delete subscription: %v", err)
	}

	// Unsubscribe if connected
	if mqttClient != nil && mqttClient.IsConnected() {
		token := mqttClient.Unsubscribe(topic)
		if token.WaitTimeout(5 * time.Second) {
			if token.Error() != nil {
				return "", fmt.Errorf("unsubscribe failed: %w", token.Error())
			}
		}
	}

	mu.Lock()
	delete(activeTopics, topic)
	mu.Unlock()

	return fmt.Sprintf("Unsubscribed from %s", topic), nil
}

func handleListSubscriptions(ctx context.Context, args map[string]string) (string, error) {
	rows, err := storage.Query(subscriptionsTable, nil, "", nil, "created_at DESC", 100, 0)
	if err != nil {
		return "", fmt.Errorf("failed to query subscriptions: %w", err)
	}

	type subscription struct {
		Topic     string `json:"topic"`
		QoS       int    `json:"qos"`
		CreatedAt int64  `json:"created_at"`
		Active    bool   `json:"active"`
	}

	subs := make([]subscription, 0, len(rows))
	for _, row := range rows {
		topic := row.Values["topic"]
		qos := 0
		if row.Values["qos"] == "1" {
			qos = 1
		} else if row.Values["qos"] == "2" {
			qos = 2
		}
		createdAt := int64(0)
		fmt.Sscanf(row.Values["created_at"], "%d", &createdAt)

		mu.RLock()
		_, active := activeTopics[topic]
		mu.RUnlock()

		subs = append(subs, subscription{
			Topic:     topic,
			QoS:       qos,
			CreatedAt: createdAt,
			Active:    active,
		})
	}

	data, _ := json.Marshal(subs)
	return string(data), nil
}

func handleStatus(ctx context.Context, args map[string]string) (string, error) {
	mu.RLock()
	isConnected := connected
	topics := make([]string, 0, len(activeTopics))
	for t := range activeTopics {
		topics = append(topics, t)
	}
	mu.RUnlock()

	status := map[string]interface{}{
		"connected":     isConnected,
		"broker_url":    client.GetConfig("broker_url"),
		"client_id":     client.GetConfig("client_id"),
		"active_topics": topics,
	}

	data, _ := json.Marshal(status)
	return string(data), nil
}

func handleReconnect(ctx context.Context, args map[string]string) (string, error) {
	connectMQTT()
	return "Reconnection initiated", nil
}

func handleGetMessages(ctx context.Context, args map[string]string) (string, error) {
	topic := args["topic"]
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}

	limit := int32(10)
	if l, ok := args["limit"]; ok && l != "" {
		var parsed int
		if _, err := fmt.Sscanf(l, "%d", &parsed); err == nil && parsed > 0 {
			limit = int32(parsed)
		}
	}

	rows, err := storage.Query(messagesTable, nil, "topic = ?", []string{topic}, "timestamp DESC", limit, 0)
	if err != nil {
		return "", fmt.Errorf("failed to query messages: %w", err)
	}

	// If no messages found, help the LLM by showing available topics
	if len(rows) == 0 {
		mu.RLock()
		topics := make([]string, 0, len(activeTopics))
		for t := range activeTopics {
			topics = append(topics, t)
		}
		mu.RUnlock()

		return fmt.Sprintf(`{"error": "no messages found for topic '%s'", "hint": "check topic name - active topics are: %v"}`, topic, topics), nil
	}

	type message struct {
		Topic     string `json:"topic"`
		Payload   string `json:"payload"`
		QoS       int    `json:"qos"`
		Retained  bool   `json:"retained"`
		Timestamp int64  `json:"timestamp"`
	}

	messages := make([]message, 0, len(rows))
	for _, row := range rows {
		qos := 0
		fmt.Sscanf(row.Values["qos"], "%d", &qos)
		var ts int64
		fmt.Sscanf(row.Values["timestamp"], "%d", &ts)

		messages = append(messages, message{
			Topic:     row.Values["topic"],
			Payload:   row.Values["payload"],
			QoS:       qos,
			Retained:  row.Values["retained"] == "1",
			Timestamp: ts,
		})
	}

	data, _ := json.Marshal(messages)
	return string(data), nil
}
