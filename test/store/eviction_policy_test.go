package store_test

import (
	"testing"
	"time"

	"fas/pkg/store"
)

func TestStore_EvictionLRU(t *testing.T) {
	s := store.New(250, store.EvictionAllKeysLRU)

	s.Set("k1", "v1", 0)
	s.Set("k2", "v2", 0)

	// Touch k1 to make k2 the LRU
	if _, err := s.Get("k1"); err != nil {
		t.Fatalf("get k1: %v", err)
	}

	s.Set("k3", "v3", 0) // should evict k2

	if _, err := s.Get("k2"); err != store.ErrNotFound {
		t.Fatalf("expected k2 evicted, got %v", err)
	}
	if v, err := s.Get("k1"); err != nil || v != "v1" {
		t.Fatalf("k1 should remain, got %q err=%v", v, err)
	}
	if v, err := s.Get("k3"); err != nil || v != "v3" {
		t.Fatalf("k3 missing, got %q err=%v", v, err)
	}
}

func TestStore_EvictionLFU(t *testing.T) {
	s := store.New(250, store.EvictionAllKeysLFU)

	s.Set("k1", "v1", 0)
	s.Set("k2", "v2", 0)

	// Increase k1 frequency
	for i := 0; i < 3; i++ {
		if _, err := s.Get("k1"); err != nil {
			t.Fatalf("get k1: %v", err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	s.Set("k3", "v3", 0) // should evict lowest freq (k2)

	if _, err := s.Get("k2"); err != store.ErrNotFound {
		t.Fatalf("expected k2 evicted, got %v", err)
	}
	if v, err := s.Get("k1"); err != nil || v != "v1" {
		t.Fatalf("k1 should remain, got %q err=%v", v, err)
	}
	if v, err := s.Get("k3"); err != nil || v != "v3" {
		t.Fatalf("k3 missing, got %q err=%v", v, err)
	}
}
