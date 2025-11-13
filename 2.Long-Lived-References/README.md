# Long-Lived References - Memory-Based Leaks

## Quick Links

- [← Back to Root](../)
- [← Previous: Goroutine Leaks](../1.Goroutine-Leaks-Most-Common/)
- [Next: Resource Leaks →](../3.Resource-Leaks/)
- [Conceptual Explanation](#conceptual-explanation)
- [How to Detect](#how-to-detect-it)
- [Examples](#examples)
- [Resources](#resources--learning-materials)

---

## Conceptual Explanation

### What is a Long-Lived Reference Leak?

A long-lived reference leak occurs when objects remain in memory longer than necessary because references to them are retained even after they're no longer needed. Unlike goroutine leaks, these are true memory leaks where heap objects cannot be garbage collected.

The Go garbage collector can only reclaim memory for objects that have no references pointing to them. When you maintain references to objects (in caches, global variables, or long-lived data structures) that you no longer need, the GC cannot free that memory, leading to gradual memory growth.

**Key Characteristics**:
- Objects accumulate in heap memory over time
- Memory grows but never decreases (or decreases slowly)
- Application doesn't crash immediately but degrades gradually
- GC runs frequently but cannot reclaim the leaked memory
- Often appears as "slow memory leak" taking days or weeks to manifest

**Common Causes**:
1. **Unbounded caches** without eviction policies
2. **Slice reslicing** keeping the entire underlying array alive
3. **Global variables** holding references to temporary data
4. **Event listeners** not being removed after use
5. **Maps** that grow indefinitely without cleanup

### Why Does It Happen?

**1. Unbounded Caches**

Caches improve performance by storing frequently accessed data, but without limits they grow forever:

```go
var cache = make(map[string]*LargeObject)

func getData(key string) *LargeObject {
    if obj, ok := cache[key]; ok {
        return obj
    }
    
    obj := fetchFromDatabase(key)
    cache[key] = obj  // Cache grows forever
    return obj
}
```

After processing 1 million unique keys, the cache holds 1 million objects. If each object is 1 KB, that's 1 GB of memory that cannot be reclaimed.

**2. Slice Reslicing Trap**

When you reslice a slice, the new slice shares the underlying array with the original:

```go
func processFile(filename string) []byte {
    data := readFile(filename)  // 100 MB file
    header := data[:1024]       // Keep only first 1KB
    return header               // But entire 100 MB stays in memory!
}
```

The `header` slice references the underlying 100 MB array, preventing garbage collection even though you only need 1 KB.

**3. Global State Accumulation**

Global variables live for the entire program lifetime:

```go
var processedRequests []*Request

func handleRequest(req *Request) {
    // Process request
    processRequest(req)
    
    // Store for analytics
    processedRequests = append(processedRequests, req)
    // This grows forever, holding all requests in memory
}
```

### Real-World Scenarios

**Scenario 1: In-Memory Session Store**

A web application stores user sessions in memory:

```go
var sessions = make(map[string]*Session)

func createSession(userID string) {
    session := &Session{
        UserID:    userID,
        CreatedAt: time.Now(),
        Data:      make(map[string]interface{}),
    }
    sessions[userID] = session
    // Sessions never expire or get cleaned up
}
```

Over months, millions of expired sessions accumulate, consuming gigabytes of memory.

**Scenario 2: Metrics Aggregation**

A monitoring service collects metrics:

```go
var metrics = make(map[string][]float64)

func recordMetric(name string, value float64) {
    metrics[name] = append(metrics[name], value)
    // Every metric value since startup is kept in memory
}
```

After weeks of operation, millions of data points are retained when only recent values are needed.

**Scenario 3: Image Processing Service**

An image service processes and caches thumbnails:

```go
var thumbnailCache = make(map[string][]byte)

func getThumbnail(imageID string) []byte {
    if thumb, ok := thumbnailCache[imageID]; ok {
        return thumb
    }
    
    thumb := generateThumbnail(imageID)
    thumbnailCache[imageID] = thumb
    return thumb
}
```

Processing 100,000 images at 50 KB per thumbnail = 5 GB of cached data that never expires.

---

## Technical Deep Dive

For in-depth understanding:

- [Memory Model Explanation](./resources/01-memory-model-explanation.md) - How Go memory management works
- [GC Behavior](./resources/02-gc-behavior.md) - Understanding the garbage collector
- [Slice Internals](./resources/03-slice-internals.md) - How slices reference arrays
- [Cache Patterns](./resources/04-cache-patterns.md) - Proper cache implementation

---

## How to Detect It

### Specific Metrics

**Primary Indicators**:
- Growing heap allocation over time
- High memory usage relative to data being processed
- GC running frequently but not reclaiming much memory
- Memory that grows but never decreases

**Secondary Indicators**:
- Increasing number of heap objects
- Large maps or slices in heap profiles
- Performance degradation over time
- Eventually: OOM (Out of Memory) errors

### Tools to Use

**1. Heap Profiling**

```bash
# Collect heap profile
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# View in browser
go tool pprof -http=:8081 heap.pprof

# Or command line
go tool pprof heap.pprof
(pprof) top
(pprof) list functionName
```

**2. Memory Stats**

```go
var m runtime.MemStats
runtime.ReadMemStats(&m)

fmt.Printf("Alloc = %v MB", m.Alloc / 1024 / 1024)
fmt.Printf("TotalAlloc = %v MB", m.TotalAlloc / 1024 / 1024)
fmt.Printf("Sys = %v MB", m.Sys / 1024 / 1024)
fmt.Printf("NumGC = %v\n", m.NumGC)
```

**3. Allocation Profiling**

```bash
curl http://localhost:6060/debug/pprof/allocs > allocs.pprof
go tool pprof -http=:8081 allocs.pprof
```

### Expected Values

**Healthy Application**:
- Heap size stabilizes after warmup
- Memory usage proportional to active data
- GC reclaims most garbage
- Memory pattern: sawtooth (allocate → GC → reclaim → repeat)

**Leaking Application**:
- Heap size grows monotonically
- Memory usage exceeds expected bounds
- GC reclaims little memory
- Memory pattern: steady upward trend

**Detection Thresholds**:
- **Warning**: Heap growing > 10% per hour during steady state
- **Critical**: Heap doubled without corresponding workload increase
- **Emergency**: Approaching system memory limits

More details: [Detection Methods](./resources/05-detection-methods.md)

---

## Examples

### Running Cache Leak Example

Demonstrates unbounded cache growth:

```bash
cd 2.Long-Lived-References
go run example_cache.go
```

**Expected Output**:
```
[START] Heap Alloc: 0 MB, Objects cached: 0
[AFTER 2s] Heap Alloc: 48 MB, Objects cached: 10000
[AFTER 4s] Heap Alloc: 96 MB, Objects cached: 20000
[AFTER 6s] Heap Alloc: 144 MB, Objects cached: 30000
```

**What's Happening**:
- Caches 5000 objects/second
- Each object is ~5 KB
- No eviction policy
- Memory grows: 5000 × 5 KB = 25 MB/second
- After 1 minute: 1.5 GB consumed

### Running Fixed Cache Example

Shows proper LRU cache with size limits:

```bash
go run fixed_cache.go
```

**Expected Output**:
```
[START] Heap Alloc: 0 MB, Objects cached: 0
[AFTER 2s] Heap Alloc: 12 MB, Objects cached: 1000
[AFTER 4s] Heap Alloc: 12 MB, Objects cached: 1000
[AFTER 6s] Heap Alloc: 12 MB, Objects cached: 1000
```

**What's Different**:
- LRU eviction policy
- Maximum 1000 items
- Old items removed automatically
- Memory stabilizes at ~12 MB

### Running Slice Reslicing Example

Demonstrates the slice reslicing memory trap:

```bash
go run example_reslicing.go
```

**Expected Output**:
```
Processing 100 files (10 MB each)...
[AFTER Processing] Heap Alloc: 1000 MB
Kept only headers (1 KB each), but...
Full arrays still in memory!
```

### Running Fixed Reslicing Example

Shows proper slice copying:

```bash
go run fixed_reslicing.go
```

**Expected Output**:
```
Processing 100 files (10 MB each)...
[AFTER Processing] Heap Alloc: 0.1 MB
Headers properly copied, arrays freed
```

---

## Profiling Instructions

Comprehensive guide: [pprof Analysis](./pprof_analysis.md)

**Quick Reference**:

```bash
# 1. Start leaky cache example
go run example_cache.go &

# 2. Collect initial heap profile
curl http://localhost:6060/debug/pprof/heap > heap_start.pprof

# 3. Wait 30 seconds
sleep 30

# 4. Collect second profile
curl http://localhost:6060/debug/pprof/heap > heap_end.pprof

# 5. Compare to see growth
go tool pprof -base=heap_start.pprof heap_end.pprof

# 6. View in browser
go tool pprof -http=:8081 heap_end.pprof
```

---

## Resources & Learning Materials

### Core Concepts

1. [Memory Model Explanation](./resources/01-memory-model-explanation.md)
   - Go memory allocation
   - Stack vs heap
   - Escape analysis
   - Read time: 15 minutes

2. [GC Behavior](./resources/02-gc-behavior.md)
   - How the garbage collector works
   - Mark and sweep algorithm
   - GC tuning
   - Why leaked objects aren't collected
   - Read time: 20 minutes

3. [Slice Internals](./resources/03-slice-internals.md)
   - Slice structure and backing arrays
   - The reslicing trap
   - When to copy vs reference
   - Read time: 15 minutes

### Patterns & Solutions

4. [Cache Patterns](./resources/04-cache-patterns.md)
   - LRU cache implementation
   - TTL-based eviction
   - Size-based limits
   - Concurrent-safe caches
   - Read time: 25 minutes

5. [Eviction Strategies](./resources/05-eviction-strategies.md)
   - LRU (Least Recently Used)
   - LFU (Least Frequently Used)
   - TTL (Time To Live)
   - Size-based eviction
   - Read time: 20 minutes

### Visual Learning

6. [Memory Growth Diagrams](./resources/06-memory-growth-diagrams.md)
   - Heap growth patterns
   - GC behavior visualizations
   - Slice memory layouts
   - Cache eviction flows
   - Read time: 15 minutes

### Advanced Topics

7. [Production Examples](./resources/07-production-examples.md)
   - Real-world memory leak cases
   - Cache sizing strategies
   - Memory monitoring in production
   - Performance vs memory tradeoffs
   - Read time: 30 minutes

---

## Key Takeaways

1. **Long-lived references prevent GC** - The garbage collector cannot free objects that still have references

2. **Caches need eviction policies** - Unbounded caches will eventually consume all memory

3. **Slice reslicing is dangerous** - Small slices can keep large arrays alive; copy when needed

4. **Profile heap regularly** - Use heap profiles to identify growing data structures

5. **Set size limits** - Every cache, map, or collection should have a maximum size

6. **Monitor memory in production** - Track heap growth and set alerts

7. **Use specialized cache libraries** - Don't roll your own; use tested LRU/LFU implementations

---

## Related Leak Types

- [Goroutine Leaks](../1.Goroutine-Leaks-Most-Common/) - Goroutines holding references to heap objects
- [Resource Leaks](../3.Resource-Leaks/) - Resources that consume memory
- [Memory Growth Patterns](../visual-guides/memory-growth-patterns.md) - Identifying different growth patterns

---

**Next Steps**: Try the [Resource Leaks](../3.Resource-Leaks/) examples to learn about OS resource management.

