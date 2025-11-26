package server

import (
	"fmt"
	"strings"

	"fas/pkg/store"
)

// ParseEvictionPolicy parses string to store.EvictionPolicy.
func ParseEvictionPolicy(p string) (store.EvictionPolicy, error) {
	switch strings.ToLower(p) {
	case "noeject", "none", "no-eviction":
		return store.EvictionNoEviction, nil
	case "allkeys-random":
		return store.EvictionAllKeysRandom, nil
	case "volatile-random":
		return store.EvictionVolatileRandom, nil
	case "allkeys-lru":
		return store.EvictionAllKeysLRU, nil
	case "allkeys-lfu":
		return store.EvictionAllKeysLFU, nil
	default:
		return 0, fmt.Errorf("unknown policy %s", p)
	}
}
