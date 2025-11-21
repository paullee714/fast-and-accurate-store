package pubsub

import (
	"sync"
)

// PubSub manages the publish/subscribe messaging system.
type PubSub struct {
	mu       sync.RWMutex
	channels map[string][]chan string
}

// New creates and returns a new PubSub instance.
func New() *PubSub {
	return &PubSub{
		channels: make(map[string][]chan string),
	}
}

// Subscribe subscribes to a channel and returns a Go channel to receive messages.
// The returned channel has a buffer size of 10.
func (ps *PubSub) Subscribe(channel string) chan string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan string, 10) // Buffer size 10
	ps.channels[channel] = append(ps.channels[channel], ch)
	return ch
}

// Publish sends a message to all subscribers of the given channel.
// It returns the number of subscribers that received the message.
// Note: This uses a non-blocking send, so if a subscriber is slow, they might miss messages.
func (ps *PubSub) Publish(channel string, message string) int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	subscribers, exists := ps.channels[channel]
	if !exists {
		return 0
	}

	count := 0
	for _, ch := range subscribers {
		// Non-blocking send to avoid stalling if a subscriber is slow
		select {
		case ch <- message:
			count++
		default:
			// Drop message if channel is full (or handle differently based on requirements)
		}
	}
	return count
}
