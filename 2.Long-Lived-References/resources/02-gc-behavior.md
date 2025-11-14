# GC Behavior: Understanding Go's Garbage Collector

**Read Time**: 25 minutes

**Prerequisites**: Understanding of [Memory Model](./01-memory-model-explanation.md)

**Related Topics**: 
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [Slice Internals](./03-slice-internals.md)
- [Detection Methods](./05-detection-methods.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [How Go's GC Works](#how-gos-gc-works)
2. [The Mark and Sweep Algorithm](#the-mark-and-sweep-algorithm)
3. [GC Phases and Timing](#gc-phases-and-timing)
4. [Why Leaked Objects Aren't Collected](#why-leaked-objects-arent-collected)
5. [GC Metrics and Tuning](#gc-metrics-and-tuning)
6. [Write Barriers and STW](#write-barriers-and-stw)
7. [GC Behavior with Long-Lived References](#gc-behavior-with-long-lived-references)
8. [Summary](#summary)

---

## How Go's GC Works

### High-Level Overview

Go uses a **concurrent, tri-color, mark-and-sweep** garbage collector:

```
┌─────────────────────────────────────────┐
│     Go Garbage Collector Design         │
├─────────────────────────────────────────┤
│  Type: Concurrent Mark-and-Sweep        │
│  Algorithm: Tri-color marking           │
│  Goal: Low latency (< 1ms pause)        │
│  Trade-off: Some CPU overhead           │
└─────────────────────────────────────────┘
```

**Key Characteristics**:
- **Concurrent**: GC runs alongside application (mostly)
- **Non-moving**: Objects don't move in memory (no compaction)
- **Non-generational**: Scans entire heap each cycle (Go 1.21+)
- **Conservative**: When uncertain, keeps objects alive

### Design Goals

1. **Low Latency** (Primary):
   - Sub-millisecond pause times
   - Target: < 500 microseconds for stop-the-world phases
   - Critical for interactive applications

2. **Throughput** (Secondary):
   - Minimize CPU overhead
   - Run concurrently with application
   - ~25% CPU overhead during GC

3. **Simplicity** (Tertiary):
   - Predictable behavior
   - Fewer tuning knobs than other GCs
   - Easier to reason about

---

## The Mark and Sweep Algorithm

### The Tri-Color Abstraction

The GC uses three conceptual "colors" to track objects:

```
┌──────────┐
│  WHITE   │ ← Not yet visited (initially all objects)
└──────────┘

┌──────────┐
│   GRAY   │ ← Visited, but children not yet scanned
└──────────┘

┌──────────┐
│  BLACK   │ ← Visited, and all children scanned
└──────────┘

Algorithm:
1. Start: All objects WHITE
2. Mark roots GRAY
3. Process GRAY objects:
   - Scan for references
   - Mark referenced objects GRAY
   - Mark current object BLACK
4. Repeat until no GRAY objects
5. All WHITE objects are garbage
6. Sweep phase: Reclaim WHITE objects
```

### Step-by-Step Example

```go
type Node struct {
    Value int
    Next  *Node
}

var root *Node  // GC root (global)

func example() {
    // Create a linked list
    root = &Node{Value: 1}
    root.Next = &Node{Value: 2}
    root.Next.Next = &Node{Value: 3}
}
```

**GC Cycle Visualization**:

```
Initial State (before GC):
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│ (orphaned)
│WHITE │    │WHITE │    │WHITE │    │WHITE │
└──────┘    └──────┘    └──────┘    └──────┘
   ▲
   │
┌──────┐
│ root │ (GC Root)
└──────┘


Phase 1: Mark roots
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│
│ GRAY │    │WHITE │    │WHITE │    │WHITE │
└──────┘    └──────┘    └──────┘    └──────┘
   ▲
   │
┌──────┐
│ root │ (processed)
└──────┘


Phase 2: Process Node 1
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│
│BLACK │    │ GRAY │    │WHITE │    │WHITE │
└──────┘    └──────┘    └──────┘    └──────┘
(done)      (found via Next)


Phase 3: Process Node 2
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│
│BLACK │    │BLACK │    │ GRAY │    │WHITE │
└──────┘    └──────┘    └──────┘    └──────┘
           (done)       (found via Next)


Phase 4: Process Node 3
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│
│BLACK │    │BLACK │    │BLACK │    │WHITE │
└──────┘    └──────┘    └──────┘    └──────┘
                        (done, no children)


Phase 5: Sweep
┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐
│Node 1│───▶│Node 2│───▶│Node 3│    │Node X│ ← GARBAGE
│BLACK │    │BLACK │    │BLACK │    │WHITE │   (reclaimed)
└──────┘    └──────┘    └──────┘    └──────┘
(keep)      (keep)      (keep)         (free)
```

### The Sweep Phase

After marking, the GC sweeps through memory:

```go
for each memory block:
    if block.color == WHITE:
        // This object was not marked
        // No references to it exist
        reclaim(block)  // Add to free list
    else:
        // Object is reachable
        block.color = WHITE  // Reset for next cycle
        keep(block)
```

---

## GC Phases and Timing

### The Four Phases

```
┌────────────────────────────────────────────────────────────┐
│                    GC Cycle Timeline                       │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  1. Mark Setup (STW)                                       │
│     ├─ Stop the world                                      │
│     ├─ Enable write barriers                               │
│     └─ Scan stacks, globals                                │
│     Duration: ~100-500 μs                                  │
│                                                            │
│  2. Concurrent Mark                                        │
│     ├─ Resume application                                  │
│     ├─ Mark reachable objects                              │
│     ├─ Runs concurrently with app                          │
│     └─ Uses ~25% of CPU                                    │
│     Duration: varies (1-100ms typical)                     │
│                                                            │
│  3. Mark Termination (STW)                                 │
│     ├─ Stop the world again                                │
│     ├─ Finish marking                                      │
│     └─ Disable write barriers                              │
│     Duration: ~100-500 μs                                  │
│                                                            │
│  4. Concurrent Sweep                                       │
│     ├─ Resume application                                  │
│     ├─ Reclaim white objects                               │
│     └─ Runs in background                                  │
│     Duration: varies                                       │
│                                                            │
└────────────────────────────────────────────────────────────┘

Total STW time: ~200-1000 μs (sub-millisecond)
Total cycle time: varies based on heap size
```

### GC Trigger Conditions

The GC starts when:

**1. Heap Size Doubles** (Primary):
```go
// If last GC finished at 100 MB heap size
// Next GC triggers at 200 MB heap size
//
// Controlled by GOGC (default 100):
// GOGC=100: trigger at 2× previous heap (default)
// GOGC=200: trigger at 3× previous heap
// GOGC=50:  trigger at 1.5× previous heap
```

**2. Explicit Call**:
```go
runtime.GC()  // Force GC immediately
```

**3. Periodic (Backup)**:
```go
// If no GC for 2 minutes, force one
// Even if heap hasn't grown
```

### Measuring GC Frequency

```go
package main

import (
    "fmt"
    "runtime"
    "time"
)

func measureGC() {
    var m runtime.MemStats
    
    // Record initial GC count
    runtime.ReadMemStats(&m)
    initialGC := m.NumGC
    
    // Do some work
    time.Sleep(10 * time.Second)
    
    // Check GC count again
    runtime.ReadMemStats(&m)
    finalGC := m.NumGC
    
    fmt.Printf("GC ran %d times in 10 seconds\n", finalGC-initialGC)
    fmt.Printf("Total pause time: %v\n", time.Duration(m.PauseTotalNs))
    fmt.Printf("Average pause: %v\n", time.Duration(m.PauseTotalNs/uint64(finalGC)))
}
```

---

## Why Leaked Objects Aren't Collected

### The Reachability Problem

```go
// This is WHY the GC can't collect leaked objects
var cache = make(map[string]*Data)  // GC Root

func leak(key string) {
    data := &Data{/* large object */}
    cache[key] = data
    
    // GC Reasoning:
    // 1. cache is a global (root) → reachable
    // 2. cache contains entries → entries reachable
    // 3. Each entry.value points to Data → Data reachable
    // 4. Conclusion: Data is reachable → CANNOT COLLECT
    //
    // Even if we NEVER use this key again!
}
```

### The GC Can't Read Your Mind

```go
func example() {
    var results []Result
    
    for i := 0; i < 1_000_000; i++ {
        result := compute(i)
        results = append(results, result)
    }
    
    // We only need the last 10 results
    results = results[len(results)-10:]
    
    // GC sees:
    // - results is reachable (on stack)
    // - results points to backing array
    // - backing array contains 1M Result objects
    // - Conclusion: all 1M objects reachable → CANNOT COLLECT
    //
    // GC doesn't know we only "want" the last 10!
}
```

### What GC Can and Cannot Do

```
┌────────────────────────────────────────┐
│   What GC CAN Do                       │
├────────────────────────────────────────┤
│ ✓ Reclaim unreferenced objects        │
│ ✓ Follow all pointer chains           │
│ ✓ Handle cycles (A→B→A)                │
│ ✓ Run concurrently with app           │
│ ✓ Manage heap automatically           │
└────────────────────────────────────────┘

┌────────────────────────────────────────┐
│   What GC CANNOT Do                    │
├────────────────────────────────────────┤
│ ✗ Remove references you're keeping    │
│ ✗ Know which objects you "need"       │
│ ✗ Second-guess your data structures   │
│ ✗ Shrink maps automatically           │
│ ✗ Understand semantic "done-ness"     │
└────────────────────────────────────────┘
```

### Example: Cache with Dead Objects

```go
type CacheEntry struct {
    Data      *BigData
    LastUsed  time.Time
}

var cache = make(map[string]*CacheEntry)

func get(key string) *BigData {
    entry := cache[key]
    entry.LastUsed = time.Now()
    return entry.Data
}

func add(key string, data *BigData) {
    cache[key] = &CacheEntry{
        Data:     data,
        LastUsed: time.Now(),
    }
}

// Problem: Old entries never removed
// GC's perspective:
//   cache (root) → all CacheEntry objects
//                → all BigData objects
//                → ALL REACHABLE
//
// Reality: Entries not used in days
// But GC can't know LastUsed is semantically important!
```

---

## GC Metrics and Tuning

### Key GC Metrics

```go
package main

import (
    "fmt"
    "runtime"
)

func printGCStats() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    fmt.Printf("Heap Allocation:\n")
    fmt.Printf("  Alloc:      %d MB (currently allocated)\n", m.Alloc/1024/1024)
    fmt.Printf("  TotalAlloc: %d MB (total allocated over time)\n", m.TotalAlloc/1024/1024)
    fmt.Printf("  Sys:        %d MB (obtained from OS)\n", m.Sys/1024/1024)
    fmt.Printf("  HeapAlloc:  %d MB (heap allocation)\n", m.HeapAlloc/1024/1024)
    fmt.Printf("  HeapSys:    %d MB (heap obtained from OS)\n", m.HeapSys/1024/1024)
    
    fmt.Printf("\nGC Stats:\n")
    fmt.Printf("  NumGC:      %d (number of GC cycles)\n", m.NumGC)
    fmt.Printf("  PauseTotalNs: %d ms (total pause time)\n", m.PauseTotalNs/1_000_000)
    fmt.Printf("  LastGC:     %v (time of last GC)\n", time.Unix(0, int64(m.LastGC)))
    
    fmt.Printf("\nHeap Objects:\n")
    fmt.Printf("  HeapObjects: %d (number of allocated objects)\n", m.HeapObjects)
    fmt.Printf("  NextGC:      %d MB (heap size to trigger next GC)\n", m.NextGC/1024/1024)
}
```

### GOGC Tuning

The `GOGC` environment variable controls GC aggressiveness:

```bash
# Default (100): GC when heap doubles
GOGC=100 go run main.go

# More aggressive (50): GC when heap grows 50%
# - More frequent GC
# - Lower memory usage
# - Higher CPU usage
GOGC=50 go run main.go

# Less aggressive (200): GC when heap triples
# - Less frequent GC
# - Higher memory usage
# - Lower CPU usage
GOGC=200 go run main.go

# Disable GC (dangerous!)
GOGC=off go run main.go
```

**When to Tune GOGC**:

```go
// Scenario 1: Memory-constrained environment
// (containers with small memory limits)
export GOGC=50
// → More frequent GC, stays under memory limit

// Scenario 2: Batch processing
// (short-lived process, performance critical)
export GOGC=500
// → Less frequent GC, faster completion

// Scenario 3: Long-running service with spikes
// (default is usually best)
export GOGC=100
// → Balanced approach
```

### GC Percentage vs Memory Usage

```
GOGC Value     |  Memory Usage  |  CPU Usage  |  GC Frequency
---------------|----------------|-------------|---------------
GOGC=50        |  Low (1.5×)    |  High       |  Very frequent
GOGC=100       |  Medium (2×)   |  Medium     |  Moderate
GOGC=200       |  High (3×)     |  Low        |  Infrequent
GOGC=off       |  Unbounded     |  None       |  Never

Example with 100 MB heap after GC:
GOGC=50   → Next GC at 150 MB
GOGC=100  → Next GC at 200 MB (default)
GOGC=200  → Next GC at 300 MB
```

---

## Write Barriers and STW

### What Are Write Barriers?

During concurrent marking, the application continues running and can modify pointers. **Write barriers** ensure the GC doesn't miss objects.

```go
// Without write barriers (broken):
// 1. GC marks A as black (done scanning)
// 2. Application adds A.ref = B (new reference)
// 3. GC never sees B
// 4. B is swept as garbage (incorrect!)

// With write barriers:
// 1. GC marks A as black
// 2. Application: A.ref = B triggers write barrier
// 3. Write barrier: marks B as gray
// 4. GC scans B before finishing
// 5. B is kept (correct!)
```

**Performance Impact**:
- Small cost on every pointer write during GC
- ~5-10% overhead during mark phase
- Enabled only during GC, not always

### Stop-The-World (STW) Phases

Go minimizes STW time but can't eliminate it:

```
Why STW is needed:

Phase 1 (Mark Setup):
  - Must snapshot goroutine stacks
  - Stack scanning isn't concurrent-safe
  - Need consistent view of roots
  
Phase 2 (Mark Termination):
  - Finish any remaining marking
  - Disable write barriers
  - Prepare for sweep
```

**Typical STW Times** (Go 1.20+):
```
Small heap (<100 MB):   ~100-200 μs
Medium heap (1 GB):     ~200-500 μs
Large heap (10 GB):     ~500-1000 μs
Pathological:           >1 ms (investigate!)
```

### Monitoring STW Pauses

```go
package main

import (
    "fmt"
    "runtime/debug"
)

func monitorSTW() {
    // Enable GC trace
    debug.SetGCPercent(100)
    
    // This will print GC info to stderr:
    // gc 1 @0.003s 0%: 0.018+0.25+0.003 ms clock, ...
    //                   ^STW1 ^Concurrent ^STW2
}
```

Or use the `GODEBUG` environment variable:
```bash
GODEBUG=gctrace=1 go run main.go
```

Output example:
```
gc 1 @0.003s 0%: 0.018+0.25+0.003 ms clock, 0.14+0.12/0.23/0.056+0.024 ms cpu, 4->4->0 MB, 5 MB goal, 8 P

Explanation:
- gc 1: First GC cycle
- 0.018 ms: Mark setup STW time
- 0.25 ms: Concurrent mark time
- 0.003 ms: Mark termination STW time
- Total STW: 0.021 ms (excellent!)
```

---

## GC Behavior with Long-Lived References

### Scenario 1: Growing Cache

```go
var cache = make(map[string]*Data)

func addToCache(key string, data *Data) {
    cache[key] = data
}

// Over time:
// Heap: 100 MB → 200 MB → 400 MB → 800 MB → ...
//
// GC behavior:
// - GC runs when heap doubles
// - Scans all of cache each time
// - Finds all objects reachable
// - Reclaims nothing
// - Heap keeps growing
// - GC runs more frequently (more work each time)
// - CPU usage increases (more to scan)
```

**GC Metrics Evolution**:
```
Time  | Heap Size | GC Frequency | Scan Time | Effect
------|-----------|--------------|-----------|------------------
1h    | 100 MB    | Every 30s    | 10 ms     | Normal
6h    | 600 MB    | Every 20s    | 60 ms     | Slowing
24h   | 2.4 GB    | Every 15s    | 240 ms    | Degraded
48h   | 4.8 GB    | Every 10s    | 480 ms    | Critical
```

### Scenario 2: Slice Reslicing

```go
func processFiles() []Result {
    var allResults []Result
    
    for _, file := range files {
        data, _ := os.ReadFile(file)  // 100 MB each
        result := extract(data[:100]) // Only need 100 bytes
        allResults = append(allResults, result)
    }
    
    return allResults
}

// GC behavior:
// - allResults holds Result objects
// - Each Result contains data[0:100] (resliced)
// - Each data slice references 100 MB backing array
// - GC sees backing arrays as reachable
// - Scans 100 MB × N files each GC
// - Cannot reclaim backing arrays
// - Memory: N × 100 MB (not N × 100 bytes)
```

### Scenario 3: Event Listeners

```go
type EventBus struct {
    listeners map[string][]func(Event)
}

func (eb *EventBus) Subscribe(eventType string, handler func(Event)) {
    eb.listeners[eventType] = append(eb.listeners[eventType], handler)
}

// Problem: Closures capture scope
handler := func(e Event) {
    component.Process(e)  // Captures 'component'
}
bus.Subscribe("update", handler)

// GC behavior:
// - bus.listeners holds closure
// - Closure holds reference to component
// - Component holds reference to its data
// - GC sees: bus → closure → component → data
// - All reachable, none collectible
// - Even after component logically "done"
```

### What GC Sees vs What We Intend

```
What we intend:
┌─────────────────────────────────────┐
│  Keep: Last 1000 items              │
│  Discard: Everything older          │
└─────────────────────────────────────┘

What GC sees:
┌─────────────────────────────────────┐
│  cache (root) → 1,000,000 items     │
│  All reachable → Keep everything    │
└─────────────────────────────────────┘

The gap: No semantic understanding
```

---

## Summary

### Key Concepts

1. **Tri-Color Marking**:
   - White (not visited) → Gray (visited) → Black (done)
   - Only white objects are collected
   - Leaked objects never become white

2. **Concurrent Design**:
   - Most GC work runs alongside application
   - STW phases minimized (< 1 ms)
   - Trade-off: some CPU overhead

3. **Reachability-Based**:
   - GC keeps everything reachable from roots
   - Can't distinguish "needed" from "reachable"
   - Developer must break reference chains

4. **GOGC Tuning**:
   - Controls GC frequency
   - Default (100) usually best
   - Tune only if needed (memory limits, batch jobs)

5. **Long-Lived Reference Impact**:
   - Increases heap size
   - More GC work (more to scan)
   - More frequent GC cycles
   - Higher CPU usage
   - Eventual OOM

### Mental Model

```
GC Cycle:
1. Application runs, allocates memory
2. Heap grows to 2× previous size
3. GC triggered
4. Mark phase: Find all reachable objects
5. Sweep phase: Reclaim unreachable objects
6. Back to step 1

With leaks:
1. Application runs, allocates memory
2. Heap grows to 2× previous size
3. GC triggered
4. Mark phase: Find all reachable objects (MORE each time)
5. Sweep phase: Reclaim unreachable objects (LESS each time)
6. Heap didn't shrink much, still close to trigger threshold
7. Allocate more, trigger again quickly
8. Repeat, getting worse each time
```

### Prevention Strategy

```go
// Instead of relying on GC, explicitly manage references

// BAD: Let GC handle it (it can't)
var cache = make(map[string]*Data)
func add(k string, v *Data) {
    cache[k] = v  // Hope GC cleans up old data
}

// GOOD: Explicitly manage lifecycle
var cache *lru.Cache
func init() {
    cache, _ = lru.New(1000)  // Size-bounded
}
func add(k string, v *Data) {
    cache.Add(k, v)  // Automatic eviction
}
```

### Diagnostic Questions

When facing memory issues, ask:
- [ ] Is heap size growing over time?
- [ ] Is GC running frequently but not reclaiming much?
- [ ] Are there large maps or slices in heap profiles?
- [ ] Do we have global variables holding collections?
- [ ] Are we breaking reference chains explicitly?

---

## Next Steps

- **Learn about slices**: Read [Slice Internals](./03-slice-internals.md)
- **Study cache patterns**: Read [Cache Patterns](./04-cache-patterns.md)
- **Visual understanding**: Read [Memory Growth Diagrams](./06-memory-growth-diagrams.md)
- **Detection techniques**: Read [Detection Methods](./05-detection-methods.md)

---

**Return to**: [Long-Lived References README](../README.md)
