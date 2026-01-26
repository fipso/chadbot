# Chadbot

<img height="400" alt="image" src="https://github.com/user-attachments/assets/fae03a15-c716-4655-9737-810b8b8359d1" />

A modular chat bot framework with plugin-based architecture, LLM integration, and multi-platform support.

## Features

- **Plugin Architecture**: Extensible via IPC plugins over Unix sockets
- **LLM Integration**: OpenAI and Anthropic support with function calling
- **Multi-Platform**: WhatsApp, MQTT, and custom platforms via plugins
- **Event System**: Pub/sub events with wildcard pattern matching
- **Persistent Storage**: Per-plugin namespaced SQLite storage
- **Configuration Management**: Plugin-specific config with live reload

## Installation

```bash
go build -o chadbot ./cmd/chadbot
```

## Usage

### Starting the Server

```bash
./chadbot [options]
```

**Options:**

| Flag | Default | Description |
|------|---------|-------------|
| `-socket` | `/var/run/chadbot.sock` | Unix socket path for plugin IPC |
| `-http` | `:8080` | HTTP/WebSocket listen address |
| `-db` | `chadbot.db` | SQLite database path |
| `-openai-key` | `$OPENAI_API_KEY` | OpenAI API key |
| `-anthropic-key` | `$ANTHROPIC_API_KEY` | Anthropic API key |
| `-llm` | `openai` | Default LLM provider (openai/anthropic) |

### Running Plugins

Plugins are separate executables that connect to the server:

```bash
# Set custom socket path (optional)
export CHADBOT_SOCKET=/tmp/chadbot.sock

# Run a plugin
./plugins/whatsapp/whatsapp
./plugins/mqtt/mqtt
```

## IPC Plugin Structure

Plugins communicate with the backend via **gRPC bidirectional streaming** over Unix sockets.

### Protocol Overview

```
Plugin                          Backend
  │                                │
  │──── RegisterRequest ──────────>│
  │<─── RegisterResponse ──────────│
  │                                │
  │──── SkillRegister ────────────>│
  │──── EventSubscribe ───────────>│
  │──── ConfigSchema ─────────────>│
  │                                │
  │<─── SkillInvoke ───────────────│
  │──── SkillResponse ────────────>│
  │                                │
  │<─── EventDispatch ─────────────│
  │──── EventEmit ────────────────>│
  │                                │
```

### Plugin Lifecycle

1. Connect to backend via gRPC stream
2. Send `RegisterRequest` with name, version, description
3. Receive `RegisterResponse` with assigned `plugin_id`
4. Register skills, subscribe to events, define config schema
5. Process incoming `SkillInvoke` and `EventDispatch` messages
6. Send responses and emit events as needed

### Message Types

#### Plugin → Backend

| Message | Purpose |
|---------|---------|
| `RegisterRequest` | Initial registration |
| `SkillRegister` | Register skills for LLM function calling |
| `EventSubscribe` | Subscribe to event patterns |
| `EventEmit` | Emit events to other plugins |
| `SkillResponse` | Return skill execution result |
| `StorageRequest` | Database operations (create/insert/update/delete/query) |
| `ConfigSchema` | Define plugin configuration fields |
| `ConfigGetRequest` | Get current config values |
| `ChatGetOrCreateRequest` | Get/create chat linked to messenger |
| `ChatAddMessageRequest` | Add message to chat history |
| `ChatLLMRequest` | Request LLM response |
| `ChatGetMessagesRequest` | Retrieve chat messages |

#### Backend → Plugin

| Message | Purpose |
|---------|---------|
| `RegisterResponse` | Registration confirmation + plugin_id |
| `SkillInvoke` | Request to execute a skill |
| `EventDispatch` | Subscribed event notification |
| `Error` | Error message |
| `StorageResponse` | Database operation result |
| `ConfigGetResponse` | Current config values |
| `ConfigChanged` | Config value changed notification |
| `ChatGetOrCreateResponse` | Chat ID |
| `ChatAddMessageResponse` | Message added confirmation |
| `ChatLLMResponse` | LLM response content |
| `ChatGetMessagesResponse` | Retrieved messages |

### Skill Definition

Skills are functions the LLM can call:

```go
client.RegisterSkill(sdk.Skill{
    Name:        "my_skill",
    Description: "What this skill does (sent to LLM)",
    Parameters: []sdk.SkillParameter{
        {
            Name:        "param1",
            Type:        "string",
            Description: "Parameter description",
            Required:    true,
        },
    },
}, func(ctx sdk.SkillContext, args map[string]string) (string, error) {
    // Handle skill invocation
    return "result", nil
})
```

### Event Patterns

Subscribe to events using wildcard patterns:

```go
client.Subscribe("chat.message.*")  // matches chat.message.received, chat.message.sent
client.Subscribe("mqtt.*")          // matches mqtt.message.received, mqtt.connected
client.Subscribe("*")               // matches all events
```

### Storage API

Each plugin has namespaced storage:

```go
// Create table
client.Storage().CreateTable("messages", map[string]string{
    "id":      "TEXT PRIMARY KEY",
    "content": "TEXT",
    "ts":      "INTEGER",
})

// Insert
client.Storage().Insert("messages", map[string]string{
    "id":      "123",
    "content": "hello",
    "ts":      "1234567890",
})

// Query
rows, _ := client.Storage().Query("messages", "ts > ?", "1234567800")

// Update
client.Storage().Update("messages", "id = ?", map[string]string{
    "content": "updated",
}, "123")

// Delete
client.Storage().Delete("messages", "id = ?", "123")
```

### Configuration

Define plugin config schema:

```go
client.SetConfigSchema([]sdk.ConfigField{
    {
        Key:          "broker_url",
        Label:        "Broker URL",
        Description:  "MQTT broker address",
        Type:         sdk.ConfigFieldTypeString,
        DefaultValue: "tcp://localhost:1883",
    },
    {
        Key:   "enabled",
        Label: "Enabled",
        Type:  sdk.ConfigFieldTypeBool,
    },
})

// Get config
config, _ := client.GetConfig()
brokerURL := config["broker_url"]

// React to changes
client.OnConfigChanged(func(key, value string, allConfig map[string]string) {
    // Handle config change
})
```

## Writing a Plugin

Minimal plugin example:

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"

    "chadbot/pkg/sdk"
)

func main() {
    client, err := sdk.NewClient(sdk.ClientConfig{
        Name:        "my-plugin",
        Version:     "1.0.0",
        Description: "My custom plugin",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Register a skill
    client.RegisterSkill(sdk.Skill{
        Name:        "hello",
        Description: "Says hello to someone",
        Parameters: []sdk.SkillParameter{
            {Name: "name", Type: "string", Description: "Name to greet", Required: true},
        },
    }, func(ctx sdk.SkillContext, args map[string]string) (string, error) {
        return "Hello, " + args["name"] + "!", nil
    })

    // Subscribe to events
    client.Subscribe("chat.message.*")
    client.OnEvent(func(eventType string, data map[string]string) {
        log.Printf("Event: %s, Data: %v", eventType, data)
    })

    // Wait for shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
}
```

## Included Plugins

### WhatsApp (`plugins/whatsapp`)

WhatsApp messaging via whatsmeow library.

**Skills:**
- `whatsapp_send_message` - Send message to contact/group
- `whatsapp_list_contacts` - List all contacts
- `whatsapp_list_groups` - List joined groups
- `whatsapp_get_chat_history` - Get message history
- `whatsapp_search_contact` - Search contacts by name

### MQTT (`plugins/mqtt`)

MQTT broker integration.

**Skills:**
- `mqtt_publish` - Publish to topic
- `mqtt_subscribe` - Subscribe to topic pattern
- `mqtt_unsubscribe` - Unsubscribe from topic
- `mqtt_list_subscriptions` - List active subscriptions
- `mqtt_status` - Connection status
- `mqtt_get_messages` - Get recent messages from topic

**Events emitted:**
- `mqtt.message.received` - When message arrives on subscribed topic

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Backend                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
│  │  gRPC    │  │  Plugin  │  │  Event   │  │   LLM    │     │
│  │  Server  │──│  Manager │──│   Bus    │──│  Router  │     │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │
│       │              │                           │          │
│       │        ┌──────────┐               ┌──────────┐      │
│       │        │  Skill   │               │  Chat    │      │
│       │        │ Registry │               │ Service  │      │
│       │        └──────────┘               └──────────┘      │
│       │              │                           │          │
│  ┌────────────────────────────────────────────────────┐     │
│  │                    SQLite Storage                  │     │
│  └────────────────────────────────────────────────────┘     │
└───────────────────────────┬─────────────────────────────────┘
                            │ Unix Socket
        ┌───────────────────┼───────────────────┐
        │                   │                   │
   ┌────────┐          ┌────────┐          ┌────────┐
   │WhatsApp│          │  MQTT  │          │ Custom │
   │ Plugin │          │ Plugin │          │ Plugin │
   └────────┘          └────────┘          └────────┘
```

## License

MIT
