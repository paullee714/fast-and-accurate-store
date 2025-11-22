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
// The returned channel has a buffer size of 100.
func (ps *PubSub) Subscribe(channel string) <-chan string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan string, 100) // Increased buffer size
	ps.channels[channel] = append(ps.channels[channel], ch)
	return ch
}

// Unsubscribe removes a subscriber from a channel.
func (ps *PubSub) Unsubscribe(channel string, ch <-chan string) {
	ps.mu.Lock()

	subscribers, exists := ps.channels[channel]
	if !exists {
		ps.mu.Unlock()
		return
	}

	var channelToClose chan string

	// Find and remove the subscriber
	for i, sub := range subscribers {
		if sub == ch {
			channelToClose = sub
			// Remove from list
			ps.channels[channel] = append(subscribers[:i], subscribers[i+1:]...)
			break
		}
	}

	// Clean up empty channel list
	if len(ps.channels[channel]) == 0 {
		delete(ps.channels, channel)
	}

	ps.mu.Unlock()

	// Close the channel after releasing the lock and removing it from the list.
	// This ensures Publish (which holds RLock) won't find this channel in the list,
	// avoiding sending to a closed channel.
	if channelToClose != nil {
		close(channelToClose)
	}
}

// Publish sends a message to all subscribers of the given channel.
// It returns the number of subscribers that received the message.
// Note: This uses a non-blocking send. If a subscriber is slow (buffer full), the message is dropped.
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
			// Drop message if channel is full (Backpressure: Drop)
		}
	}
	return count
}
