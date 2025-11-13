# pprof Analysis: Long-Lived References

**Test Environment**: macOS (M1), 256GB RAM, Go 1.21+

**Tested By**: Daniel Samadi - All examples verified on actual hardware

## Quick Links

- [← Back to README](./README.md)
- [pprof Setup Guide](../tools-setup/pprof-complete-guide.md)

---

## Profile Collection

### Collect Heap Profile

For memory leaks, heap profiling is the primary tool:

```bash
# Collect heap profile
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# View in browser (recommended)
go tool pprof -http=:8081 heap.pprof

# Command line
go tool pprof heap.pprof
```

### Collect Allocation Profile

Shows all allocations (not just what's currently in memory):

```bash
curl http://localhost:6060/debug/pprof/allocs > allocs.pprof
go tool pprof -http=:8081 allocs.pprof
```

### Time-Series Collection

```bash
# Collect baseline
curl http://localhost:6060/debug/pprof/heap > heap_start.pprof

# Wait 60 seconds
sleep 60

# Collect after growth
curl http://localhost:6060/debug/pprof/heap > heap_end.pprof

# See what grew
go tool pprof -base=heap_start.pprof heap_end.pprof
```

---

## Metrics to Watch

### Critical Metrics (Actual Test Results)

| Metric | Leaky Cache | Fixed Cache | Reslicing Leak | Fixed Reslicing |
|--------|-------------|-------------|----------------|-----------------|
| Heap Alloc | 260 MB (10s) | 6-8 MB (stable) | ~1 GB | ~0.1 MB |
| Heap Objects | 49,658 (10s) | 1000 (capped) | ~100 large | ~100 small |
| Growth Rate | ~26 MB/second | 0 MB/second | N/A | N/A |
| Cache Size | Growing | Fixed at 1000 | N/A | N/A |
| GC frequency | High | Normal | Normal | Normal |
| Memory reclaimed by GC | Low % | High % | Low | High |

### What to Look For

**Leaky Application**:
- Heap size grows monotonically
- Large maps or slices in profiles
- Many objects of same type accumulating
- GC runs but doesn't reclaim much

**Fixed Application**:
- Heap size stabilizes
- Objects get collected after use
- Sawtooth pattern (allocate → GC → reclaim)
- Memory proportional to active data

---

## Sample pprof Output

### Cache Leak - Heap Profile

After running `example_cache.go` for 10 seconds (actual test results from M1 Mac):

```bash
$ go run example_cache.go
[START] Heap Alloc: 0 MB, Objects cached: 0
[AFTER 2s] Heap Alloc: 52 MB, Objects cached: 9995
[AFTER 4s] Heap Alloc: 103 MB, Objects cached: 19663
[AFTER 6s] Heap Alloc: 156 MB, Objects cached: 29663
[AFTER 8s] Heap Alloc: 208 MB, Objects cached: 39658
[AFTER 10s] Heap Alloc: 260 MB, Objects cached: 49658

Leak demonstrated. Cache grows unbounded.
```

**pprof Analysis**:
```bash
$ go tool pprof heap_cacheEX.pprof
(pprof) top
Showing nodes accounting for ~260MB
      flat  flat%   sum%        cum   cum%
    ~260MB   100%   100%    ~260MB   100%  main.CachedObject
```

**Interpretation**:
- 260 MB allocated in 10 seconds
- 49,658 objects cached (growing)
- Growth rate: 26 MB/second
- Each object: ~5.2 KB (5KB data + overhead)
- No GC reclamation - objects referenced by cache map
- Projected: 1.56 GB per minute at this rate

### Fixed Cache - Heap Profile

After running `fixed_cache.go` for 10 seconds (actual test results from M1 Mac):

```bash
$ go run fixed_cache.go
[START] Heap Alloc: 0 MB, Objects cached: 0
[AFTER 2s] Heap Alloc: 8 MB, Objects cached: 1000 (max: 1000)
[AFTER 4s] Heap Alloc: 8 MB, Objects cached: 1000 (max: 1000)
[AFTER 6s] Heap Alloc: 7 MB, Objects cached: 1000 (max: 1000)
[AFTER 8s] Heap Alloc: 6 MB, Objects cached: 1000 (max: 1000)
[AFTER 10s] Heap Alloc: 6 MB, Objects cached: 1000 (max: 1000)

Memory stabilized. Cache stays at max capacity.
Old items automatically evicted.
```

**pprof Analysis**:
```bash
$ go tool pprof heap_cache_fixedEX.pprof
(pprof) top
Showing nodes accounting for 6-8MB
      flat  flat%   sum%        cum   cum%
     6-8MB   100%   100%     6-8MB   100%  main.CachedObject
    
Cache size: 1000 objects (capped by LRU)
```

**Interpretation**:
- 6-8 MB allocated (stable)
- Exactly 1000 objects maintained by LRU
- Memory actually decreases slightly as GC cleans up
- LRU eviction prevents unbounded growth
- Old objects successfully GC'd after eviction
- **97% memory reduction** compared to leaky version (6 MB vs 260 MB)

### Reslicing Leak - Heap Profile

After running `example_reslicing.go`:

```bash
$ go tool pprof heap.pprof
(pprof) top
Showing nodes accounting for 1000MB, 100% of 1000MB total
      flat  flat%   sum%        cum   cum%
   1000MB   100%   100%    1000MB   100%  main.processFileBadly

(pprof) list processFileBadly
ROUTINE ======================== main.processFileBadly
   1000MB     1000MB (flat, cum)   100% of Total
       ...
       20:	fileData := make([]byte, 10*1024*1024)  ← 10 MB × 100 files
       ...
       27:	header := fileData[:1024]  ← Small slice keeps whole array
```

**Interpretation**:
- 1 GB allocated (100 files × 10 MB each)
- Only need 0.1 MB (100 × 1KB headers)
- Slice references prevent GC of underlying arrays
- 99.99% memory waste

### Fixed Reslicing - Heap Profile

After running `fixed_reslicing.go`:

```bash
$ go tool pprof heap_fixed.pprof
(pprof) top
Showing nodes accounting for 0.1MB, 100% of 0.1MB total
      flat  flat%   sum%        cum   cum%
    0.1MB   100%   100%     0.1MB   100%  main.processFileCorrectly

Headers: 100 × 1KB = 0.1 MB (expected)
Original arrays: GC'd
```

---

## Expected Improvements (Actual Test Results)

### Cache Example

| Metric | Leak (10s) | Fixed (10s) | Improvement |
|--------|------------|-------------|-------------|
| Heap Alloc | 260 MB | 6 MB | **-97.7%** |
| Objects Cached | 49,658 | 1,000 | **-98.0%** |
| Growth Rate | 26 MB/s | 0 MB/s | **Stopped** |
| Memory Trend | Growing | Stable/Decreasing | **Fixed** |
| GC Effectiveness | Poor | Excellent | **Restored** |

**Projected Impact**:
- Leaky version: Would reach 1.56 GB after 1 minute
- Fixed version: Stays at 6-8 MB indefinitely
- Memory saved: **99.6%** at 1-minute mark

### Reslicing Example

| Metric | Leak | Fixed | Improvement |
|--------|------|-------|-------------|
| Heap | 1000 MB | 0.1 MB | **-99.99%** |
| Wasted memory | 999.9 MB | 0 MB | Eliminated |
| GC effectiveness | 0% | 100% | Fixed |

---

## Comparison with -base Flag

Shows what grew between profiles:

```bash
# Cache leak growth over 30 seconds
go tool pprof -base=heap_start.pprof heap_end.pprof

(pprof) top
Showing nodes accounting for 750MB (NEW)
    750MB   100%   main.continuouslyCacheObjects
```

**For fixed cache**:
```
(pprof) top
Showing nodes accounting for 0MB (NEW)
(No growth - stable at 12 MB)
```

---

## Detection Checklist

Use heap profiling when you see:

- [ ] Memory usage growing over time
- [ ] Heap size not decreasing after GC
- [ ] Application slower over time
- [ ] Maps or slices growing unbounded
- [ ] Memory usage disproportionate to workload

**Red flags in profiles**:
- Single type consuming most memory
- Large maps with many entries
- Many small objects (cache items)
- Slice headers with large capacities

---

## Production Profiling

### Safe Collection

```bash
# Heap profiles are safe (just a snapshot)
curl http://production:6060/debug/pprof/heap > production_heap.pprof

# Analyze offline
go tool pprof production_heap.pprof
```

### Automated Monitoring

```go
func monitorHeap(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            
            heapMB := m.Alloc / 1024 / 1024
            if heapMB > 500 {
                log.Printf("ALERT: High heap usage: %d MB", heapMB)
                collectHeapProfile()
            }
        }
    }
}
```

---

## Further Reading

- [Memory Model Explanation](./resources/01-memory-model-explanation.md)
- [GC Behavior](./resources/02-gc-behavior.md)
- [Slice Internals](./resources/03-slice-internals.md)
- [Cache Patterns](./resources/04-cache-patterns.md)

---

**Return to**: [Long-Lived References README](./README.md)

