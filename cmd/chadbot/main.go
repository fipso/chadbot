package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/fipso/chadbot/internal/server"
)

func main() {
	socket := flag.String("socket", server.DefaultSocket, "Unix socket path for plugin IPC")
	httpAddr := flag.String("http", ":8080", "HTTP/WebSocket listen address")
	dbPath := flag.String("db", "chadbot.db", "SQLite database path")
	openaiKey := flag.String("openai-key", "", "OpenAI API key (or set OPENAI_API_KEY)")
	anthropicKey := flag.String("anthropic-key", "", "Anthropic API key (or set ANTHROPIC_API_KEY)")
	defaultLLM := flag.String("llm", "", "Default LLM provider (openai or anthropic)")
	flag.Parse()

	// Use /tmp for development if /var/run is not writable
	if *socket == server.DefaultSocket {
		if _, err := os.Stat("/var/run"); os.IsPermission(err) {
			*socket = "/tmp/chadbot.sock"
			log.Printf("[Main] Using %s (no write permission to /var/run)", *socket)
		}
	}

	config := &server.Config{
		Socket:       *socket,
		HTTPAddr:     *httpAddr,
		DBPath:       *dbPath,
		OpenAIKey:    *openaiKey,
		AnthropicKey: *anthropicKey,
		DefaultLLM:   *defaultLLM,
	}

	srv := server.New(config)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
