# Cache Patterns: Implementing Bounded Caches in Go

**Read Time**: 25 minutes

**Prerequisites**: Understanding of [Memory Model](./01-memory-model-explanation.md) and [GC Behavior](./02-gc-behavior.md)

**Related Topics**: 
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [GC Behavior](./02-gc-behavior.md)
- [Eviction Strategies](./05-eviction-strategies.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Why Caches Need Bounds](#why-caches-need-bounds)
2. [LRU Cache Implementation](#lru-cache-implementation)
3. [TTL-Based Cache](#ttl-based-cache)
4. [Size-Based Limits](#size-based-limits)
5. [Concurrent-Safe Caches](#concurrent-safe-caches)
6. [Cache Libraries](#cache-libraries)
7. [Performance Considerations](#performance-considerations)
8. [Summary](#summary)

---

## Why Caches Need Bounds

### The Unbounded Cache Problem

```go
// DANGEROUS: Unbounded cache
var cache = make(map[string]*Data)

func get(key string) *Data {
    if data, ok := cache[key]; ok {
        return data
    }
    
    data := fetchFromDatabase(key)
    cache[key] = data  // Grows forever!
    return data
}

// After 1 million unique keys:
// - Map holds 1 million entries
// - Memory usage: 1M × (keySize + valueSize + overhead)
// - No eviction mechanism
// - Eventually: Out of memory
```

### Production Impact

```
Timeline of unbounded cache:

Hour 1:   1,000 entries   →   10 MB     → Normal
Hour 6:   10,000 entries  →   100 MB    → Elevated
Day 1:    100,000 entries →   1 GB      → Warning
Week 1:   1M entries      →   10 GB     → Critical
Week 2:   OOM Kill        →   Crash     → Outage

Cost: Downtime, data loss, customer impact
Fix: Add bounds and eviction policy
```

### Why Maps Don't Shrink

```go
// Important Go behavior
m := make(map[string]int)

// Add 1 million entries
for i := 0; i < 1_000_000; i++ {
    m[fmt.Sprintf("key_%d", i)] = i
}
// Map internal storage: ~50 MB

// Delete all but 10 entries
for i := 10; i < 1_000_000; i++ {
    delete(m, fmt.Sprintf("key_%d", i))
}
// Map internal storage: STILL ~50 MB!
// Deleted entries leave tombstones
// Internal hash table doesn't shrink

// Only way to reclaim:
m = make(map[string]int)  // Start fresh
// Old map can be GC'd
```

---

## LRU Cache Implementation

### Concept

**LRU (Least Recently Used)**: Evict the least recently accessed item when cache is full.

```
Access pattern: A, B, C, A, D, E (cache max size: 3)

Step 1: Access A
  Cache: [A]

Step 2: Access B
  Cache: [A, B]

Step 3: Access C
  Cache: [A, B, C]  (full)

Step 4: Access A
  Cache: [B, C, A]  (A moved to end)

Step 5: Access D
  Cache: [C, A, D]  (B evicted - least recently used)

Step 6: Access E
  Cache: [A, D, E]  (C evicted - least recently used)
```

### Simple LRU Implementation

```go
package main

import (
    "container/list"
    "sync"
)

type LRUCache struct {
    capacity int
    cache    map[string]*list.Element
    lruList  *list.List
    mu       sync.Mutex
}

type entry struct {
    key   string
    value interface{}
}

func NewLRUCache(capacity int) *LRUCache {
    return &LRUCache{
        capacity: capacity,
        cache:    make(map[string]*list.Element),
        lruList:  list.New(),
    }
}

func (c *LRUCache) Get(key string) (interface{}, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if elem, ok := c.cache[key]; ok {
        // Move to front (most recently used)
        c.lruList.MoveToFront(elem)
        return elem.Value.(*entry).value, true
    }
    
    return nil, false
}

func (c *LRUCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Check if key exists
    if elem, ok := c.cache[key]; ok {
        // Update existing entry
        c.lruList.MoveToFront(elem)
        elem.Value.(*entry).value = value
        return
    }
    
    // Add new entry
    elem := c.lruList.PushFront(&entry{key, value})
    c.cache[key] = elem
    
    // Evict if over capacity
    if c.lruList.Len() > c.capacity {
        c.evictOldest()
    }
}

func (c *LRUCache) evictOldest() {
    elem := c.lruList.Back()
    if elem != nil {
        c.lruList.Remove(elem)
        delete(c.cache, elem.Value.(*entry).key)
    }
}

func (c *LRUCache) Len() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.lruList.Len()
}
```

### Usage Example

```go
func main() {
    cache := NewLRUCache(1000)  // Max 1000 entries
    
    // Add entries
    for i := 0; i < 10000; i++ {
        cache.Put(fmt.Sprintf("key_%d", i), &Data{ID: i})
    }
    
    // Cache never exceeds 1000 entries
    fmt.Printf("Cache size: %d\n", cache.Len())  // 1000
    
    // Get entry (moves to front)
    if value, ok := cache.Get("key_9999"); ok {
        fmt.Printf("Found: %v\n", value)
    }
}
```

### LRU Memory Behavior

```
Memory usage over time (max 1000 entries):

Without LRU:
Memory │     ╱
       │   ╱
       │  ╱
       │ ╱
       │╱________________
       └────────────────► Time
       Grows unbounded

With LRU:
Memory │  ┌────────────────
       │ ╱ reaches max
       │╱  and stays flat
       └────────────────► Time
       Bounded and predictable
```

---

## TTL-Based Cache

### Concept

**TTL (Time To Live)**: Each entry expires after a certain duration.

```go
package main

import (
    "sync"
    "time"
)

type TTLCache struct {
    entries map[string]*ttlEntry
    mu      sync.RWMutex
    ttl     time.Duration
}

type ttlEntry struct {
    value     interface{}
    expiresAt time.Time
}

func NewTTLCache(ttl time.Duration) *TTLCache {
    c := &TTLCache{
        entries: make(map[string]*ttlEntry),
        ttl:     ttl,
    }
    
    // Start cleanup goroutine
    go c.cleanupLoop()
    
    return c
}

func (c *TTLCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    entry, ok := c.entries[key]
    if !ok {
        return nil, false
    }
    
    // Check if expired
    if time.Now().After(entry.expiresAt) {
        return nil, false
    }
    
    return entry.value, true
}

func (c *TTLCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.entries[key] = &ttlEntry{
        value:     value,
        expiresAt: time.Now().Add(c.ttl),
    }
}

func (c *TTLCache) cleanupLoop() {
    ticker := time.NewTicker(c.ttl / 2)
    defer ticker.Stop()
    
    for range ticker.C {
        c.cleanup()
    }
}

func (c *TTLCache) cleanup() {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    now := time.Now()
    for key, entry := range c.entries {
        if now.After(entry.expiresAt) {
            delete(c.entries, key)
        }
    }
}

func (c *TTLCache) Len() int {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return len(c.entries)
}
```

### TTL Usage Example

```go
func main() {
    // Entries expire after 5 minutes
    cache := NewTTLCache(5 * time.Minute)
    
    // Add entry
    cache.Put("session_123", &UserSession{UserID: 123})
    
    // Retrieve within TTL
    time.Sleep(2 * time.Minute)
    if val, ok := cache.Get("session_123"); ok {
        fmt.Println("Session found:", val)
    }
    
    // Expired after TTL
    time.Sleep(4 * time.Minute)
    if _, ok := cache.Get("session_123"); !ok {
        fmt.Println("Session expired")
    }
}
```

### TTL Memory Pattern

```
Entry lifecycle:

Time 0:     Add entry
Time 0-5m:  Entry accessible
Time 5m:    Entry expires (logically)
Time 5-10m: Entry in memory but not accessible
Time 10m:   Cleanup runs, entry deleted

Memory usage:
       │    ╱╲    ╱╲    ╱╲
       │   ╱  ╲  ╱  ╲  ╱  ╲
       │  ╱    ╲╱    ╲╱    ╲
       └─────────────────────► Time
          ^    ^    ^
        Cleanup runs periodically
```

---

## Size-Based Limits

### Concept

Track actual memory usage, not just entry count.

```go
package main

import (
    "sync"
)

type SizeBasedCache struct {
    entries     map[string]interface{}
    entrySizes  map[string]int
    currentSize int
    maxSize     int
    mu          sync.Mutex
}

func NewSizeBasedCache(maxSizeBytes int) *SizeBasedCache {
    return &SizeBasedCache{
        entries:    make(map[string]interface{}),
        entrySizes: make(map[string]int),
        maxSize:    maxSizeBytes,
    }
}

func (c *SizeBasedCache) Put(key string, value interface{}, size int) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Check if we're replacing an existing key
    if oldSize, ok := c.entrySizes[key]; ok {
        c.currentSize -= oldSize
        delete(c.entries, key)
        delete(c.entrySizes, key)
    }
    
    // Check if new value fits
    if c.currentSize+size > c.maxSize {
        return false  // Cache full
    }
    
    // Add entry
    c.entries[key] = value
    c.entrySizes[key] = size
    c.currentSize += size
    
    return true
}

func (c *SizeBasedCache) Get(key string) (interface{}, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    value, ok := c.entries[key]
    return value, ok
}

func (c *SizeBasedCache) CurrentSize() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.currentSize
}
```

### Advanced: Size-Based LRU

```go
// Combines LRU eviction with size tracking
type SizeLRUCache struct {
    capacity    int  // Max total size in bytes
    currentSize int
    cache       map[string]*list.Element
    lruList     *list.List
    mu          sync.Mutex
}

type sizeEntry struct {
    key   string
    value interface{}
    size  int
}

func (c *SizeLRUCache) Put(key string, value interface{}, size int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Evict until we have space
    for c.currentSize+size > c.capacity && c.lruList.Len() > 0 {
        c.evictOldest()
    }
    
    // Add new entry
    elem := c.lruList.PushFront(&sizeEntry{key, value, size})
    c.cache[key] = elem
    c.currentSize += size
}

func (c *SizeLRUCache) evictOldest() {
    elem := c.lruList.Back()
    if elem != nil {
        entry := elem.Value.(*sizeEntry)
        c.lruList.Remove(elem)
        delete(c.cache, entry.key)
        c.currentSize -= entry.size
    }
}
```

---

## Concurrent-Safe Caches

### sync.Map for Simple Cases

```go
package main

import "sync"

type SimpleCache struct {
    m sync.Map
}

func (c *SimpleCache) Get(key string) (interface{}, bool) {
    return c.m.Load(key)
}

func (c *SimpleCache) Put(key string, value interface{}) {
    c.m.Store(key, value)
}

// Good for: Read-heavy workloads
// Bad for: Needs eviction, size limits, complex logic
```

### Sharded Cache for High Concurrency

```go
package main

import (
    "hash/fnv"
    "sync"
)

type ShardedCache struct {
    shards    []*CacheShard
    shardMask uint32
}

type CacheShard struct {
    entries map[string]interface{}
    mu      sync.RWMutex
}

func NewShardedCache(shardCount int) *ShardedCache {
    // shardCount must be power of 2
    shards := make([]*CacheShard, shardCount)
    for i := range shards {
        shards[i] = &CacheShard{
            entries: make(map[string]interface{}),
        }
    }
    
    return &ShardedCache{
        shards:    shards,
        shardMask: uint32(shardCount - 1),
    }
}

func (c *ShardedCache) getShard(key string) *CacheShard {
    h := fnv.New32a()
    h.Write([]byte(key))
    hash := h.Sum32()
    return c.shards[hash&c.shardMask]
}

func (c *ShardedCache) Get(key string) (interface{}, bool) {
    shard := c.getShard(key)
    shard.mu.RLock()
    defer shard.mu.RUnlock()
    
    value, ok := shard.entries[key]
    return value, ok
}

func (c *ShardedCache) Put(key string, value interface{}) {
    shard := c.getShard(key)
    shard.mu.Lock()
    defer shard.mu.Unlock()
    
    shard.entries[key] = value
}

// Benefits:
// - Reduces lock contention
// - Better concurrent write performance
// - Scalable to many cores
```

### Read-Write Lock Pattern

```go
type RWCache struct {
    entries map[string]interface{}
    mu      sync.RWMutex  // Not sync.Mutex
}

func (c *RWCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()  // Multiple readers allowed
    defer c.mu.RUnlock()
    
    value, ok := c.entries[key]
    return value, ok
}

func (c *RWCache) Put(key string, value interface{}) {
    c.mu.Lock()  // Exclusive access for writes
    defer c.mu.Unlock()
    
    c.entries[key] = value
}

// Use when: Reads >> Writes (typical for caches)
```

---

## Cache Libraries

### Popular Go Cache Libraries

**1. hashicorp/golang-lru**
```go
import "github.com/hashicorp/golang-lru"

cache, _ := lru.New(1000)
cache.Add("key", value)
value, ok := cache.Get("key")
cache.Remove("key")

// Pros: Simple, battle-tested, good performance
// Cons: No TTL support, basic features only
```

**2. patrickmn/go-cache**
```go
import "github.com/patrickmn/go-cache"

c := cache.New(5*time.Minute, 10*time.Minute)
c.Set("key", value, cache.DefaultExpiration)
value, found := c.Get("key")

// Pros: TTL support, simple API
// Cons: No size limits, uses cleanup goroutine
```

**3. allegro/bigcache**
```go
import "github.com/allegro/bigcache"

config := bigcache.DefaultConfig(10 * time.Minute)
cache, _ := bigcache.NewBigCache(config)
cache.Set("key", []byte("value"))
entry, _ := cache.Get("key")

// Pros: Very fast, low GC overhead, byte-oriented
// Cons: Complex API, only []byte values
```

**4. dgraph-io/ristretto**
```go
import "github.com/dgraph-io/ristretto"

cache, _ := ristretto.NewCache(&ristretto.Config{
    NumCounters: 1e7,
    MaxCost:     1 << 30,  // 1 GB
    BufferItems: 64,
})
cache.Set("key", value, 1)
value, ok := cache.Get("key")

// Pros: Excellent hit ratio, cost-based eviction
// Cons: Complex configuration, higher memory overhead
```

### Library Comparison

```
Library          | Type | TTL | Size Limit | Concurrent | Performance
-----------------|------|-----|------------|------------|------------
golang-lru       | LRU  | No  | Count      | Needs lock | Good
go-cache         | TTL  | Yes | No         | Built-in   | Good
bigcache         | TTL  | Yes | Yes        | Built-in   | Excellent
ristretto        | LRU  | Yes | Cost       | Built-in   | Excellent
sync.Map         | None | No  | No         | Built-in   | Good reads

Recommendation:
- Simple LRU: golang-lru
- TTL-based: go-cache
- High performance: bigcache or ristretto
- Custom logic: Build your own
```

---

## Performance Considerations

### Benchmark Comparison

```go
package main

import (
    "testing"
)

// Unbounded map
func BenchmarkUnboundedMap(b *testing.B) {
    m := make(map[int]int)
    
    for i := 0; i < b.N; i++ {
        m[i] = i
    }
}
// Result: ~50 ns/op (fast but grows forever)

// LRU cache
func BenchmarkLRUCache(b *testing.B) {
    cache, _ := lru.New(10000)
    
    for i := 0; i < b.N; i++ {
        cache.Add(i, i)
    }
}
// Result: ~200 ns/op (slower but bounded)

// Sharded cache
func BenchmarkShardedCache(b *testing.B) {
    cache := NewShardedCache(16)
    
    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            cache.Put(i, i)
            i++
        }
    })
}
// Result: ~150 ns/op (good concurrent performance)
```

### Trade-offs

```
Metric          | Unbounded | LRU   | TTL   | Sharded
----------------|-----------|-------|-------|----------
Memory          | Unbounded | Fixed | Varies| Fixed
Lookup Speed    | O(1) fast | O(1)  | O(1)  | O(1)
Insert Speed    | O(1) fast | O(1)  | O(1)  | O(1)
Concurrency     | Poor      | Fair  | Fair  | Excellent
Memory Safety   | ❌ Unsafe | ✅ Safe| ✅ Safe| ✅ Safe
Complexity      | Low       | Medium| Medium| High

Choose based on your priorities:
- Need speed only: Unbounded (not production-safe!)
- Need bounded: LRU
- Need TTL: TTL cache
- Need high concurrency: Sharded
```

---

## Summary

### Key Patterns

1. **LRU**: Evict least recently used items
2. **TTL**: Expire items after time duration
3. **Size-based**: Track actual memory usage
4. **Sharded**: Reduce lock contention
5. **Hybrid**: Combine multiple strategies

### Decision Matrix

```
Question: What's your primary concern?

Memory Growth → Use LRU or Size-based
  ├─ Fixed entry count → LRU
  └─ Fixed memory size → Size-based

Time-based expiry → Use TTL
  ├─ Sessions → TTL
  └─ Auth tokens → TTL

High concurrency → Use Sharding
  ├─ Many writers → Sharded + LRU
  └─ Mostly readers → RWMutex + LRU

Complex requirements → Use Library
  ├─ Hit ratio critical → ristretto
  ├─ Performance critical → bigcache
  └─ Simple needs → golang-lru
```

### Implementation Checklist

When implementing a cache:
- [ ] Define maximum size (count or bytes)
- [ ] Choose eviction policy (LRU, LFU, TTL)
- [ ] Add concurrency protection (mutex, sharding)
- [ ] Implement metrics (hits, misses, evictions)
- [ ] Add monitoring (size, memory usage)
- [ ] Test edge cases (full cache, expired entries)
- [ ] Document behavior (thread-safe?, TTL?, etc.)

### Common Mistakes to Avoid

```go
// ❌ No size limit
var cache = make(map[string]*Data)

// ❌ No cleanup for TTL
type Entry struct {
    Value     interface{}
    ExpiresAt time.Time
}
var cache = make(map[string]*Entry)
// Expired entries never removed!

// ❌ No concurrency protection
type Cache struct {
    m map[string]interface{}
}
func (c *Cache) Get(key string) interface{} {
    return c.m[key]  // Race condition!
}

// ❌ Evicting on Get instead of Put
func (c *Cache) Get(key string) interface{} {
    if len(c.m) > c.maxSize {
        c.evict()  // Wrong place!
    }
    return c.m[key]
}
```

---

## Next Steps

- **Learn eviction algorithms**: Read [Eviction Strategies](./05-eviction-strategies.md)
- **Visualize behavior**: Read [Memory Growth Diagrams](./06-memory-growth-diagrams.md)
- **Study real cases**: Read [Production Examples](./07-production-examples.md)
- **Understand detection**: Read [Detection Methods](./05-detection-methods.md)

---

**Return to**: [Long-Lived References README](../README.md)
