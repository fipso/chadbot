package event

import (
	"log"
	"strings"
	"sync"

	pb "github.com/fipso/chadbot/gen/chadbot"
)

// Handler is a function that handles events
type Handler func(event *pb.Event)

// Subscription represents an event subscription
type Subscription struct {
	ID       string
	Patterns []string
	Handler  Handler
}

// Bus manages event routing between plugins
type Bus struct {
	mu            sync.RWMutex
	subscriptions map[string]*Subscription
	nextID        int
}

// NewBus creates a new event bus
func NewBus() *Bus {
	return &Bus{
		subscriptions: make(map[string]*Subscription),
	}
}

// Subscribe registers a handler for events matching the given patterns
func (b *Bus) Subscribe(patterns []string, handler Handler) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := strings.Join(patterns, ",") + string(rune(b.nextID))

	b.subscriptions[id] = &Subscription{
		ID:       id,
		Patterns: patterns,
		Handler:  handler,
	}

	log.Printf("[EventBus] New subscription: %s for patterns: %v", id, patterns)
	return id
}

// Unsubscribe removes a subscription
func (b *Bus) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscriptions, id)
	log.Printf("[EventBus] Removed subscription: %s", id)
}

// Publish sends an event to all matching subscribers
func (b *Bus) Publish(event *pb.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscriptions {
		if b.matches(event.EventType, sub.Patterns) {
			go sub.Handler(event)
		}
	}
}

// matches checks if an event type matches any of the patterns
func (b *Bus) matches(eventType string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, eventType) {
			return true
		}
	}
	return false
}

// matchPattern checks if an event type matches a pattern
// Supports wildcards: "chat.*" matches "chat.message", "chat.typing"
// "chat.message.*" matches "chat.message.received", "chat.message.sent"
func matchPattern(pattern, eventType string) bool {
	if pattern == "*" {
		return true
	}

	patternParts := strings.Split(pattern, ".")
	eventParts := strings.Split(eventType, ".")

	for i, pp := range patternParts {
		if pp == "*" {
			// Wildcard matches remaining parts
			return true
		}
		if i >= len(eventParts) {
			return false
		}
		if pp != eventParts[i] {
			return false
		}
	}

	return len(patternParts) == len(eventParts)
}
