package store_test

import (
	"testing"
	"time"

	"fas/pkg/store"
)

func TestStore_SetGet(t *testing.T) {
	t.Log("Starting TestStore_SetGet")
	s := store.New(1024*1024, store.EvictionNoEviction)

	key := "test_key"
	value := "test_value"

	t.Logf("Step 1: Setting key '%s' with value '%s'", key, value)
	s.Set(key, value, 0)

	t.Logf("Step 2: Getting key '%s'", key)
	got, err := s.Get(key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}
	t.Log("TestStore_SetGet Passed")
}

func TestStore_Expiration(t *testing.T) {
	t.Log("Starting TestStore_Expiration")
	s := store.New(1024*1024, store.EvictionNoEviction)

	key := "expire_key"
	value := "expire_value"
	ttl := 10 * time.Millisecond

	t.Logf("Step 1: Setting key '%s' with TTL %v", key, ttl)
	s.Set(key, value, ttl)

	// Verify it exists initially
	t.Log("Step 2: Verifying key exists immediately")
	_, err := s.Get(key)
	if err != nil {
		t.Errorf("Get() error before expiration = %v", err)
	}

	// Wait for expiration
	t.Log("Step 3: Waiting 20ms for expiration")
	time.Sleep(20 * time.Millisecond)

	// Verify it's gone
	t.Log("Step 4: Verifying key is gone")
	_, err = s.Get(key)
	if err != store.ErrNotFound {
		t.Errorf("Get() error after expiration = %v, want %v", err, store.ErrNotFound)
	}
	t.Log("TestStore_Expiration Passed")
}

func TestStore_TypeSafety(t *testing.T) {
	t.Log("Starting TestStore_TypeSafety")
	s := store.New(1024*1024, store.EvictionNoEviction)
	key := "string_key"
	value := "string_value"

	t.Log("Step 1: Setting string value")
	s.Set(key, value, 0)

	// Verify standard behavior is type safe for strings
	t.Log("Step 2: Getting string value")
	got, err := s.Get(key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}
	// Note: Since we only have string type now, we can't really test type mismatch
	// unless we manually inject wrong type item, but Store.Set only accepts string.
	// So this test is trivial now.
	t.Log("TestStore_TypeSafety Passed")
}

func TestStore_NotFound(t *testing.T) {
	t.Log("Starting TestStore_NotFound")
	s := store.New(1024*1024, store.EvictionNoEviction)
	t.Log("Step 1: Getting non-existent key")
	_, err := s.Get("non_existent")
	if err != store.ErrNotFound {
		t.Errorf("Get() error = %v, want %v", err, store.ErrNotFound)
	}
	t.Log("TestStore_NotFound Passed")
}

func TestStore_NonTTL_FIFO_EvictsOldest(t *testing.T) {
	t.Log("Starting TestStore_NonTTL_FIFO_EvictsOldest")
	s := store.New(150, store.EvictionNoEviction) // small cap forces eviction

	t.Log("Step 1: Setting first non-TTL key")
	s.Set("k1", "v1", 0)

	t.Log("Step 2: Setting second non-TTL key (should evict k1 due to FIFO and memory cap)")
	s.Set("k2", "v2", 0)

	t.Log("Step 3: Verifying k1 was evicted")
	if _, err := s.Get("k1"); err != store.ErrNotFound {
		t.Fatalf("expected k1 to be evicted, got err=%v", err)
	}

	t.Log("Step 4: Verifying k2 remains")
	if v, err := s.Get("k2"); err != nil || v != "v2" {
		t.Fatalf("k2 missing or wrong: v=%q err=%v", v, err)
	}
}
