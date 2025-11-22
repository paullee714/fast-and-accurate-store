package store_test

import (
	"testing"
	"time"

	"fas/pkg/store"
)

func TestStore_SetGet(t *testing.T) {
	t.Log("Starting TestStore_SetGet")
	s := store.New()

	key := "test_key"
	value := "test_value"

	t.Logf("Step 1: Setting key '%s' with value '%s'", key, value)
	s.Set(key, value, 0)

	t.Logf("Step 2: Getting key '%s'", key)
	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}
	t.Log("TestStore_SetGet Passed")
}

func TestStore_Expiration(t *testing.T) {
	t.Log("Starting TestStore_Expiration")
	s := store.New()

	key := "expire_key"
	value := "expire_value"
	ttl := 10 * time.Millisecond

	t.Logf("Step 1: Setting key '%s' with TTL %v", key, ttl)
	s.Set(key, value, ttl)

	// Verify it exists initially
	t.Log("Step 2: Verifying key exists immediately")
	_, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get() before expiration error = %v", err)
	}

	// Wait for expiration
	t.Logf("Step 3: Waiting %v for expiration", 20*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	// Verify it's gone
	t.Log("Step 4: Verifying key is gone")
	_, err = s.Get(key)
	if err != store.ErrNotFound {
		t.Errorf("Get() after expiration error = %v, want %v", err, store.ErrNotFound)
	}
	t.Log("TestStore_Expiration Passed")
}

func TestStore_TypeSafety(t *testing.T) {
	t.Log("Starting TestStore_TypeSafety")
	s := store.New()
	key := "string_key"

	t.Log("Step 1: Setting string value")
	s.Set(key, "value", 0)

	// Verify standard behavior is type safe for strings
	t.Log("Step 2: Getting string value")
	val, err := s.Get(key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if val != "value" {
		t.Errorf("Get() = %v, want %v", val, "value")
	}
	t.Log("TestStore_TypeSafety Passed")
}

func TestStore_NotFound(t *testing.T) {
	t.Log("Starting TestStore_NotFound")
	s := store.New()
	t.Log("Step 1: Getting non-existent key")
	_, err := s.Get("non_existent")
	if err != store.ErrNotFound {
		t.Errorf("Get() error = %v, want %v", err, store.ErrNotFound)
	}
	t.Log("TestStore_NotFound Passed")
}
