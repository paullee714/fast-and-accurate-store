package pubsub

import (
	"sync"
	"testing"
	"time"
)

func TestPubSub_SubscribePublish(t *testing.T) {
	ps := New()
	channel := "test_channel"
	msg := "hello"

	// Subscribe
	ch := ps.Subscribe(channel)

	// Publish in a goroutine to avoid blocking if channel was unbuffered (though it is buffered)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		count := ps.Publish(channel, msg)
		if count != 1 {
			t.Errorf("Publish() count = %d, want 1", count)
		}
	}()

	// Receive
	select {
	case received := <-ch:
		if received != msg {
			t.Errorf("Received %s, want %s", received, msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	wg.Wait()
}

func TestPubSub_MultipleSubscribers(t *testing.T) {
	ps := New()
	channel := "multi_channel"
	msg := "broadcast"

	sub1 := ps.Subscribe(channel)
	sub2 := ps.Subscribe(channel)

	count := ps.Publish(channel, msg)
	if count != 2 {
		t.Errorf("Publish() count = %d, want 2", count)
	}

	// Verify sub1
	select {
	case v := <-sub1:
		if v != msg {
			t.Errorf("Sub1 received %s, want %s", v, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Sub1 timeout")
	}

	// Verify sub2
	select {
	case v := <-sub2:
		if v != msg {
			t.Errorf("Sub2 received %s, want %s", v, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Sub2 timeout")
	}
}

func TestPubSub_NoSubscribers(t *testing.T) {
	ps := New()
	count := ps.Publish("empty_channel", "msg")
	if count != 0 {
		t.Errorf("Publish() count = %d, want 0", count)
	}
}
