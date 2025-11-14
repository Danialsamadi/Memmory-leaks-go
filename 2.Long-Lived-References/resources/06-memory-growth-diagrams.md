# Memory Growth Diagrams: Visualizing Long-Lived Reference Leaks

**Read Time**: 15 minutes

**Prerequisites**: Understanding of [Memory Model](./01-memory-model-explanation.md) and [GC Behavior](./02-gc-behavior.md)

**Related Topics**: 
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [GC Behavior](./02-gc-behavior.md)
- [Cache Patterns](./04-cache-patterns.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Heap Growth Patterns](#heap-growth-patterns)
2. [GC Behavior Visualizations](#gc-behavior-visualizations)
3. [Slice Memory Layouts](#slice-memory-layouts)
4. [Cache Eviction Flows](#cache-eviction-flows)
5. [Comparison Diagrams](#comparison-diagrams)
6. [Summary](#summary)

---

## Heap Growth Patterns

### Healthy Application Pattern

```
Memory (MB)
│
100 │     ╱╲     ╱╲     ╱╲     ╱╲
    │    ╱  ╲   ╱  ╲   ╱  ╲   ╱  ╲
 50 │   ╱    ╲ ╱    ╲ ╱    ╲ ╱    ╲
    │  ╱      ╲      ╲      ╲      ╲
  0 │─────────────────────────────────► Time
     0    10    20    30    40    50 (minutes)

Pattern: Sawtooth
- Allocates memory
- GC runs
- Memory reclaimed
- Repeat

Characteristics:
✓ Memory returns to baseline
✓ Predictable pattern
✓ GC effectively reclaims memory
```

### Unbounded Cache Leak Pattern

```
Memory (GB)
│
10 │                                  ╱
    │                               ╱
 5  │                           ╱
    │                      ╱
  1 │             ╱
    │      ╱
  0 │─────────────────────────────────► Time
     0    10    20    30    40    50 (minutes)

Pattern: Linear growth
- Memory continuously increases
- GC runs but reclaims little
- No return to baseline

Characteristics:
✗ Monotonic growth
✗ GC ineffective
✗ Eventually OOM
```

### Slice Reslicing Leak Pattern

```
Memory (GB)
│
10 │    ┌────────────────────────────
    │   ╱│  (plateau)
 5  │  ╱ │
    │ ╱  │
  0 │╱───┴────────────────────────────► Time
     0    10    20    30    40    50 (minutes)
     ↑
     Processing phase

Pattern: Rapid growth then plateau
- Quick allocation (loading files)
- Memory stays high
- Small slices keep large arrays alive

Characteristics:
✗ Rapid initial growth
✗ Memory doesn't decrease
✗ Plateau far above working set size
```

### Global Variable Accumulation Pattern

```
Memory (GB)
│
10 │                              ╱
    │                          ╱
 5  │                      ╱
    │                  ╱
  1 │              ╱
    │          ╱
  0 │─────────────────────────────────► Time
     0    5    10    15    20    25 (days)

Pattern: Gradual growth
- Slower than cache leak
- Accumulates over days/weeks
- Hard to detect early

Characteristics:
✗ Gradual increase
✗ Long time to manifest
✗ Difficult to detect in testing
```

---

## GC Behavior Visualizations

### Normal GC Cycle

```
Application Timeline:

Time:      0     5     10    15    20    25    30 (seconds)
           │     │     │     │     │     │     │
Heap:      │  ▲  │  ▲  │  ▲  │  ▲  │  ▲  │  ▲  │
           │ ╱ ╲ │ ╱ ╲ │ ╱ ╲ │ ╱ ╲ │ ╱ ╲ │ ╱ ╲ │
           │╱   ╲│╱   ╲│╱   ╲│╱   ╲│╱   ╲│╱   ╲│
           └─────┴─────┴─────┴─────┴─────┴─────┴─
GC Events:   ↑     ↑     ↑     ↑     ↑     ↑
           (every ~5 seconds)

GC reclaims: ~50% of heap each cycle
```

### GC with Memory Leak

```
Application Timeline:

Time:      0     5     10    15    20    25    30 (seconds)
           │     │     │     │     │     │     │
Heap:      │  ╱  │  ╱  │  ╱  │  ╱  │  ╱  │  ╱  │
           │ ╱╲ ╱│ ╱╲ ╱│ ╱╲ ╱│ ╱╲ ╱│ ╱╲ ╱│ ╱╲ ╱│
           │╱  ╲╱│╱  ╲╱│╱  ╲╱│╱  ╲╱│╱  ╲╱│╱  ╲╱│
           └─────┴─────┴─────┴─────┴─────┴─────┴─
GC Events:   ↑  ↑  ↑  ↑  ↑  ↑  ↑  ↑  ↑  ↑  ↑  ↑
           (increasing frequency)

GC reclaims: ~10% of heap each cycle (worsening)
Baseline increases after each cycle
More frequent GC runs (heap grows to trigger threshold faster)
```

### GC Metrics Over Time (Leaked vs Healthy)

```
Heap Size:
Healthy:  ████████████████ 100 MB (stable)
Leaked:   ████████████████████████████████████ 800 MB (growing)

GC Frequency (per minute):
Healthy:  ████ 4 runs
Leaked:   ████████████ 12 runs

GC Reclaim Rate:
Healthy:  ███████████████████ 60%
Leaked:   ████ 10%

CPU Time in GC:
Healthy:  ████ 2%
Leaked:   ████████████████ 15%
```

---

## Slice Memory Layouts

### Healthy Slice Usage

```
Function scope:
┌─────────────────────────────────────┐
│ func process() {                    │
│   data := make([]byte, 1MB)         │
│   result := compute(data)           │
│   return result                     │
│ }                                   │
└─────────────────────────────────────┘

Memory Layout:
                Stack                    Heap
              ┌──────┐                ┌──────────┐
              │ data │───────────────▶│ 1 MB     │
              │ ptr  │                │ array    │
              │ len  │                │          │
              │ cap  │                │          │
              └──────┘                └──────────┘

After function returns:
              ┌──────┐                ┌──────────┐
              │      │  (stack        │ 1 MB     │ ← No references
              │      │   cleared)     │ array    │   GC reclaims
              └──────┘                └──────────┘
```

### Slice Reslicing Leak

```
Before reslicing:
              Stack                    Heap
            ┌────────┐              ┌─────────────┐
            │ data   │─────────────▶│ 100 MB      │
            │ ptr    │              │ array       │
            │ len    │              │             │
            │ cap    │              │             │
            └────────┘              └─────────────┘

After reslicing (header := data[:1KB]):
            ┌────────┐              ┌─────────────┐
            │ header │─────────────▶│ 100 MB      │
            │ ptr    │  (same ptr)  │ array       │
            │ len:1KB│              │             │
            │ cap    │              │ (all kept)  │
            └────────┘              └─────────────┘
                                          │
Problem: Small slice → Large array       │
         └──────────────────────────────┘
         GC cannot reclaim 100 MB

After copying (header := clone(data[:1KB])):
            ┌────────┐              ┌──────┐
            │ header │─────────────▶│ 1 KB │ (new array)
            │ ptr    │              └──────┘
            │ len:1KB│              
            └────────┘              ┌─────────────┐
                                    │ 100 MB      │ ← No references
                                    │ (freed)     │   GC reclaims
                                    └─────────────┘
```

### Pointer Slice Truncation

```
Before truncation (1M items):
┌─────────────────────────────────────────────────────┐
│ Backing Array (8 MB - holds 1M pointers)           │
├──┬──┬──┬──┬──────────────────────┬──┬──┬──┬──┬──┬─┤
│*0│*1│*2│*3│ ... (999,996 more) ... │*N│*N│*N│*N│*N│
└──┴──┴──┴──┴──────────────────────┴──┴──┴──┴──┴──┴─┘
 │  │  │  │                           │  │  │  │  │
 ▼  ▼  ▼  ▼                           ▼  ▼  ▼  ▼  ▼
Obj Obj Obj Obj ... (1M objects) ...  Obj Obj Obj Obj Obj
10KB each = 10 GB total

After truncation (items = items[999990:]):
Slice descriptor:
┌─────────────┐
│ ptr: →99999 │ (points to index 999990)
│ len: 10     │
│ cap: 10     │
└─────────────┘

Backing Array STILL HOLDS ALL POINTERS:
├──┬──┬──┬──┬──────────────────────┬──┬──┬──┬──┬──┬─┤
│*0│*1│*2│*3│ ... (999,986 more) ... │*N│*N│*N│*N│*N│
└──┴──┴──┴──┴──────────────────────┴──┴──┴──┴──┴──┴─┘
 │  │  │  │                           │  │  │  │  │
 ▼  ▼  ▼  ▼                           ▼  ▼  ▼  ▼  ▼
ALL 1M Objects still reachable via backing array!
GC keeps all 10 GB in memory

Fixed (after nil'ing):
├──┬──┬──┬──┬──────────────────────┬──┬──┬──┬──┬──┬─┤
│∅ │∅ │∅ │∅ │ ... (all nil'd) ...  │*N│*N│*N│*N│*N│
└──┴──┴──┴──┴──────────────────────┴──┴──┴──┴──┴──┴─┘
                                      │  │  │  │  │
999,990 objects ← NO REFERENCES       ▼  ▼  ▼  ▼  ▼
GC reclaims ~9.99 GB                 Only 10 objects kept
                                     100 KB in memory
```

---

## Cache Eviction Flows

### Unbounded Cache Flow

```
Request Flow:

Request 1 → Get("key1") → Miss → Fetch → Add to cache
                                            └─ Cache: 1 item

Request 2 → Get("key2") → Miss → Fetch → Add to cache
                                            └─ Cache: 2 items

Request 1000 → Get("key1000") → Miss → Fetch → Add to cache
                                                  └─ Cache: 1000 items

Request 1M → Get("key1M") → Miss → Fetch → Add to cache
                                              └─ Cache: 1M items

Memory Growth:
  Items:  1 → 2 → 1000 → ... → 1,000,000
  Memory: 5KB → 10KB → 5MB → ... → 5GB
  Problem: NO EVICTION ❌
```

### LRU Cache Flow

```
LRU Cache (max 3 items):

Step 1: Add A
  Cache: [A]
  Most recent: A

Step 2: Add B
  Cache: [B, A]
  Most recent: B

Step 3: Add C
  Cache: [C, B, A] ← FULL

Step 4: Access A
  Cache: [A, C, B] ← A moved to front
  Most recent: A

Step 5: Add D (triggers eviction)
  Before: [A, C, B] ← B is least recent
  Evict:  [A, C] ← B removed
  After:  [D, A, C]
  Most recent: D

Memory stays bounded at 3 items ✓

Visualization:
┌─────────────────────────────────────┐
│         LRU Queue                   │
│  ┌───┐  ┌───┐  ┌───┐               │
│  │ D │→ │ A │→ │ C │→ null         │
│  └───┘  └───┘  └───┘               │
│   ↑                    ↑            │
│  Most               Least           │
│  Recent            Recent           │
│  (head)            (tail)           │
└─────────────────────────────────────┘

Next eviction removes from tail (C)
```

### TTL Cache Flow

```
TTL Cache (5-minute expiration):

Time 0:00 - Add "session_abc"
  ┌────────────────────────────┐
  │ session_abc                │
  │ expires: 0:05              │
  └────────────────────────────┘

Time 0:02 - Add "session_xyz"
  ┌────────────────────────────┐
  │ session_abc (3min left)    │
  │ session_xyz (expires: 0:07)│
  └────────────────────────────┘

Time 0:05 - Cleanup runs
  ┌────────────────────────────┐
  │ session_abc EXPIRED ❌     │
  │ session_xyz (2min left)    │
  └────────────────────────────┘

After cleanup:
  ┌────────────────────────────┐
  │ session_xyz (2min left)    │
  └────────────────────────────┘

Timeline:
  0:00        0:05        0:10
  │           │           │
  A┌──────────┐           │
   │          │ (expired) │
   └──────────X           │
   B   ┌──────────────────┐
       │                  │ (expired)
       └──────────────────X
```

---

## Comparison Diagrams

### Memory Usage: Leak vs Fixed

```
Unbounded Cache (1 week):
Memory│
(GB)  │                                           ╱ 10 GB
   10 │                                        ╱
      │                                     ╱
    5 │                              ╱
      │                      ╱
    1 │           ╱
      │    ╱
    0 └─────────────────────────────────────────────────► Time
      Mon   Tue   Wed   Thu   Fri   Sat   Sun

LRU Cache (1 week):
Memory│
(MB)  │ ┌──────────────────────────────────────────────
  100 │╱  (stable at max capacity)
      │
   50 │
      │
    0 └─────────────────────────────────────────────────► Time
      Mon   Tue   Wed   Thu   Fri   Sat   Sun

Savings: 10 GB → 100 MB (99% reduction)
```

### GC Impact Comparison

```
Metric               | Unbounded | LRU Cached | Improvement
---------------------|-----------|------------|-------------
Heap Size            | 10 GB     | 100 MB     | 100×
GC Frequency         | 50/min    | 4/min      | 12.5×
GC CPU Time          | 25%       | 2%         | 12.5×
GC Pause (p99)       | 5 ms      | 0.5 ms     | 10×
Memory Reclaim Rate  | 5%        | 60%        | 12×

Visual Comparison:
GC Time per minute:
Unbounded: ████████████████████████████ 25%
LRU:       ███ 2%
```

### Request Latency Distribution

```
Unbounded Cache (after 1 week):
Latency│
(ms)   │                     ╱╲
  100  │                    ╱  ╲
       │                   ╱    ╲
   50  │              ╱╲  ╱      ╲
       │         ╱╲  ╱  ╲╱        ╲
   10  │    ╱╲  ╱  ╲╱              ╲___
       │___╱  ╲╱
     0 └────────────────────────────────────► Request #
       p50   p90   p95   p99   (outliers due to GC pauses)

Bounded Cache:
Latency│
(ms)   │
   50  │
       │
   10  │ ____________________________________________
       │ (consistent, low latency)
     0 └────────────────────────────────────► Request #
       p50   p90   p95   p99   (stable)
```

---

## Summary

### Pattern Recognition Guide

```
If you see:              Likely cause:

Sawtooth pattern        → Healthy (GC working)
Linear growth           → Unbounded cache
Rapid growth + plateau  → Slice reslicing
Gradual growth (days)   → Global accumulation
Increasing GC frequency → Memory leak
Low reclaim rate        → Leaked objects
High GC CPU %           → Memory leak
```

### Diagnostic Checklist

Use these diagrams to understand:
- [ ] Is memory growing over time?
- [ ] Is the pattern linear or saw tooth?
- [ ] Is GC frequency increasing?
- [ ] Is GC reclaiming memory effectively?
- [ ] Are there sudden jumps (file loading)?
- [ ] Does memory plateau (slice issue)?
- [ ] Is growth slow (global accumulation)?

### Key Takeaways

1. **Sawtooth = Healthy**: Memory goes up and down with GC
2. **Linear = Leak**: Continuous growth without return to baseline
3. **Plateau = Retention**: Memory used once but never freed
4. **Gradual = Accumulation**: Long-term collection growth

---

## Next Steps

- **Study real cases**: Read [Production Examples](./07-production-examples.md)
- **Learn detection**: Review [pprof Analysis](../pprof_analysis.md)
- **Implement fixes**: Review [Cache Patterns](./04-cache-patterns.md)
- **Understand eviction**: Read [Eviction Strategies](./05-eviction-strategies.md)

---

**Return to**: [Long-Lived References README](../README.md)
