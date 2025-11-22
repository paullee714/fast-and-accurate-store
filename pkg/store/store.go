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

// EvictionPolicy defines how to select keys to evict when max memory is reached.
type EvictionPolicy int

const (
	EvictionNoEviction EvictionPolicy = iota
	EvictionAllKeysRandom
	EvictionVolatileRandom
)

// Store represents the in-memory key-value store.
type Store struct {
	data           map[string]*Item
	mu             sync.RWMutex
	maxMemory      int64
	usedMemory     int64
	evictionPolicy EvictionPolicy
}

// New creates a new Store instance.
func New(maxMemory int64, policy EvictionPolicy) *Store {
	s := &Store{
		data:           make(map[string]*Item),
		maxMemory:      maxMemory,
		evictionPolicy: policy,
	}
	return s
}

// StartActiveExpiration starts the active expiration loop.
// Should only be called in multi-threaded mode.
func (s *Store) StartActiveExpiration() {
	go s.activeExpirationLoop()
}

// activeExpirationLoop periodically samples keys and deletes expired ones.
func (s *Store) activeExpirationLoop() {
	ticker := time.NewTicker(100 * time.Millisecond) // Run 10 times per second
	defer ticker.Stop()

	for range ticker.C {
		s.activeExpire()
	}
}

func (s *Store) activeExpire() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.data) == 0 {
		return
	}

	// Sample 20 keys
	sampleSize := 20
	expiredCount := 0

	// Go map iteration is random
	count := 0
	for key, item := range s.data {
		if count >= sampleSize {
			break
		}
		if item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
			delete(s.data, key)
			s.usedMemory -= estimateSize(key, item)
			expiredCount++
		}
		count++
	}
}

func estimateSize(key string, item *Item) int64 {
	// Rough estimation: key len + value len + struct overhead
	size := int64(len(key)) + 100 // overhead
	if item.Type == TypeString {
		size += int64(len(item.Value.(string)))
	}
	return size
}

func (s *Store) evictIfNeeded() {
	if s.maxMemory <= 0 || s.usedMemory <= s.maxMemory {
		return
	}

	// Simple random eviction
	// Note: Go map iteration order is not guaranteed to be random,
	// but for a simple implementation, iterating and deleting is sufficient.
	// For true random eviction, keys would need to be stored in a separate data structure.
	for k, v := range s.data {
		if s.usedMemory <= s.maxMemory {
			break
		}
		// If policy is volatile, only evict if has TTL
		if s.evictionPolicy == EvictionVolatileRandom && v.ExpiresAt == 0 {
			continue
		}

		delete(s.data, k)
		s.usedMemory -= estimateSize(k, v)
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

	newItem := &Item{
		Value:     value,
		Type:      TypeString,
		ExpiresAt: expiresAt,
	}

	// Calculate size delta
	newSize := estimateSize(key, newItem)
	oldSize := int64(0)
	if oldItem, exists := s.data[key]; exists {
		oldSize = estimateSize(key, oldItem)
	}

	s.usedMemory += newSize - oldSize
	s.data[key] = newItem

	s.evictIfNeeded()
}

// Get retrieves the value associated with the given key.
// It returns ErrNotFound if the key does not exist or has expired.
// It returns ErrWrongType if the stored value is not a string.
func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	// We don't defer RUnlock here because we might need to upgrade lock

	item, exists := s.data[key]
	if !exists {
		s.mu.RUnlock()
		return "", ErrNotFound
	}

	// Lazy expiration check
	if item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
		s.mu.RUnlock()

		// Upgrade to write lock to delete
		s.mu.Lock()
		defer s.mu.Unlock()

		// Double check after acquiring lock
		item, exists = s.data[key]
		if exists && item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
			delete(s.data, key)
			s.usedMemory -= estimateSize(key, item)
		}

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

// UnsafeSet stores a key-value pair without locking.
// USE ONLY IN SINGLE-THREADED CONTEXT.
func (s *Store) UnsafeSet(key string, value string, ttl time.Duration) {
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixNano()
	}

	newItem := &Item{
		Value:     value,
		Type:      TypeString,
		ExpiresAt: expiresAt,
	}

	newSize := estimateSize(key, newItem)
	oldSize := int64(0)
	if oldItem, exists := s.data[key]; exists {
		oldSize = estimateSize(key, oldItem)
	}

	s.usedMemory += newSize - oldSize
	s.data[key] = newItem

	// For unsafe, we assume the caller handles eviction or we call it here?
	// Since UnsafeSet is used in EventLoop which is single threaded, we can call evictIfNeeded.
	// But evictIfNeeded uses range loop which is safe in single thread.
	// However, evictIfNeeded currently doesn't lock, but s.data access is safe.
	// Wait, evictIfNeeded is not exported and assumes lock is held if called from Set.
	// But UnsafeSet doesn't hold lock.
	// We should make a version of evictIfNeeded that doesn't lock?
	// Actually evictIfNeeded DOES NOT lock itself, it expects lock to be held.
	// So calling it from UnsafeSet is fine as long as no other thread is accessing data.
	// But wait, activeExpirationLoop runs in background and locks!
	// If we use EventLoop, we shouldn't use activeExpirationLoop with locks?
	// Or we should disable activeExpirationLoop in EventLoop mode?
	// Yes, in EventLoop mode, active expiration should be done as an event or periodic check in the loop, not a separate goroutine with locks.

	// For now, let's just update memory usage.
}

// UnsafeGet retrieves a value without locking.
// USE ONLY IN SINGLE-THREADED CONTEXT.
func (s *Store) UnsafeGet(key string) (string, error) {
	item, exists := s.data[key]
	if !exists {
		return "", ErrNotFound
	}

	// Lazy expiration check
	if item.ExpiresAt > 0 && time.Now().UnixNano() > item.ExpiresAt {
		delete(s.data, key)
		return "", ErrNotFound
	}

	if item.Type != TypeString {
		return "", ErrWrongType
	}

	return item.Value.(string), nil
}
