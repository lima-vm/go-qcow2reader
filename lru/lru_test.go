package lru

import (
	"testing"
)

type item struct {
	key   int
	value string
}

func TestCache(t *testing.T) {
	items := []item{{0, "0"}, {1, "1"}, {2, "2"}, {3, "3"}, {4, "4"}}
	cache := New[int, string](5)

	// Cache is empty.
	for _, i := range items {
		if _, ok := cache.Get(i.key); ok {
			t.Errorf("key %d in cache", i.key)
		}
	}

	// Add all items to cache.
	for _, i := range items {
		cache.Add(i.key, i.value)
	}

	// Verify that all items are cached.
	for _, i := range items {
		if value, ok := cache.Get(i.key); !ok {
			t.Errorf("cached value for %d missing", i.key)
		} else if value != i.value {
			t.Errorf("expected %q, got %q", value, i.value)
		}
	}

	// Adding next item will remove the least used item (0).
	cache.Add(5, "5")

	// New item in cache.
	if value, ok := cache.Get(5); !ok {
		t.Errorf("cached value for 5 missing")
	} else if value != "5" {
		t.Errorf("expected \"5\", got %q", value)
	}

	// Removed item not in cache.
	if _, ok := cache.Get(0); ok {
		t.Error("key 0 in cache")
	}

	// Rest of items not affected.
	for _, i := range items[1:] {
		if value, ok := cache.Get(i.key); !ok {
			t.Errorf("cached value for %d missing", i.key)
		} else if value != i.value {
			t.Errorf("expected %q, got %q", value, i.value)
		}
	}

}
