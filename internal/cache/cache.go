package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     interface{}
	expiresAt int64
}

// Cache provides a thread-safe in-memory TTL cache using Go stdlib sync.RWMutex.
// ponytail: stdlib in-memory TTL cache; upgrade to distributed cache (Redis) when scaling across multi-instance clusters.
type Cache struct {
	mu    sync.RWMutex
	items map[string]entry
}

func New() *Cache {
	return &Cache{
		items: make(map[string]entry),
	}
}

func (c *Cache) Set(key string, val interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = entry{
		value:     val,
		expiresAt: time.Now().Add(ttl).UnixNano(),
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return nil, false
	}

	if time.Now().UnixNano() > item.expiresAt {
		c.Delete(key)
		return nil, false
	}

	return item.value, true
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]entry)
}
