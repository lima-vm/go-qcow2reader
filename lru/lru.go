package lru

import (
	"container/list"
	"sync"
)

// Cache keeps recently used values. Safe for concurrent use by multiple
// goroutines.
type Cache[K comparable, V any] struct {
	mutex        sync.Mutex
	entries      map[K]*list.Element
	recentlyUsed *list.List
	capacity     int
}

type cacheEntry[K comparable, V any] struct {
	Key   K
	Value V
}

// New returns a new empty cache that can hold up to capacity items.
func New[K comparable, V any](capacity int) *Cache[K, V] {
	return &Cache[K, V]{
		entries:      make(map[K]*list.Element),
		recentlyUsed: list.New(),
		capacity:     capacity,
	}
}

// Get returns the value stored in the cache for a key, or zero value if no
// value is present. The ok result indicates whether value was found in the
// cache.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if elem, ok := c.entries[key]; ok {
		c.recentlyUsed.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry[K, V])
		return entry.Value, true
	}

	var missing V
	return missing, false
}

// Add adds key and value to the cache. If the cache is full, the oldest entry
// is removed. If key is already in the cache, value replaces the cached value.
func (c *Cache[K, V]) Add(key K, value V) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if elem, ok := c.entries[key]; ok {
		c.recentlyUsed.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry[K, V])
		entry.Value = value
		return
	}

	if len(c.entries) >= c.capacity {
		oldest := c.recentlyUsed.Back()
		c.recentlyUsed.Remove(oldest)
		entry := oldest.Value.(*cacheEntry[K, V])
		delete(c.entries, entry.Key)
	}

	entry := &cacheEntry[K, V]{Key: key, Value: value}
	c.entries[key] = c.recentlyUsed.PushFront(entry)
}
