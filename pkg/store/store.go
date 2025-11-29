package store

import (
	"container/heap"
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
	Value      interface{} // The actual value stored
	Type       DataType    // The type of the value
	ExpiresAt  int64       // Unix timestamp in nanoseconds, 0 means no expiration
	Size       int64       // Estimated size for memory accounting
	Freq       int64       // access frequency (for LFU)
	LastAccess int64       // last access timestamp (ns)
	AccessID   int64       // monotonically increasing version for heap freshness
}

// Stats contains store metrics for monitoring.
type Stats struct {
	Keys           int
	TTLKeys        int
	MemoryUsed     int64
	MaxMemory      int64
	EvictionPolicy EvictionPolicy
	Expired        int64
}

// SnapshotEntry represents a serialized key/value for persistence.
type SnapshotEntry struct {
	Key       string
	Value     string
	Type      DataType
	ExpiresAt int64
}

// EvictionPolicy defines how to select keys to evict when max memory is reached.
type EvictionPolicy int

const (
	EvictionNoEviction EvictionPolicy = iota
	EvictionAllKeysRandom
	EvictionVolatileRandom
	EvictionAllKeysLRU
	EvictionAllKeysLFU
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

	expiredCount int64 // number of keys expired lazily/actively

	accessCounter int64
	lruHeap       *lruMinHeap
	lfuHeap       *lfuMinHeap
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
		lruHeap:          &lruMinHeap{},
		lfuHeap:          &lfuMinHeap{},
	}
	return s
}

// Stats returns current store statistics.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ttl := 0
	for _, item := range s.data {
		if item.ExpiresAt > 0 {
			ttl++
		}
	}
	return Stats{
		Keys:           len(s.data),
		TTLKeys:        ttl,
		MemoryUsed:     s.usedMemory,
		MaxMemory:      s.maxMemory,
		EvictionPolicy: s.evictionPolicy,
		Expired:        s.expiredCount,
	}
}

// SnapshotEntries returns a copy of current data for persistence.
func (s *Store) SnapshotEntries() []SnapshotEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]SnapshotEntry, 0, len(s.data))
	now := time.Now().UnixNano()
	for k, v := range s.data {
		// Skip expired during snapshot to avoid restoring stale data
		if v.ExpiresAt > 0 && now > v.ExpiresAt {
			continue
		}
		val, _ := v.Value.(string)
		entries = append(entries, SnapshotEntry{
			Key:       k,
			Value:     val,
			Type:      v.Type,
			ExpiresAt: v.ExpiresAt,
		})
	}
	return entries
}

// RestoreSnapshot loads entries into the store, replacing existing data.
func (s *Store) RestoreSnapshot(entries []SnapshotEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string]*Item, len(entries))
	s.usedMemory = 0
	s.fifoHead, s.fifoTail, s.fifoSize, s.tombs = 0, 0, 0, 0

	now := time.Now().UnixNano()
	for _, e := range entries {
		if e.ExpiresAt > 0 && now > e.ExpiresAt {
			continue
		}
		accessID := s.nextAccessID()
		item := &Item{
			Value:      e.Value,
			Type:       e.Type,
			ExpiresAt:  e.ExpiresAt,
			Size:       estimateItemSize(e.Key, e.Value),
			Freq:       1,
			LastAccess: now,
			AccessID:   accessID,
		}
		s.data[e.Key] = item
		s.usedMemory += item.Size
		if item.ExpiresAt == 0 {
			s.addFIFOUnlocked(e.Key)
		}
		s.pushHeaps(e.Key, item)
	}
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
			s.expiredCount++
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

	switch s.evictionPolicy {
	case EvictionAllKeysRandom:
		s.evictRandom(false)
	case EvictionVolatileRandom:
		s.evictRandom(true)
	case EvictionAllKeysLRU:
		s.evictLRU()
	case EvictionAllKeysLFU:
		s.evictLFU()
	default:
		// FIFO eviction for non-TTL keys only.
		for s.usedMemory > s.maxMemory && s.fifoSize > 0 {
			key := s.popFIFO()
			if key != "" {
				s.deleteKeyLocked(key)
			}
		}
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
}

func (s *Store) evictRandom(volatileOnly bool) {
	for key, item := range s.data {
		if s.usedMemory <= s.maxMemory {
			return
		}
		if volatileOnly && item.ExpiresAt == 0 {
			continue
		}
		s.deleteKeyLocked(key)
	}
}

func (s *Store) evictLRU() {
	for s.usedMemory > s.maxMemory {
		key := s.popLRU()
		if key == "" {
			return
		}
		s.deleteKeyLocked(key)
	}
}

func (s *Store) evictLFU() {
	for s.usedMemory > s.maxMemory {
		key := s.popLFU()
		if key == "" {
			return
		}
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
	s.mu.RUnlock()
	if !exists {
		return "", ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists = s.data[key]
	if !exists {
		return "", ErrNotFound
	}
	if item.ExpiresAt > 0 && now > item.ExpiresAt {
		s.deleteKeyUnlocked(key)
		s.expiredCount++
		return "", ErrNotFound
	}
	if item.Type != TypeString {
		return "", ErrWrongType
	}

	// Update access stats
	item.LastAccess = now
	item.AccessID = s.nextAccessID()
	item.Freq++
	s.pushHeaps(key, item)

	return item.Value.(string), nil
}

func (s *Store) getUnlockedWithNow(key string, now int64) (string, error) {
	item, exists := s.data[key]
	if !exists {
		return "", ErrNotFound
	}
	if item.ExpiresAt > 0 && now > item.ExpiresAt {
		s.deleteKeyUnlocked(key)
		s.expiredCount++
		return "", ErrNotFound
	}
	if item.Type != TypeString {
		return "", ErrWrongType
	}

	item.LastAccess = now
	item.AccessID = s.nextAccessID()
	item.Freq++
	s.pushHeaps(key, item)

	return item.Value.(string), nil
}

// setUnlocked sets a key without taking a lock. Caller must manage locking or exclusivity.
func (s *Store) setUnlocked(key string, value string, ttl time.Duration) {
	now := time.Now().UnixNano()
	expiresAt := int64(0)
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl).UnixNano()
	}

	accessID := s.nextAccessID()

	newItem := &Item{
		Value:      value,
		Type:       TypeString,
		ExpiresAt:  expiresAt,
		Size:       estimateItemSize(key, value),
		Freq:       1,
		LastAccess: now,
		AccessID:   accessID,
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
	s.pushHeaps(key, newItem)

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

// Delete removes a key if it exists.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteKeyUnlocked(key)
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

func (s *Store) nextAccessID() int64 {
	s.accessCounter++
	return s.accessCounter
}

func (s *Store) pushHeaps(key string, item *Item) {
	heap.Push(s.lruHeap, lruEntry{key: key, ts: item.LastAccess, version: item.AccessID})
	heap.Push(s.lfuHeap, lfuEntry{key: key, freq: item.Freq, ts: item.LastAccess, version: item.AccessID})
}

func (s *Store) popLRU() string {
	for s.lruHeap.Len() > 0 {
		entry := heap.Pop(s.lruHeap).(lruEntry)
		if item, ok := s.data[entry.key]; ok && item.AccessID == entry.version {
			return entry.key
		}
	}
	return ""
}

func (s *Store) popLFU() string {
	for s.lfuHeap.Len() > 0 {
		entry := heap.Pop(s.lfuHeap).(lfuEntry)
		if item, ok := s.data[entry.key]; ok && item.AccessID == entry.version {
			return entry.key
		}
	}
	return ""
}

type lruEntry struct {
	key     string
	ts      int64
	version int64
}

type lruMinHeap []lruEntry

func (h lruMinHeap) Len() int           { return len(h) }
func (h lruMinHeap) Less(i, j int) bool { return h[i].ts < h[j].ts }
func (h lruMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *lruMinHeap) Push(x interface{}) {
	*h = append(*h, x.(lruEntry))
}
func (h *lruMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type lfuEntry struct {
	key     string
	freq    int64
	ts      int64
	version int64
}

type lfuMinHeap []lfuEntry

func (h lfuMinHeap) Len() int { return len(h) }
func (h lfuMinHeap) Less(i, j int) bool {
	if h[i].freq == h[j].freq {
		return h[i].ts < h[j].ts
	}
	return h[i].freq < h[j].freq
}
func (h lfuMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *lfuMinHeap) Push(x interface{}) {
	*h = append(*h, x.(lfuEntry))
}
func (h *lfuMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
