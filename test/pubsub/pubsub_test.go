package pubsub_test

import (
	"sync"
	"testing"
	"time"

	"fas/pkg/pubsub"
)

func TestPubSub_SubscribePublish(t *testing.T) {
	t.Log("Starting TestPubSub_SubscribePublish")
	ps := pubsub.New()
	channel := "test_channel"
	msg := "hello"

	// Subscribe
	t.Logf("Step 1: Subscribing to channel '%s'", channel)
	ch := ps.Subscribe(channel)

	// Publish in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.Logf("Step 2: Publishing message '%s' to channel '%s'", msg, channel)
		count := ps.Publish(channel, msg)
		if count != 1 {
			t.Errorf("Publish() count = %d, want 1", count)
		}
	}()

	// Receive
	t.Log("Step 3: Waiting for message")
	select {
	case received := <-ch:
		t.Logf("Step 4: Received message '%s'", received)
		if received != msg {
			t.Errorf("Received %s, want %s", received, msg)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	wg.Wait()
	t.Log("TestPubSub_SubscribePublish Passed")
}

func TestPubSub_MultipleSubscribers(t *testing.T) {
	t.Log("Starting TestPubSub_MultipleSubscribers")
	ps := pubsub.New()
	channel := "multi_channel"
	msg := "broadcast"

	t.Log("Step 1: Registering 2 subscribers")
	sub1 := ps.Subscribe(channel)
	sub2 := ps.Subscribe(channel)

	t.Log("Step 2: Publishing message")
	count := ps.Publish(channel, msg)
	if count != 2 {
		t.Errorf("Publish() count = %d, want 2", count)
	}

	// Verify sub1
	t.Log("Step 3: Verifying subscriber 1")
	select {
	case v := <-sub1:
		if v != msg {
			t.Errorf("Sub1 received %s, want %s", v, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Sub1 timeout")
	}

	// Verify sub2
	t.Log("Step 4: Verifying subscriber 2")
	select {
	case v := <-sub2:
		if v != msg {
			t.Errorf("Sub2 received %s, want %s", v, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Sub2 timeout")
	}
	t.Log("TestPubSub_MultipleSubscribers Passed")
}

func TestPubSub_NoSubscribers(t *testing.T) {
	t.Log("Starting TestPubSub_NoSubscribers")
	ps := pubsub.New()
	t.Log("Step 1: Publishing to empty channel")
	count := ps.Publish("empty_channel", "msg")
	if count != 0 {
		t.Errorf("Publish() count = %d, want 0", count)
	}
	t.Log("TestPubSub_NoSubscribers Passed")
}

func TestPubSub_Unsubscribe(t *testing.T) {
	t.Log("Starting TestPubSub_Unsubscribe")
	ps := pubsub.New()
	channel := "unsub_channel"

	t.Log("Step 1: Subscribing")
	ch := ps.Subscribe(channel)

	t.Log("Step 2: Unsubscribing")
	ps.Unsubscribe(channel, ch)

	t.Log("Step 3: Verifying channel closed")
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for channel close")
	}

	t.Log("Step 4: Verifying publish count is 0")
	count := ps.Publish(channel, "msg")
	if count != 0 {
		t.Errorf("Publish() count = %d, want 0", count)
	}
	t.Log("TestPubSub_Unsubscribe Passed")
}

func TestPubSub_SlowSubscriber(t *testing.T) {
	t.Log("Starting TestPubSub_SlowSubscriber")
	ps := pubsub.New()
	channel := "slow_channel"

	t.Log("Step 1: Subscribing")
	_ = ps.Subscribe(channel)

	// Fill buffer (buffer size is 100)
	t.Log("Step 2: Filling buffer")
	for i := 0; i < 100; i++ {
		ps.Publish(channel, "msg")
	}

	// Publish one more, should be dropped
	t.Log("Step 3: Publishing one more message (should be dropped)")
	count := ps.Publish(channel, "dropped_msg")
	if count != 0 {
		t.Errorf("Publish() count = %d, want 0 (dropped)", count)
	}

	t.Log("TestPubSub_SlowSubscriber Passed")
}
