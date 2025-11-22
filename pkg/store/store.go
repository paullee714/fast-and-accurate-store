package store

import (
	"errors"
	"sync"
	"time"
)

// DataType represents the type of value stored in the system.
type DataType int

const (
	// TypeString represents a simple string value.
	TypeString DataType = iota
	// TypeList represents a list of strings (future implementation).
	TypeList
	// TypeMap represents a hash map (future implementation).
	TypeMap
	// TypeSet represents a set of unique strings (future implementation).
	TypeSet
)

var (
	// ErrWrongType is returned when an operation is performed on a key holding the wrong kind of value.
	ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	// ErrNotFound is returned when the specified key does not exist.
	ErrNotFound = errors.New("key not found")
)

// Item represents a single data item stored in the store.
type Item struct {
	Value     interface{} // The actual value stored
	Type      DataType    // The type of the value
	ExpiresAt int64       // Unix timestamp in nanoseconds, 0 means no expiration
}

// Store is a thread-safe in-memory key-value store.
type Store struct {
	mu   sync.RWMutex
	data map[string]*Item
}

// New creates and returns a new instance of Store.
func New() *Store {
	return &Store{
		data: make(map[string]*Item),
	}
}

// Set stores a key-value pair with an optional TTL (Time To Live).
// If ttl is 0, the key does not expire.
func (s *Store) Set(key string, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixNano()
	}

	s.data[key] = &Item{
		Value:     value,
		Type:      TypeString,
		ExpiresAt: expiresAt,
	}
}

// Get retrieves the value associated with the given key.
// It returns ErrNotFound if the key does not exist or has expired.
// It returns ErrWrongType if the stored value is not a string.
func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	item, exists := s.data[key]
	if !exists {
		s.mu.RUnlock()
		return "", ErrNotFound
	}

	// Lazy expiration check
	if item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
		s.mu.RUnlock() // Release read lock to acquire write lock

		s.mu.Lock()
		// Double-check existence and expiration after acquiring write lock
		item, exists = s.data[key]
		if exists && item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
			delete(s.data, key)
		}
		s.mu.Unlock()

		return "", ErrNotFound
	}

	if item.Type != TypeString {
		s.mu.RUnlock()
		return "", ErrWrongType
	}

	val := item.Value.(string)
	s.mu.RUnlock()
	return val, nil
}
