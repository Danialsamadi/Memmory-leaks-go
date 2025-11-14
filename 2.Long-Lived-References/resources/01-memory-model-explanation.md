# Memory Model Explanation: Long-Lived References

**Read Time**: 20 minutes

**Prerequisites**: Basic understanding of Go memory management

**Related Topics**: 
- [GC Behavior](./02-gc-behavior.md)
- [Slice Internals](./03-slice-internals.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Go Memory Architecture](#go-memory-architecture)
2. [Stack vs Heap Allocation](#stack-vs-heap-allocation)
3. [Escape Analysis](#escape-analysis)
4. [Reference Reachability](#reference-reachability)
5. [The Long-Lived Reference Problem](#the-long-lived-reference-problem)
6. [Memory Leak Definition in Go](#memory-leak-definition-in-go)
7. [Summary](#summary)

---

## Go Memory Architecture

### The Two Memory Regions

Go programs use two primary memory regions:

```
┌─────────────────────────────────────┐
│        Process Memory Space         │
├─────────────────────────────────────┤
│  Stack (per goroutine)              │
│  - Fast allocation/deallocation     │
│  - Automatic cleanup                │
│  - Limited size (2KB-1GB)           │
│  - LIFO structure                   │
├─────────────────────────────────────┤
│  Heap (shared across goroutines)    │
│  - Slower allocation                │
│  - Requires GC                      │
│  - Unlimited (system memory)        │
│  - Complex data structures          │
└─────────────────────────────────────┘
```

### Why Two Regions?

**Stack Benefits**:
- Allocation: Just increment a pointer (nanoseconds)
- Deallocation: Automatic when function returns
- Cache-friendly: Sequential memory access
- Thread-safe: Each goroutine has its own stack

**Heap Necessity**:
- Objects that outlive function calls
- Objects too large for stack
- Objects whose size isn't known at compile time
- Objects shared between goroutines

---

## Stack vs Heap Allocation

### Stack Allocation Example

```go
func stackExample() {
    // All these live on the stack
    x := 42                    // int
    y := "hello"               // string (small)
    arr := [3]int{1, 2, 3}    // array
    
    compute(x, y, arr)
    
    // When function returns:
    // Stack pointer decreases
    // All variables instantly "freed"
    // No GC involved
}
```

**Stack Memory Lifecycle**:
```
Function Entry:    Stack grows ↓
  ├─ x (8 bytes)
  ├─ y (16 bytes)
  └─ arr (24 bytes)

Function Exit:     Stack shrinks ↑
  All memory instantly available for reuse
```

### Heap Allocation Example

```go
func heapExample() *Data {
    // This allocates on the heap
    data := &Data{
        Value: 42,
        Name:  "important",
    }
    
    // Return pointer to caller
    return data
    
    // data escapes to heap because:
    // 1. We return a pointer to it
    // 2. Caller will use it after this function returns
    // 3. Stack frame will be destroyed
}
```

**Heap Memory Lifecycle**:
```
Allocation:
  1. Request memory from heap
  2. Find suitable block (malloc-like)
  3. Update heap metadata
  4. Return pointer

Usage:
  Object lives in heap
  Referenced by pointer on stack

Deallocation:
  1. GC marks object as unreachable
  2. GC sweeps and reclaims memory
  3. Memory available for future allocations
```

### Performance Comparison

```go
func stackVsHeap() {
    // FAST: Stack allocation
    for i := 0; i < 1_000_000; i++ {
        x := i * 2  // Stack: ~1ns per allocation
    }
    
    // SLOW: Heap allocation
    for i := 0; i < 1_000_000; i++ {
        x := new(int)  // Heap: ~50-100ns per allocation
        *x = i * 2
    }
}
```

**Benchmark Results** (typical Go 1.20+):
- Stack allocation: **1-2 ns/op**
- Heap allocation: **50-100 ns/op**
- Heap allocation with GC pressure: **200+ ns/op**

---

## Escape Analysis

### What Is Escape Analysis?

The Go compiler analyzes your code to determine whether variables can live on the stack or must "escape" to the heap.

**Goal**: Keep as much on the stack as possible for performance.

### Rules for Escape Analysis

A variable **escapes to the heap** if:

1. **Returned from function**:
```go
func escape1() *int {
    x := 42
    return &x  // ❌ x escapes (returned)
}
```

2. **Stored in a heap-allocated structure**:
```go
type Container struct {
    data *int
}

func escape2() {
    x := 42
    c := &Container{data: &x}  // ❌ x escapes (c is on heap)
    globalVar = c
}
```

3. **Too large for stack**:
```go
func escape3() {
    // ❌ Escapes: 10MB too large for stack
    bigArray := make([]byte, 10*1024*1024)
    process(bigArray)
}
```

4. **Sent to a channel**:
```go
func escape4(ch chan *int) {
    x := 42
    ch <- &x  // ❌ x escapes (sent to channel)
}
```

5. **Captured by closure stored in heap**:
```go
func escape5() func() int {
    x := 42
    return func() int {
        return x  // ❌ x escapes (captured by returned closure)
    }
}
```

### Viewing Escape Analysis

```bash
# Run escape analysis on your code
go build -gcflags='-m' yourfile.go

# Output example:
# ./main.go:10:2: moved to heap: x
# ./main.go:15:6: can inline stackOnly
# ./main.go:20:2: x does not escape
```

**Detailed Analysis**:
```bash
# See escape analysis decisions
go build -gcflags='-m -m' yourfile.go

# See even more detail
go build -gcflags='-m -m -m' yourfile.go
```

### Escape Analysis Example

```go
package main

func noEscape() {
    x := 42          // Stack
    y := &x          // Stack (y is local)
    process(*y)      // Stack (passed by value)
}

func escapeReturn() *int {
    x := 42
    return &x        // Heap (returned)
}

func escapeInterface(i interface{}) {
    x := 42
    i = x            // Heap (interface requires heap)
}

func escapeSlice() {
    x := make([]int, 0, 10)  // Stack (small, doesn't escape)
    x = append(x, 1, 2, 3)
}

func escapeSliceLarge() {
    x := make([]int, 0, 10000)  // Heap (large)
    x = append(x, 1, 2, 3)
}
```

**Running escape analysis**:
```bash
$ go build -gcflags='-m' example.go
# command-line-arguments
./example.go:7:2: x does not escape
./example.go:13:2: moved to heap: x
./example.go:18:2: x escapes to heap
./example.go:22:11: make([]int, 0, 10) does not escape
./example.go:27:11: make([]int, 0, 10000) escapes to heap
```

---

## Reference Reachability

### The GC Root Set

The garbage collector starts from **roots** - memory locations that are always considered "live":

```
Roots (Always Reachable):
├─ Global variables
├─ Goroutine stacks (all local variables)
├─ Registers (CPU registers)
└─ Finalizer queue

An object is REACHABLE if there's a reference chain:
  Root → Object1 → Object2 → ... → ObjectN

An object is GARBAGE if NO path exists from any root.
```

### Reachability Example

```go
type Node struct {
    Value int
    Next  *Node
}

var globalHead *Node  // Root: global variable

func example() {
    // Scenario 1: Reachable
    node1 := &Node{Value: 1}
    node2 := &Node{Value: 2}
    node1.Next = node2
    
    globalHead = node1
    
    // Reachability chain:
    // globalHead (root) → node1 → node2
    // Both node1 and node2 are reachable
}

func example2() {
    // Scenario 2: Unreachable (garbage)
    node1 := &Node{Value: 1}
    node2 := &Node{Value: 2}
    node1.Next = node2
    
    // Function returns WITHOUT storing anywhere
    // node1 and node2 become unreachable
    // GC will collect them
}

func example3() {
    // Scenario 3: Partially reachable
    node1 := &Node{Value: 1}
    node2 := &Node{Value: 2}
    node3 := &Node{Value: 3}
    node1.Next = node2
    node2.Next = node3
    
    globalHead = node1
    
    // Later: break the chain
    node1.Next = nil
    
    // Now:
    // - node1 is reachable (via globalHead)
    // - node2 and node3 are UNREACHABLE (chain broken)
    // - GC will collect node2 and node3
}
```

### Visual Reachability

```
Before breaking chain:
┌─────────────┐
│ globalHead  │ (Root)
└──────┬──────┘
       │
       ▼
  ┌────────┐     ┌────────┐     ┌────────┐
  │ node1  │────▶│ node2  │────▶│ node3  │
  │ Value:1│     │ Value:2│     │ Value:3│
  └────────┘     └────────┘     └────────┘
  REACHABLE      REACHABLE      REACHABLE


After node1.Next = nil:
┌─────────────┐
│ globalHead  │ (Root)
└──────┬──────┘
       │
       ▼
  ┌────────┐     ┌────────┐     ┌────────┐
  │ node1  │  ╳  │ node2  │────▶│ node3  │
  │ Value:1│     │ Value:2│     │ Value:3│
  └────────┘     └────────┘     └────────┘
  REACHABLE      GARBAGE        GARBAGE
```

---

## The Long-Lived Reference Problem

### What Makes a Reference "Long-Lived"?

A reference is **long-lived** if it persists for:
- The entire program lifetime (global variables)
- A very long time relative to object's utility (caches)
- Longer than the object is actually needed

### The Core Problem

```go
// Problem: globalCache is a root that lives forever
var globalCache = make(map[string]*Data)

func process(key string) {
    data := expensiveFetch(key)
    
    // This creates a long-lived reference:
    // globalCache (root) → Data object
    globalCache[key] = data
    
    // Even if we never access this key again,
    // the Data object can NEVER be garbage collected
    // because globalCache holds a reference to it
}
```

### Reachability Chain

```
Program Start:
  globalCache (root) → {} (empty map)

After 1st call:
  globalCache (root) → {key1: Data1}
                           ↓
                        Data1 (LIVE)

After 1000th call:
  globalCache (root) → {key1: Data1, key2: Data2, ..., key1000: Data1000}
                           ↓       ↓                        ↓
                        Data1   Data2   ...              Data1000
                        (LIVE)  (LIVE)                   (LIVE)

Problem: ALL 1000 Data objects are reachable and cannot be collected,
even if we only actually need the last 10.
```

### Why GC Can't Help

```go
var cache = make(map[string]*HugeData)

func leak() {
    for i := 0; i < 1_000_000; i++ {
        key := fmt.Sprintf("key_%d", i)
        cache[key] = &HugeData{/* 1MB each */}
    }
    
    // GC runs periodically but:
    // 1. Scans from roots (including cache)
    // 2. Finds cache is reachable
    // 3. Finds all map entries are reachable
    // 4. Finds all HugeData objects are reachable
    // 5. Concludes: nothing can be collected
    //
    // Result: 1 GB of memory "leaked" even though
    // we may never access most of these keys again
}
```

### The Solution Pattern

```go
// Solution: Break the reachability chain

// Option 1: Limit cache size
func bounded() {
    cache, _ := lru.New(1000)  // Max 1000 entries
    
    for i := 0; i < 1_000_000; i++ {
        key := fmt.Sprintf("key_%d", i)
        cache.Add(key, &HugeData{})
        
        // When cache exceeds 1000:
        // - Oldest entry is evicted
        // - Reference is removed from cache
        // - Object becomes unreachable
        // - GC can collect it
    }
}

// Option 2: Periodic cleanup
func periodic() {
    cache := make(map[string]*TimedData)
    
    go func() {
        ticker := time.NewTicker(1 * time.Minute)
        defer ticker.Stop()
        
        for range ticker.C {
            now := time.Now()
            for key, data := range cache {
                if now.After(data.ExpiresAt) {
                    delete(cache, key)
                    // Reference removed
                    // Object becomes unreachable
                    // GC can collect it
                }
            }
        }
    }()
}

// Option 3: Explicit removal
func explicit(cache map[string]*Data, key string) {
    // When we know we're done with an object
    delete(cache, key)
    
    // This breaks the reference chain:
    // Before: cache → Data (reachable)
    // After:  cache → (nothing) | Data (unreachable, collectible)
}
```

---

## Memory Leak Definition in Go

### Traditional Memory Leak

In languages like C/C++:
```c
void leak() {
    int* ptr = malloc(1024);
    // Forgot to call free(ptr)
    // Memory is LOST - no pointer to it anymore
}
```

The memory is **unrecoverable** - there's no pointer to free it.

### Go "Memory Leak"

```go
func goLeak() {
    data := make([]byte, 1024)
    globalSlice = append(globalSlice, data)
    // Memory is NOT lost - we have a reference
    // But we're not using it and can't recover it
}
```

The memory is **recoverable** (we could delete from globalSlice), but **practically leaked** because:
1. We maintain the reference unintentionally
2. The object stays in memory indefinitely
3. Memory usage grows unbounded
4. GC can't help because reference exists

### The Accurate Term

In Go, these are more accurately called:
- **Unintentional memory retention**
- **Reference leaks**
- **Unbounded memory growth**

But "memory leak" is the colloquial term because the **effect is identical**:
- Memory grows over time
- Application performance degrades
- Eventually: Out of memory

---

## Summary

### Key Concepts

1. **Stack vs Heap**:
   - Stack: Fast, automatic, limited
   - Heap: Slower, GC-managed, unlimited

2. **Escape Analysis**:
   - Compiler decides stack vs heap
   - Optimize by keeping data on stack
   - Use `-gcflags='-m'` to verify

3. **Reachability**:
   - Objects reachable from roots stay in memory
   - GC collects unreachable objects
   - Long-lived roots prevent collection

4. **Long-Lived References**:
   - Persistent references (globals, caches) keep objects alive
   - Objects can't be collected even if unused
   - Memory grows unbounded without cleanup

5. **Go Memory Leaks**:
   - Not traditional "lost memory"
   - Unintentional reference retention
   - Same practical effect: unbounded growth

### Mental Model

```
Memory Management Hierarchy:

1. Allocation
   ├─ Stack (automatic)
   └─ Heap (GC-managed)

2. References
   ├─ Roots (always live)
   └─ Reachable from roots

3. Lifetime
   ├─ Short-lived (local variables)
   └─ Long-lived (globals, caches)

4. Problem
   └─ Long-lived references to objects
      that should be short-lived
```

### Prevention Checklist

When storing references, ask:
- [ ] Is this reference in a global variable?
- [ ] Does this collection (map/slice) have a size limit?
- [ ] Is there a cleanup mechanism (TTL, LRU)?
- [ ] How long will this reference persist?
- [ ] Can this grow unbounded in production?

---

## Next Steps

- **Understand GC behavior**: Read [GC Behavior](./02-gc-behavior.md)
- **Learn slice pitfalls**: Read [Slice Internals](./03-slice-internals.md)
- **Study cache patterns**: Read [Cache Patterns](./04-cache-patterns.md)
- **See real examples**: Read [Production Examples](./07-production-examples.md)

---

**Return to**: [Long-Lived References README](../README.md)
