package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
)

func main() {
	client := sdk.NewClient("example", "1.0.0", "Example plugin demonstrating skills and events")

	// Register a weather skill
	client.RegisterSkill(&pb.Skill{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: []*pb.SkillParameter{
			{
				Name:        "location",
				Type:        "string",
				Description: "City name or location",
				Required:    true,
			},
		},
	}, func(ctx context.Context, args map[string]string) (string, error) {
		location := args["location"]
		if location == "" {
			return "", fmt.Errorf("location is required")
		}
		// Simulated weather response
		return fmt.Sprintf("Weather in %s: 22°C, Sunny", location), nil
	})

	// Register a reminder skill
	client.RegisterSkill(&pb.Skill{
		Name:        "set_reminder",
		Description: "Set a reminder for a specific time",
		Parameters: []*pb.SkillParameter{
			{
				Name:        "message",
				Type:        "string",
				Description: "Reminder message",
				Required:    true,
			},
			{
				Name:        "time",
				Type:        "string",
				Description: "Time for the reminder (e.g., '5m', '1h', '3pm')",
				Required:    true,
			},
		},
	}, func(ctx context.Context, args map[string]string) (string, error) {
		message := args["message"]
		time := args["time"]
		return fmt.Sprintf("Reminder set for %s: %s", time, message), nil
	})

	// Handle events
	client.OnEvent(func(event *pb.Event) {
		log.Printf("[Example] Received event: %s from %s", event.EventType, event.SourcePlugin)
	})

	// Connect to backend
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Subscribe to chat events
	if err := client.Subscribe([]string{"chat.message.*"}); err != nil {
		log.Printf("Failed to subscribe: %v", err)
	}

	log.Println("[Example] Plugin running, press Ctrl+C to exit")

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := client.Run(ctx); err != nil {
			log.Printf("[Example] Run error: %v", err)
			cancel()
		}
	}()

	<-sigChan
	log.Println("[Example] Shutting down...")
}
