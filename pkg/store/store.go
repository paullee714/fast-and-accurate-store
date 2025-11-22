package store

import (
	"container/list"
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
	Size      int64       // Estimated size for memory accounting
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

	fifoList  *list.List               // insertion order for non-TTL keys
	fifoIndex map[string]*list.Element // key -> element in fifoList
}

// New creates a new Store instance.
func New(maxMemory int64, policy EvictionPolicy) *Store {
	s := &Store{
		data:           make(map[string]*Item),
		maxMemory:      maxMemory,
		evictionPolicy: policy,
		fifoList:       list.New(),
		fifoIndex:      make(map[string]*list.Element),
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

	// Go map iteration is random
	count := 0
	now := time.Now().UnixNano()
	for key, item := range s.data {
		if count >= sampleSize {
			break
		}
		if item.ExpiresAt > 0 && now > item.ExpiresAt {
			s.deleteKeyLocked(key)
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

func estimateItemSize(key string, value string) int64 {
	return int64(len(key)) + 100 + int64(len(value))
}

func (s *Store) evictIfNeeded() {
	if s.maxMemory <= 0 || s.usedMemory <= s.maxMemory {
		return
	}

	// FIFO eviction for non-TTL keys only.
	for s.usedMemory > s.maxMemory && s.fifoList.Len() > 0 {
		elem := s.fifoList.Front()
		key := elem.Value.(string)
		s.deleteKeyLocked(key)
	}
}

// Set stores a key-value pair with an optional TTL (Time To Live).
// If ttl is 0, the key does not expire.
func (s *Store) Set(key string, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setUnlocked(key, value, ttl)
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
	now := time.Now().UnixNano()
	if item.ExpiresAt > 0 && now > item.ExpiresAt {
		s.mu.RUnlock()

		// Upgrade to write lock to delete
		s.mu.Lock()
		defer s.mu.Unlock()

		// Double check after acquiring lock
		item, exists = s.data[key]
		if exists && item.ExpiresAt > 0 && now > item.ExpiresAt {
			s.deleteKeyLocked(key)
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
	// Caller must ensure exclusive access.
	s.setUnlocked(key, value, ttl)
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
		s.deleteKeyUnlocked(key)
		return "", ErrNotFound
	}

	if item.Type != TypeString {
		return "", ErrWrongType
	}

	return item.Value.(string), nil
}

// setUnlocked sets a key without taking a lock. Caller must manage locking or exclusivity.
func (s *Store) setUnlocked(key string, value string, ttl time.Duration) {
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixNano()
	}

	newItem := &Item{
		Value:     value,
		Type:      TypeString,
		ExpiresAt: expiresAt,
		Size:      estimateItemSize(key, value),
	}

	// Remove existing entry accounting
	if oldItem, exists := s.data[key]; exists {
		s.usedMemory -= oldItem.Size
		if oldItem.ExpiresAt == 0 {
			s.removeFIFOUnlocked(key)
		}
	}

	s.data[key] = newItem
	s.usedMemory += newItem.Size

	if ttl == 0 {
		s.addFIFOUnlocked(key)
	} else {
		s.removeFIFOUnlocked(key)
	}

	s.evictIfNeeded()
}

// deleteKeyLocked deletes a key and updates tracking structures. Caller must hold write lock.
func (s *Store) deleteKeyLocked(key string) {
	s.deleteKeyUnlocked(key)
}

// deleteKeyUnlocked deletes key without acquiring locks. Caller must ensure safety.
func (s *Store) deleteKeyUnlocked(key string) {
	if item, exists := s.data[key]; exists {
		s.usedMemory -= item.Size
		if item.ExpiresAt == 0 {
			s.removeFIFOUnlocked(key)
		}
		delete(s.data, key)
	}
}

func (s *Store) addFIFOUnlocked(key string) {
	if elem, ok := s.fifoIndex[key]; ok {
		s.fifoList.Remove(elem)
	}
	s.fifoIndex[key] = s.fifoList.PushBack(key)
}

func (s *Store) removeFIFOUnlocked(key string) {
	if elem, ok := s.fifoIndex[key]; ok {
		s.fifoList.Remove(elem)
		delete(s.fifoIndex, key)
	}
}
