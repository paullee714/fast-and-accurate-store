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

	// Non-TTL FIFO ring buffer
	fifoKeys []string
	fifoHead int // index of oldest
	fifoTail int // index to write next
	fifoSize int // number of valid entries
	fifoCap  int
	tombs    int // tombstones to trigger compaction

	// Optional: when true, allow eviction of TTL keys after FIFO is empty to honor maxMemory.
	evictTTLWhenFull bool
}

// New creates a new Store instance.
func New(maxMemory int64, policy EvictionPolicy) *Store {
	s := &Store{
		data:             make(map[string]*Item),
		maxMemory:        maxMemory,
		evictionPolicy:   policy,
		fifoCap:          1024,
		fifoKeys:         make([]string, 1024),
		evictTTLWhenFull: true,
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
	expired := 0
	for key, item := range s.data {
		if count >= sampleSize {
			break
		}
		if item.ExpiresAt > 0 && now > item.ExpiresAt {
			s.deleteKeyLocked(key)
			expired++
		}
		count++
	}

	// Adaptive: if majority of sampled keys are expired, do an extra pass to avoid lag.
	if expired > sampleSize/2 {
		for key, item := range s.data {
			if item.ExpiresAt > 0 && now > item.ExpiresAt {
				s.deleteKeyLocked(key)
			}
		}
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
	return int64(len(key)+len(value)) + 100
}

func (s *Store) evictIfNeeded() {
	if s.maxMemory <= 0 || s.usedMemory <= s.maxMemory {
		return
	}

	// FIFO eviction for non-TTL keys only.
	for s.usedMemory > s.maxMemory && s.fifoSize > 0 {
		key := s.popFIFO()
		if key != "" {
			s.deleteKeyLocked(key)
		}
	}

	// Optionally evict TTL keys oldest-first (by expiresAt) if still over limit.
	if s.evictTTLWhenFull && s.usedMemory > s.maxMemory {
		for key, item := range s.data {
			if s.usedMemory <= s.maxMemory {
				break
			}
			if item.ExpiresAt > 0 {
				s.deleteKeyLocked(key)
			}
		}
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
	now := time.Now().UnixNano()
	return s.getWithNow(key, now)
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
	now := time.Now().UnixNano()
	return s.getUnlockedWithNow(key, now)
}

func (s *Store) getWithNow(key string, now int64) (string, error) {
	s.mu.RLock()
	item, exists := s.data[key]
	if !exists {
		s.mu.RUnlock()
		return "", ErrNotFound
	}

	if item.ExpiresAt > 0 && now > item.ExpiresAt {
		s.mu.RUnlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		item, exists = s.data[key]
		if exists && item.ExpiresAt > 0 && now > item.ExpiresAt {
			s.deleteKeyUnlocked(key)
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

func (s *Store) getUnlockedWithNow(key string, now int64) (string, error) {
	item, exists := s.data[key]
	if !exists {
		return "", ErrNotFound
	}
	if item.ExpiresAt > 0 && now > item.ExpiresAt {
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
	s.ensureFIFOCap()
	s.fifoKeys[s.fifoTail] = key
	s.fifoTail = (s.fifoTail + 1) % s.fifoCap
	if s.fifoSize < s.fifoCap {
		s.fifoSize++
	} else {
		// overwrote oldest
		s.fifoHead = (s.fifoHead + 1) % s.fifoCap
	}
}

func (s *Store) removeFIFOUnlocked(key string) {
	for i, cnt := s.fifoHead, 0; cnt < s.fifoSize; cnt++ {
		if s.fifoKeys[i] == key {
			s.fifoKeys[i] = "" // tombstone
			s.tombs++
			break
		}
		i++
		if i == s.fifoCap {
			i = 0
		}
	}
	s.compactIfNeeded()
}

// popFIFO pops the oldest non-tombstone key, compacting tombstones on the way.
func (s *Store) popFIFO() string {
	for s.fifoSize > 0 {
		key := s.fifoKeys[s.fifoHead]
		s.fifoKeys[s.fifoHead] = ""
		s.fifoHead = (s.fifoHead + 1) % s.fifoCap
		s.fifoSize--
		if key != "" {
			return key
		}
	}
	return ""
}

func (s *Store) ensureFIFOCap() {
	if s.fifoSize < s.fifoCap-1 {
		return
	}
	oldCap := s.fifoCap
	newCap := oldCap * 2
	newKeys := make([]string, newCap)
	// copy in order
	idx := 0
	for i, cnt := s.fifoHead, 0; cnt < s.fifoSize; cnt++ {
		newKeys[idx] = s.fifoKeys[i]
		idx++
		i++
		if i == oldCap {
			i = 0
		}
	}
	s.fifoKeys = newKeys
	s.fifoHead = 0
	s.fifoTail = s.fifoSize
	s.fifoCap = newCap
	s.tombs = 0
}

func (s *Store) compactIfNeeded() {
	if s.fifoSize == 0 {
		s.fifoHead, s.fifoTail, s.tombs = 0, 0, 0
		return
	}
	if s.tombs < s.fifoSize/2 {
		return
	}
	oldCap := s.fifoCap
	newKeys := make([]string, oldCap)
	idx := 0
	for i, cnt := s.fifoHead, 0; cnt < s.fifoSize; cnt++ {
		if s.fifoKeys[i] != "" {
			newKeys[idx] = s.fifoKeys[i]
			idx++
		}
		i++
		if i == oldCap {
			i = 0
		}
	}
	s.fifoKeys = newKeys
	s.fifoHead = 0
	s.fifoTail = idx
	s.fifoSize = idx
	s.tombs = 0
}
