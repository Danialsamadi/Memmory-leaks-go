# Cache Patterns

**Read Time**: 25 minutes

**Related**: [Eviction Strategies](./05-eviction-strategies.md) | [Back to README](../README.md)

---

## LRU Cache Implementation

See `fixed_cache.go` for a complete implementation using `container/list`.

## TTL-Based Cache

```go
type CacheEntry struct {
    Value     interface{}
    ExpiresAt time.Time
}

func (c *Cache) Get(key string) (interface{}, bool) {
    entry, ok := c.data[key]
    if !ok {
        return nil, false
    }
    
    if time.Now().After(entry.ExpiresAt) {
        delete(c.data, key)  // Expired
        return nil, false
    }
    
    return entry.Value, true
}
```

## Size-Limited Cache

```go
type Cache struct {
    data     map[string]interface{}
    maxSize  int
    maxBytes int64
    curBytes int64
}

func (c *Cache) Set(key string, value interface{}) {
    size := calculateSize(value)
    
    if c.curBytes+size > c.maxBytes {
        c.evictUntilSpace(size)
    }
    
    c.data[key] = value
    c.curBytes += size
}
```

---

**Return to**: [Long-Lived References README](../README.md)

