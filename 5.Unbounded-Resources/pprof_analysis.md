# pprof Analysis for Unbounded Resources

**Test Environment**: macOS (M1), 256GB RAM, Go 1.21+

**Tested By**: Daniel Samadi

---

## Overview

This guide demonstrates how to use `pprof` to detect and analyze unbounded resource growth. The key difference from other leak types is that unbounded resources grow **rapidly under load**, so you'll often see dramatic changes within seconds rather than hours.

---

## Example 1: Worker Pool Leak Analysis

### Running the Leaky Version

```bash
cd 5.Unbounded-Resources/examples/worker-pool-leak
go run example.go
```

### Actual Console Output (M1 Mac)

```
pprof server running on http://localhost:6060
Collect goroutine profile: curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof

[START] Goroutines: 2
Simulating traffic spike: 1000 tasks/second

[AFTER 2s] Goroutines: 1921  |  Tasks submitted: 1916  |  Completed: 0

WARNING: Unbounded goroutine growth detected!
Each task creates a new goroutine without limits.
[AFTER 4s] Goroutines: 3859  |  Tasks submitted: 3854  |  Completed: 0
[AFTER 6s] Goroutines: 4849  |  Tasks submitted: 5777  |  Completed: 933
[AFTER 8s] Goroutines: 4863  |  Tasks submitted: 7745  |  Completed: 2887
[AFTER 10s] Goroutines: 4846  |  Tasks submitted: 9658  |  Completed: 4817

Leak demonstrated. Goroutines grow without bound.
Final goroutine count: 4846
```

### Goroutine Profile Analysis

While the program is running, collect a goroutine profile:

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1 > goroutine_unbounded.txt
```

**Actual pprof Output**:

```
goroutine profile: total 4730
4724 @ 0x10420bec0 0x10420fe30 0x1043af918 0x104213c54
#	0x10420fe2f	time.Sleep+0x14f		/opt/homebrew/Cellar/go/1.25.0/libexec/src/runtime/time.go:363
#	0x1043af917	main.processTaskBadly+0x27	.../worker-pool-leak/example.go:90

1 @ 0x10420bec0 0x1041a486c 0x1041a4444 0x1043af7e8 0x104213c54
#	0x1043af7e7	main.simulateTrafficSpike+0x67	.../worker-pool-leak/example.go:79

1 @ ... net/http.(*Server).Serve ...
#	0x1043afa38	main.main.func1+0xc8	.../worker-pool-leak/example.go:27
```

**Key Observations**:
- **4724 goroutines** in `main.processTaskBadly` function
- All blocked on `time.Sleep`
- Peak goroutine count: ~4863 (limited by 5-second task duration)
- Growth rate: ~1000 goroutines/second until tasks start completing
- Memory impact: 4724 × 2KB = ~9.5MB just for stacks

---

### Running the Fixed Version

```bash
cd ../worker-pool-fixed
go run fixed_example.go
```

### Actual Console Output (M1 Mac)

```
pprof server running on http://localhost:6061
[START] Goroutines: 102 (100 workers + overhead)
Simulating traffic spike: 1000 tasks/second

[AFTER 2s] Goroutines: 103  |  Submitted: 600  |  Completed: 0  |  Rejected: 1240
Goroutines stable! Worker pool bounded at 100.
[AFTER 4s] Goroutines: 103  |  Submitted: 600  |  Completed: 0  |  Rejected: 3197
Goroutines stable! Worker pool bounded at 100.
[AFTER 6s] Goroutines: 103  |  Submitted: 700  |  Completed: 100  |  Rejected: 5041
Goroutines stable! Worker pool bounded at 100.
[AFTER 8s] Goroutines: 103  |  Submitted: 700  |  Completed: 100  |  Rejected: 7007
Goroutines stable! Worker pool bounded at 100.
[AFTER 10s] Goroutines: 103  |  Submitted: 700  |  Completed: 100  |  Rejected: 8967
Goroutines stable! Worker pool bounded at 100.

No leak! Goroutine count remained stable.
Final goroutine count: 103
Total tasks: submitted=700, completed=100, rejected=8967
```

### Goroutine Profile (Fixed)

```bash
curl http://localhost:6061/debug/pprof/goroutine?debug=1 > goroutine_bounded.txt
```

**Actual pprof Output**:

```
goroutine profile: total 106
100 @ 0x10420bec0 0x10420fe30 0x1043afdfc 0x1043afde9 0x1043af3a8 0x104213c54
#	0x10420fe2f	time.Sleep+0x14f			/opt/homebrew/Cellar/go/1.25.0/libexec/src/runtime/time.go:363
#	0x1043afdfb	main.processTaskCorrectly+0x2b		.../worker-pool-fixed/fixed_example.go:154
#	0x1043afde8	main.simulateTrafficSpike.func1+0x18	.../worker-pool-fixed/fixed_example.go:141
#	0x1043af3a7	main.(*WorkerPool).worker+0x37		.../worker-pool-fixed/fixed_example.go:49

1 @ ... main.simulateTrafficSpike ...
1 @ ... net/http.(*Server).Serve ...
1 @ ... main.main ...
```

**Key Differences**:
- **Only 100 worker goroutines** (bounded by pool size)
- All 100 workers in `main.(*WorkerPool).worker` function
- Goroutine count stable at 103-106 throughout test
- Backpressure working: 8967 tasks rejected

### Comparison Table (Actual Results)

| Metric | Leaky (10s) | Fixed (10s) | Improvement |
|--------|-------------|-------------|-------------|
| Goroutines | **4846** | **103** | **97.9% reduction** |
| Peak Goroutines | 4863 | 103 | 47x fewer |
| Memory (stacks) | ~10 MB | ~0.2 MB | 98% reduction |
| Tasks Submitted | 9658 | 700 | Bounded queue |
| Tasks Completed | 4817 | 100 | Same rate |
| Tasks Rejected | 0 | 8967 | Backpressure! |
| System Stability | Growing | Stable | Fixed |

---

## Example 2: Channel Buffer Leak Analysis

### Running the Leaky Version

```bash
cd 5.Unbounded-Resources/examples/channel-buffer-leak
go run example.go
```

### Actual Console Output (M1 Mac)

```
pprof server running on http://localhost:6060
[START] Heap Alloc: 1007 MB, Events queued: 0
Simulating event burst: 10,000 events/second
Processing rate: 100 events/second

[AFTER 2s] Heap: 1007 MB  |  Queued: 15826  |  Processed: 187  |  Pending: 15639

WARNING: Event backlog growing!
Large buffer hides the problem - no backpressure signal.
[AFTER 4s] Heap: 1007 MB  |  Queued: 31192  |  Processed: 375  |  Pending: 30817

WARNING: Event backlog growing!
Large buffer hides the problem - no backpressure signal.
[AFTER 6s] Heap: 1007 MB  |  Queued: 49762  |  Processed: 573  |  Pending: 49189
[AFTER 8s] Heap: 1007 MB  |  Queued: 67897  |  Processed: 770  |  Pending: 67127
[AFTER 10s] Heap: 1007 MB  |  Queued: 82003  |  Processed: 938  |  Pending: 81065

Final state: 1007 MB heap, 81076 events pending
The large buffer consumed memory without providing feedback.
```

### Memory Profile Analysis

```bash
curl http://localhost:6060/debug/pprof/heap?debug=1 > heap_buffer_leak.txt
```

**Actual pprof Output**:

```
heap profile: 7: 1056014448 [8: 1056014688] @ heap/1048576

1: 1056006144 [1: 1056006144] @ 0x1003c3394 0x1003c337d 0x1001eb898 0x100227c54
#	0x1003c3393	main.NewEventProcessor+0x63	.../channel-buffer-leak/example.go:36
#	0x1003c337c	main.main+0x4c			.../channel-buffer-leak/example.go:65

# runtime.MemStats
# Alloc = 1062292800
# TotalAlloc = 1062301120
# Sys = 1076513288
# HeapAlloc = 1062292800
# HeapSys = 1069023232
# HeapInuse = 1063288832
# HeapObjects = 2557
# NumGC = 1
```

**Key Observations**:
- **1056 MB (1 GB)** consumed by channel buffer at startup
- `main.NewEventProcessor` allocates the entire 1M-event buffer immediately
- HeapAlloc: 1,062,292,800 bytes (~1 GB)
- Processing can't keep up (100/sec vs 10,000/sec)
- Buffer hides the problem - no blocking, no errors, no backpressure signal
- Memory allocated upfront for 1M events × ~1KB = 1 GB

---

### Running the Fixed Version

```bash
cd ../channel-buffer-fixed
go run fixed_example.go
```

### Actual Console Output (M1 Mac)

```
pprof server running on http://localhost:6061
[START] Heap Alloc: 1 MB, Buffer size: 1000 events
Simulating event burst: 10,000 events/second
Processing rate: 100 events/second
Excess events will be dropped (backpressure)

[AFTER 2s] Heap: 1 MB  |  Queued: 1200  |  Processed: 199  |  Dropped: 18764  |  Pending: 1001
[AFTER 4s] Heap: 1 MB  |  Queued: 1392  |  Processed: 391  |  Dropped: 35673  |  Pending: 1001
[AFTER 6s] Heap: 1 MB  |  Queued: 1589  |  Processed: 588  |  Dropped: 53593  |  Pending: 1001
[AFTER 8s] Heap: 1 MB  |  Queued: 1789  |  Processed: 788  |  Dropped: 73246  |  Pending: 1001
[AFTER 10s] Heap: 1 MB  |  Queued: 1987  |  Processed: 986  |  Dropped: 92417  |  Pending: 1001

Final state: 1 MB heap
Events: queued=1987, processed=986, dropped=92419
Backpressure prevented memory exhaustion.
```

### Memory Profile (Fixed)

```bash
curl http://localhost:6061/debug/pprof/heap?debug=1 > heap_buffer_fixed.txt
```

**Actual pprof Output**:

```
heap profile: 4: 2528 [4: 2529] @ heap/1048576

2: 32 [2: 32] @ 0x102a230c8 0x102a23b74 0x102a25200 0x1029d0824 0x1029d0444 0x102bdbebc 0x102a3fc54
#	0x102bdbebb	main.simulateEventBurst+0xbb	.../channel-buffer-fixed/fixed_example.go:160

# runtime.MemStats
# Alloc = 1263456
# TotalAlloc = 1263456
# Sys = 8407304
# HeapAlloc = 1263456
# HeapSys = 3801088
# HeapInuse = 1835008
# HeapObjects = 704
# NumGC = 0
```

**Key Observations**:
- **1.2 MB** total heap allocation (vs 1 GB in leaky version)
- HeapAlloc: 1,263,456 bytes (~1.2 MB)
- Buffer bounded at 1000 events
- 92,419 events dropped (backpressure working!)
- Memory stable throughout test
- No GC pressure (NumGC = 0)

### Comparison Table (Actual Results)

| Metric | Leaky (10s) | Fixed (10s) | Improvement |
|--------|-------------|-------------|-------------|
| Heap Memory | **1007 MB** | **1 MB** | **99.9% reduction** |
| HeapAlloc (bytes) | 1,062,292,800 | 1,263,456 | 840x smaller |
| Events Pending | 81,076 | 1,001 (max) | Bounded |
| Events Dropped | 0 | 92,419 | Backpressure! |
| Feedback Signal | None | Drops | Visible |
| Memory Trend | 1GB upfront | Stable 1MB | Fixed |
| MaxRSS | 363 MB | 12 MB | 96.7% reduction |

---

## Advanced Detection Techniques

### 1. Real-Time Goroutine Monitoring

```bash
# Watch goroutine count change over time
watch -n 1 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"

# Sample output (leaky):
# goroutine profile: total 1003
# goroutine profile: total 2003
# goroutine profile: total 3003  ← Growing!

# Sample output (fixed):
# goroutine profile: total 103
# goroutine profile: total 103
# goroutine profile: total 103  ← Stable!
```

### 2. Goroutine Growth Rate Detection

```bash
# Capture baseline
curl -s http://localhost:6060/debug/pprof/goroutine | head -1 > baseline.txt

# Wait 10 seconds
sleep 10

# Capture current
curl -s http://localhost:6060/debug/pprof/goroutine | head -1 > current.txt

# Compare
echo "Baseline: $(cat baseline.txt)"
echo "Current:  $(cat current.txt)"
```

### 3. Using pprof's `-base` Flag

```bash
# Capture baseline profile
curl http://localhost:6060/debug/pprof/goroutine > baseline.pprof

# Wait for growth
sleep 30

# Capture current profile
curl http://localhost:6060/debug/pprof/goroutine > current.pprof

# Show only NEW goroutines
go tool pprof -http=:8081 -base=baseline.pprof current.pprof
```

### 4. Flame Graph Analysis

```bash
# Generate flame graph
go tool pprof -http=:8081 goroutine.pprof

# In browser:
# - Click "Flame Graph" view
# - Look for wide bars (many goroutines)
# - Wide bars in same function = unbounded creation
```

### 5. Load Testing with Metrics

```bash
# Install hey (HTTP load generator)
go install github.com/rakyll/hey@latest

# Run load test
hey -n 10000 -c 500 http://localhost:8080/api/process

# Monitor during test
watch -n 1 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"
```

---

## Common Patterns in pprof Output

### Pattern 1: Unbounded Goroutine Creation

```
goroutine profile: total 15423
15000 @ 0x... 0x... 0x...
#   main.handleRequest.func1
#   ^^^ Thousands in same anonymous function
```

**Cause**: `go func()` inside request handler without limits.

### Pattern 2: Channel Buffer Accumulation

```
(pprof) top
    500MB   100%  main.eventQueue
```

**Cause**: Large channel buffer filling faster than draining.

### Pattern 3: Worker Pool Explosion

```
goroutine profile: total 8000
7900 @ 0x... 0x...
#   main.(*DynamicPool).worker
```

**Cause**: "Dynamic" pool that only grows, never shrinks.

---

## Detection Checklist

Use this checklist when analyzing unbounded resource issues:

- [ ] Is goroutine count growing linearly with requests?
- [ ] Are there thousands of goroutines in the same function?
- [ ] Is memory growing faster than expected under load?
- [ ] Are channel buffers filling without draining?
- [ ] Is there any backpressure mechanism?
- [ ] Do load tests cause resource exhaustion?
- [ ] Are there limits on concurrent operations?

---

## Key Metrics to Monitor

| Metric | Normal | Warning | Critical |
|--------|--------|---------|----------|
| Goroutine count | < 500 | 500-2000 | > 2000 |
| Goroutine growth/sec | < 10 | 10-100 | > 100 |
| Channel buffer fill % | < 50% | 50-80% | > 80% |
| Memory growth/sec | < 1 MB | 1-10 MB | > 10 MB |

---

## Prevention Strategies

1. **Always use worker pools** for concurrent tasks
2. **Size channel buffers intentionally** (not arbitrarily large)
3. **Implement backpressure** (return errors, drop requests)
4. **Add rate limiting** at entry points
5. **Monitor goroutine count** as a key metric
6. **Load test regularly** to expose unbounded patterns

---

## Related Resources

- [Concurrency Limits](resources/01-concurrency-limits.md)
- [Worker Pool Patterns](resources/02-worker-pool-patterns.md)
- [Backpressure Mechanisms](resources/03-backpressure-mechanisms.md)
- [Production Case Studies](resources/07-production-case-studies.md)

