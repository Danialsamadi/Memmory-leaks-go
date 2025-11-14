# Eviction Strategies: Choosing the Right Cache Policy

**Read Time**: 20 minutes

**Prerequisites**: Understanding of [Cache Patterns](./04-cache-patterns.md)

**Related Topics**: 
- [Cache Patterns](./04-cache-patterns.md)
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Overview of Eviction Policies](#overview-of-eviction-policies)
2. [LRU (Least Recently Used)](#lru-least-recently-used)
3. [LFU (Least Frequently Used)](#lfu-least-frequently-used)
4. [TTL (Time To Live)](#ttl-time-to-live)
5. [FIFO (First In First Out)](#fifo-first-in-first-out)
6. [Random Eviction](#random-eviction)
7. [Size-Based Eviction](#size-based-eviction)
8. [Comparing Strategies](#comparing-strategies)
9. [Summary](#summary)

---

## Overview of Eviction Policies

### Why Eviction Matters

```
Cache without eviction:
Memory │      ╱
       │    ╱
       │  ╱
       │╱
       └──────────► Time
       Unbounded growth → OOM

Cache with eviction:
Memory │  ┌────────────
       │ ╱ (bounded)
       │╱
       └──────────► Time
       Bounded → Predictable
```

### Common Policies

| Policy | Evicts | Best For | Complexity |
|--------|--------|----------|------------|
| LRU | Least recently accessed | General purpose, temporal locality | O(1) |
| LFU | Least frequently accessed | Access patterns with hot items | O(log n) |
| TTL | Expired entries | Time-sensitive data (sessions, tokens) | O(1) |
| FIFO | Oldest entry | Simple, no access tracking needed | O(1) |
| Random | Random entry | Simplest, surprisingly effective | O(1) |
| Size | Largest entries | Memory optimization | O(n) |

---

## LRU (Least Recently Used)

### Concept

**Evict the item that hasn't been accessed for the longest time.**

**Assumption**: Recently accessed items are likely to be accessed again soon (temporal locality).

### How It Works

```
Cache state (max 3 items):

Step 1: Access A
  [A] (most recent)

Step 2: Access B
  [B, A]

Step 3: Access C
  [C, B, A] (cache full)

Step 4: Access A (hit)
  [A, C, B] (A moved to front)

Step 5: Access D (miss, cache full)
  [D, A, C] (B evicted - least recently used)

Step 6: Access B (miss)
  [B, D, A] (C evicted)
```

### Implementation

```go
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

type lruEntry struct {
    key   string
    value interface{}
}

func NewLRU(capacity int) *LRUCache {
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
        c.lruList.MoveToFront(elem)  // Mark as recently used
        return elem.Value.(*lruEntry).value, true
    }
    return nil, false
}

func (c *LRUCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if elem, ok := c.cache[key]; ok {
        c.lruList.MoveToFront(elem)
        elem.Value.(*lruEntry).value = value
        return
    }
    
    elem := c.lruList.PushFront(&lruEntry{key, value})
    c.cache[key] = elem
    
    if c.lruList.Len() > c.capacity {
        oldest := c.lruList.Back()
        if oldest != nil {
            c.lruList.Remove(oldest)
            delete(c.cache, oldest.Value.(*lruEntry).key)
        }
    }
}
```

### When to Use LRU

**✅ Good for:**
- Web page caching (recently viewed pages)
- Database query results
- API response caching
- General-purpose caching

**❌ Not ideal for:**
- Scan-resistant needed (sequential access patterns)
- All items accessed uniformly
- Very small cache (overhead not worth it)

### LRU Performance

```
Time Complexity:
  Get: O(1) - hash lookup + list move
  Put: O(1) - hash insert + list operations
  
Space Complexity:
  O(n) - map + doubly linked list
  
Overhead per entry:
  ~48 bytes (map entry + 2 list pointers)
```

---

## LFU (Least Frequently Used)

### Concept

**Evict the item that has been accessed the fewest times.**

**Assumption**: Frequently accessed items will continue to be accessed frequently.

### How It Works

```
Access pattern: A, B, C, A, A, D

Counter state (max 3 items):

After A, B, C:
  A: 1 access
  B: 1 access
  C: 1 access

After A, A (hits):
  A: 3 accesses
  B: 1 access
  C: 1 access

After D (miss, evict least frequent):
  A: 3 accesses
  D: 1 access
  C: 1 access  (B evicted, had 1 access)
```

### Implementation

```go
type LFUCache struct {
    capacity  int
    minFreq   int
    keyToVal  map[string]interface{}
    keyToFreq map[string]int
    freqToKeys map[int]map[string]bool
    mu        sync.Mutex
}

func NewLFU(capacity int) *LFUCache {
    return &LFUCache{
        capacity:   capacity,
        keyToVal:   make(map[string]interface{}),
        keyToFreq:  make(map[string]int),
        freqToKeys: make(map[int]map[string]bool),
    }
}

func (c *LFUCache) Get(key string) (interface{}, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    val, ok := c.keyToVal[key]
    if !ok {
        return nil, false
    }
    
    c.incrementFreq(key)
    return val, true
}

func (c *LFUCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.capacity == 0 {
        return
    }
    
    if _, ok := c.keyToVal[key]; ok {
        c.keyToVal[key] = value
        c.incrementFreq(key)
        return
    }
    
    if len(c.keyToVal) >= c.capacity {
        c.evictLFU()
    }
    
    c.keyToVal[key] = value
    c.keyToFreq[key] = 1
    c.minFreq = 1
    
    if c.freqToKeys[1] == nil {
        c.freqToKeys[1] = make(map[string]bool)
    }
    c.freqToKeys[1][key] = true
}

func (c *LFUCache) incrementFreq(key string) {
    freq := c.keyToFreq[key]
    delete(c.freqToKeys[freq], key)
    
    if len(c.freqToKeys[freq]) == 0 && freq == c.minFreq {
        c.minFreq++
    }
    
    c.keyToFreq[key] = freq + 1
    if c.freqToKeys[freq+1] == nil {
        c.freqToKeys[freq+1] = make(map[string]bool)
    }
    c.freqToKeys[freq+1][key] = true
}

func (c *LFUCache) evictLFU() {
    // Get any key from minFreq bucket
    for key := range c.freqToKeys[c.minFreq] {
        delete(c.freqToKeys[c.minFreq], key)
        delete(c.keyToFreq, key)
        delete(c.keyToVal, key)
        break
    }
}
```

### When to Use LFU

**✅ Good for:**
- Content Distribution Networks
- Database connection pools
- Popular item caching (products, articles)
- Workloads with "hot" items

**❌ Not ideal for:**
- Temporally clustered access (bursts)
- Access patterns change over time
- Cold start problem (new items disadvantaged)

---

## TTL (Time To Live)

### Concept

**Evict entries after a specified time period.**

**Assumption**: Data becomes stale after a certain duration.

### Implementation

```go
type TTLCache struct {
    entries map[string]*ttlEntry
    mu      sync.RWMutex
    ttl     time.Duration
}

type ttlEntry struct {
    value     interface{}
    expiresAt time.Time
}

func NewTTL(ttl time.Duration) *TTLCache {
    c := &TTLCache{
        entries: make(map[string]*ttlEntry),
        ttl:     ttl,
    }
    go c.cleanupLoop()
    return c
}

func (c *TTLCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    entry, ok := c.entries[key]
    if !ok || time.Now().After(entry.expiresAt) {
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
```

### When to Use TTL

**✅ Good for:**
- Session data
- Authentication tokens
- API rate limits
- Temporary data (OTP codes)
- DNS caching

**❌ Not ideal for:**
- Static data
- Unknown expiration time
- Strict memory limits needed

---

## FIFO (First In First Out)

### Concept

**Evict the oldest entry regardless of access patterns.**

**Simple queue behavior** - like a line at a store.

### Implementation

```go
type FIFOCache struct {
    capacity int
    cache    map[string]interface{}
    queue    []string
    mu       sync.Mutex
}

func NewFIFO(capacity int) *FIFOCache {
    return &FIFOCache{
        capacity: capacity,
        cache:    make(map[string]interface{}),
        queue:    make([]string, 0, capacity),
    }
}

func (c *FIFOCache) Get(key string) (interface{}, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    value, ok := c.cache[key]
    return value, ok
}

func (c *FIFOCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if _, ok := c.cache[key]; ok {
        c.cache[key] = value
        return
    }
    
    if len(c.cache) >= c.capacity {
        oldest := c.queue[0]
        c.queue = c.queue[1:]
        delete(c.cache, oldest)
    }
    
    c.cache[key] = value
    c.queue = append(c.queue, key)
}
```

### When to Use FIFO

**✅ Good for:**
- Simple caching needs
- Time-series data
- Log buffering
- Predictable access patterns

**❌ Not ideal for:**
- Temporal locality matters
- Hot item access patterns

---

## Random Eviction

### Concept

**Evict a random entry when cache is full.**

**Surprisingly effective** for many workloads!

### Implementation

```go
type RandomCache struct {
    capacity int
    cache    map[string]interface{}
    keys     []string
    mu       sync.Mutex
}

func (c *RandomCache) Put(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if len(c.cache) >= c.capacity {
        // Evict random entry
        idx := rand.Intn(len(c.keys))
        randomKey := c.keys[idx]
        delete(c.cache, randomKey)
        c.keys[idx] = c.keys[len(c.keys)-1]
        c.keys = c.keys[:len(c.keys)-1]
    }
    
    c.cache[key] = value
    c.keys = append(c.keys, key)
}
```

### When to Use Random

**✅ Good for:**
- Simplicity needed
- Uniform access patterns
- Low overhead critical
- Approximate LRU acceptable

**❌ Not ideal for:**
- Predictable behavior required
- Specific eviction logic needed

---

## Size-Based Eviction

### Concept

**Evict largest entries first** or **keep total size under limit**.

### Implementation

```go
type SizeCache struct {
    maxSize     int
    currentSize int
    cache       map[string]*sizeEntry
    mu          sync.Mutex
}

type sizeEntry struct {
    value interface{}
    size  int
}

func (c *SizeCache) Put(key string, value interface{}, size int) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Evict until we have space
    for c.currentSize+size > c.maxSize && len(c.cache) > 0 {
        c.evictLargest()
    }
    
    if c.currentSize+size > c.maxSize {
        return false  // Still doesn't fit
    }
    
    c.cache[key] = &sizeEntry{value, size}
    c.currentSize += size
    return true
}

func (c *SizeCache) evictLargest() {
    var largestKey string
    var largestSize int
    
    for key, entry := range c.cache {
        if entry.size > largestSize {
            largestSize = entry.size
            largestKey = key
        }
    }
    
    if largestKey != "" {
        delete(c.cache, largestKey)
        c.currentSize -= largestSize
    }
}
```

---

## Comparing Strategies

### Performance Comparison

```
Operation       | LRU   | LFU    | TTL   | FIFO  | Random
----------------|-------|--------|-------|-------|--------
Get             | O(1)  | O(1)   | O(1)  | O(1)  | O(1)
Put             | O(1)  | O(1)   | O(1)  | O(1)  | O(1)
Evict           | O(1)  | O(1)   | O(n)* | O(1)  | O(1)
Memory Overhead | High  | High   | Low   | Medium| Low
Complexity      | Medium| High   | Low   | Low   | Very Low

* O(n) for cleanup sweep, but amortized across many operations
```

### Hit Rate Comparison (Typical Workloads)

```
Workload Type          | LRU  | LFU  | TTL  | FIFO | Random
-----------------------|------|------|------|------|--------
Temporal locality      | 95%  | 80%  | 85%  | 70%  | 65%
Hot items              | 85%  | 95%  | 80%  | 75%  | 70%
Uniform random         | 50%  | 50%  | 45%  | 50%  | 50%
Sequential scan        | 20%  | 15%  | 25%  | 25%  | 35%
Time-sensitive data    | 70%  | 65%  | 95%  | 60%  | 55%

Note: Actual hit rates depend on:
- Cache size relative to working set
- Access pattern characteristics
- Specific implementation details
```

### Memory Usage

```
For 10,000 entries with 1 KB values:

Policy    | Data  | Overhead | Total
----------|-------|----------|--------
LRU       | 10 MB | ~480 KB  | 10.48 MB
LFU       | 10 MB | ~640 KB  | 10.64 MB
TTL       | 10 MB | ~160 KB  | 10.16 MB
FIFO      | 10 MB | ~160 KB  | 10.16 MB
Random    | 10 MB | ~160 KB  | 10.16 MB
Unbounded | 10 MB | ~80 KB   | 10.08 MB

Overhead includes:
- Map entries (hash buckets)
- List/heap structures
- Metadata (counters, timestamps)
```

---

## Summary

### Decision Matrix

```
Choose based on your workload:

Temporal Locality (recent reuse)
  → LRU

Hot Items (frequent reuse)
  → LFU

Time-Sensitive Data
  → TTL

Simplicity Needed
  → FIFO or Random

Memory Constraints
  → Size-Based + LRU

High Hit Rate Critical
  → LRU or LFU (test both)

Low Overhead Critical
  → Random or FIFO
```

### Hybrid Approaches

```go
// Combine LRU + TTL
type HybridCache struct {
    lru *LRUCache
    ttl time.Duration
}

// Each entry has:
// - Position in LRU (for access-based eviction)
// - Expiration time (for time-based eviction)

// Combine Size + LRU
type SizeLRU struct {
    maxSize     int
    currentSize int
    lru         *LRUCache
}

// Evict based on:
// - LRU policy (least recently used)
// - Until size is under limit
```

### Best Practices

1. **Profile your workload** before choosing
2. **Test hit rates** with production-like traffic
3. **Monitor eviction rates** to tune capacity
4. **Consider memory overhead** for small caches
5. **Use TTL** for time-sensitive data regardless of other policy
6. **Start simple** (LRU) and optimize if needed

### Common Mistakes

```go
// ❌ Wrong: No eviction policy
cache := make(map[string]interface{})

// ❌ Wrong: LRU on sequential scans
// (LRU thrashes on sequential access)
for i := 0; i < 1000000; i++ {
    lru.Get(fmt.Sprintf("seq_%d", i))
}

// ❌ Wrong: LFU without decay
// (Popular item stays forever even if no longer accessed)

// ✅ Correct: LRU for general purpose
cache := NewLRU(1000)

// ✅ Correct: TTL for sessions
cache := NewTTL(30 * time.Minute)

// ✅ Correct: Size-based for memory constraints
cache := NewSizeLRU(100 * 1024 * 1024)  // 100 MB max
```

---

## Next Steps

- **Visualize eviction**: Read [Memory Growth Diagrams](./06-memory-growth-diagrams.md)
- **Study real cases**: Read [Production Examples](./07-production-examples.md)
- **Implement caches**: Review [Cache Patterns](./04-cache-patterns.md)
- **Understand detection**: Read [Detection Methods](../pprof_analysis.md)

---

**Return to**: [Long-Lived References README](../README.md)
