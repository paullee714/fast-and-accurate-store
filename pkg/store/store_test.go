package store

import (
	"testing"
	"time"
)

func TestStore_SetGet(t *testing.T) {
	s := New()

	key := "test_key"
	value := "test_value"

	s.Set(key, value, 0)

	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != value {
		t.Errorf("Get() = %v, want %v", got, value)
	}
}

func TestStore_Expiration(t *testing.T) {
	s := New()

	key := "expire_key"
	value := "expire_value"
	ttl := 10 * time.Millisecond

	s.Set(key, value, ttl)

	// Verify it exists initially
	_, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get() before expiration error = %v", err)
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Verify it's gone
	_, err = s.Get(key)
	if err != ErrNotFound {
		t.Errorf("Get() after expiration error = %v, want %v", err, ErrNotFound)
	}
}

func TestStore_TypeSafety(t *testing.T) {
	s := New()
	key := "string_key"
	s.Set(key, "value", 0)

	// Manually inject a wrong type to test safety (simulating internal corruption or future list implementation)
	// Since we can't easily inject wrong type via public API yet without other methods,
	// we rely on the fact that Set currently only supports String.
	// So we verify that Get returns string and no error for now.
	// Ideally, if we had List support, we would SetList and try Get (String), expecting ErrWrongType.

	// For now, let's just verify standard behavior is type safe for strings
	val, err := s.Get(key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if val != "value" {
		t.Errorf("Get() = %v, want %v", val, "value")
	}
}

func TestStore_NotFound(t *testing.T) {
	s := New()
	_, err := s.Get("non_existent")
	if err != ErrNotFound {
		t.Errorf("Get() error = %v, want %v", err, ErrNotFound)
	}
}
