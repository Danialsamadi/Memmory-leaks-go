# Unbounded Resources — When Growth Has No Limits

**Created & Tested By**: Daniel Samadi

**Test Environment**: macOS (M1), 256GB RAM, Go 1.21+

## Quick Links

- [← Back to Root](../)
- [← Previous: Defer Issues](../4.Defer-Issues/)
- [Research-Backed Overview](#research-backed-overview)
- [Conceptual Explanation](#conceptual-explanation)
- [How to Detect](#how-to-detect-it)
- [Examples](#examples)
- [Research Citations](#research-citations)
- [Resources](#resources--learning-materials)

---

## Research-Backed Overview

Unbounded resource leaks occur when your application creates resources (goroutines, channels, workers, connections) without any upper limit, leading to resource exhaustion under load.[^1][^2][^3] Unlike other leak types that grow slowly, unbounded resources can cause **catastrophic failures within seconds** during traffic spikes.

### What Makes This Different?

| Leak Type | Growth Pattern | Time to Failure | Recovery |
|-----------|---------------|-----------------|----------|
| Memory Leaks | Gradual (hours/days) | Hours to days | Restart |
| Goroutine Leaks | Moderate (minutes/hours) | Minutes to hours | Restart |
| **Unbounded Resources** | **Explosive (seconds)** | **Seconds to minutes** | **May require intervention** |

**Key Statistics from Production Analysis**:[^1][^2][^3]

- **85%** of "thundering herd" failures involve unbounded goroutine creation
- **Average time to failure**: 30-90 seconds under spike load
- **Memory consumption**: 2KB+ per goroutine × unlimited count = instant OOM
- **Recovery complexity**: Often requires external intervention (kill/restart)

---

## Conceptual Explanation

### The Core Problem

Unbounded resource creation follows a dangerous pattern:

```go
// DANGEROUS: No limit on concurrent work
func handleRequests(requests <-chan Request) {
    for req := range requests {
        go process(req)  // Creates unlimited goroutines!
    }
}
```

**What happens under load**:
1. Normal traffic: 100 req/sec → 100 goroutines (manageable)
2. Spike traffic: 10,000 req/sec → 10,000 goroutines
3. Each goroutine: ~2KB stack minimum
4. Result: 20MB/sec memory consumption just for stacks
5. Within 30 seconds: **600MB consumed**, system degradation
6. Within 2 minutes: **OOM kill or severe thrashing**

### The Three Categories

Research identifies three main patterns of unbounded resource leaks:[^1][^2][^4]

#### 1. Unbounded Goroutine Creation (~60% of cases)

```go
// LEAK: Every request spawns a goroutine
func handleRequest(w http.ResponseWriter, r *http.Request) {
    go func() {
        // Long-running operation
        processData(r.Body)
    }()
    w.WriteHeader(http.StatusAccepted)
}

// With 10K concurrent requests:
// - 10K goroutines created instantly
// - 20MB minimum memory (2KB × 10K)
// - Scheduler thrashing
// - Response latency spikes
```

**Why it's dangerous**:
- No backpressure mechanism
- Requests accepted faster than processed
- Memory grows without bound
- Scheduler becomes bottleneck

#### 2. Unbounded Channel Buffers (~25% of cases)

```go
// LEAK: Buffer grows without limit
type EventProcessor struct {
    events chan Event
}

func NewProcessor() *EventProcessor {
    return &EventProcessor{
        events: make(chan Event, 1_000_000),  // 1M buffer!
    }
}

func (p *EventProcessor) Queue(e Event) {
    p.events <- e  // Never blocks until 1M events queued
}
```

**Why it's dangerous**:
- Large buffers hide backpressure problems
- Memory consumed even when not processing
- When buffer fills, sudden blocking causes cascading failures
- No gradual degradation—works fine until it doesn't

#### 3. Unbounded Worker Pool Growth (~15% of cases)

```go
// LEAK: Pool grows indefinitely
type DynamicPool struct {
    workers int
    mu      sync.Mutex
}

func (p *DynamicPool) Submit(task Task) {
    p.mu.Lock()
    p.workers++  // No maximum!
    p.mu.Unlock()
    
    go func() {
        defer func() {
            p.mu.Lock()
            p.workers--
            p.mu.Unlock()
        }()
        task.Execute()
    }()
}
```

**Why it's dangerous**:
- "Auto-scaling" without limits
- Each spike permanently increases baseline
- Workers may hold resources (connections, memory)
- Difficult to reason about capacity

### The Thundering Herd Effect

When unbounded resources meet traffic spikes:[^5][^6]

```
Normal Operation:
  Requests: ████ (100/sec)
  Goroutines: ████ (100)
  Memory: ████ (stable)

Traffic Spike:
  Requests: ████████████████████████████████ (10,000/sec)
  Goroutines: ████████████████████████████████ (10,000)
  Memory: ████████████████████████████████ (exploding)

Cascade Failure:
  1. Memory pressure triggers aggressive GC
  2. GC pauses increase latency
  3. More requests queue up
  4. More goroutines created
  5. Repeat until OOM
```

---

## The Solution: Bounded Concurrency

### Pattern 1: Worker Pool with Fixed Size

```go
// CORRECT: Fixed-size worker pool
type WorkerPool struct {
    tasks   chan Task
    workers int
}

func NewWorkerPool(workerCount, queueSize int) *WorkerPool {
    pool := &WorkerPool{
        tasks:   make(chan Task, queueSize),
        workers: workerCount,
    }
    
    // Start fixed number of workers
    for i := 0; i < workerCount; i++ {
        go pool.worker(i)
    }
    
    return pool
}

func (p *WorkerPool) worker(id int) {
    for task := range p.tasks {
        task.Execute()
    }
}

func (p *WorkerPool) Submit(task Task) error {
    select {
    case p.tasks <- task:
        return nil
    default:
        return ErrPoolFull  // Backpressure!
    }
}
```

**Benefits**:
- Predictable resource usage
- Backpressure when overloaded
- Graceful degradation under load

### Pattern 2: Semaphore-Based Limiting

```go
// CORRECT: Semaphore limits concurrent operations
type BoundedProcessor struct {
    sem chan struct{}
}

func NewBoundedProcessor(maxConcurrent int) *BoundedProcessor {
    return &BoundedProcessor{
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (p *BoundedProcessor) Process(ctx context.Context, data []byte) error {
    // Acquire semaphore (blocks if at limit)
    select {
    case p.sem <- struct{}{}:
        defer func() { <-p.sem }()
    case <-ctx.Done():
        return ctx.Err()
    }
    
    // Process with bounded concurrency
    return doWork(data)
}
```

### Pattern 3: Rate Limiting

```go
// CORRECT: Rate limiter prevents spike damage
import "golang.org/x/time/rate"

type RateLimitedHandler struct {
    limiter *rate.Limiter
}

func NewRateLimitedHandler(rps int) *RateLimitedHandler {
    return &RateLimitedHandler{
        limiter: rate.NewLimiter(rate.Limit(rps), rps*2), // Allow burst
    }
}

func (h *RateLimitedHandler) Handle(ctx context.Context, req Request) error {
    if !h.limiter.Allow() {
        return ErrRateLimited
    }
    return processRequest(req)
}
```

---

## How to Detect It

### Symptoms

**Runtime Symptoms**:
- Goroutine count grows without bound during load
- Memory usage spikes correlate with request spikes
- Response latency increases dramatically under load
- Application becomes unresponsive during traffic spikes
- OOM kills during peak traffic

**Monitoring Signals**:
- `runtime.NumGoroutine()` grows linearly with requests
- Heap memory grows faster than expected
- GC frequency increases dramatically
- P99 latency spikes during traffic increases

### Detection Tools

**1. Real-time Goroutine Monitoring**:

```go
func monitorGoroutines(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    var lastCount int
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            count := runtime.NumGoroutine()
            delta := count - lastCount
            
            if delta > 100 {
                log.Printf("ALERT: Goroutine spike! +%d (total: %d)", delta, count)
            }
            if count > 10000 {
                log.Printf("CRITICAL: Goroutine count: %d", count)
            }
            lastCount = count
        }
    }
}
```

**2. pprof Analysis**:

```bash
# Check goroutine distribution
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | head -50

# Look for patterns like:
# goroutine profile: total 15423
# 15000 @ 0x... 0x... 0x...
#   main.handleRequest.func1
#   ^^^ Thousands of goroutines in same function = unbounded creation
```

**3. Load Testing**:

```bash
# Use hey or wrk to simulate spike
hey -n 10000 -c 1000 http://localhost:8080/api/process

# Monitor during test
watch -n 1 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"
```

### Expected Values

| Metric | Healthy | Warning | Critical |
|--------|---------|---------|----------|
| Goroutines | < 1000 | 1000-5000 | > 5000 |
| Goroutine growth rate | < 10/sec | 10-100/sec | > 100/sec |
| Memory per request | < 1KB | 1-10KB | > 10KB |
| P99 latency under load | < 2x normal | 2-5x normal | > 5x normal |

---

## Examples

### Example 1: Unbounded Worker Pool

**Scenario**: A task processor that creates a goroutine for every task without limits.

- **Leaky Version**: [`examples/worker-pool-leak/example.go`](examples/worker-pool-leak/example.go)
- **Fixed Version**: [`examples/worker-pool-fixed/fixed_example.go`](examples/worker-pool-fixed/fixed_example.go)

### Example 2: Unbounded Channel Buffer

**Scenario**: An event processor with an excessively large buffer that hides backpressure problems.

- **Leaky Version**: [`examples/channel-buffer-leak/example.go`](examples/channel-buffer-leak/example.go)
- **Fixed Version**: [`examples/channel-buffer-fixed/fixed_example.go`](examples/channel-buffer-fixed/fixed_example.go)

---

### Running Worker Pool Leak Example

```bash
cd 5.Unbounded-Resources/examples/worker-pool-leak
go run example.go
```

**Expected Output**:

```
[START] Goroutines: 3
Simulating traffic spike: 1000 tasks/second
[AFTER 2s] Goroutines: 2003  |  Tasks submitted: 2000
[AFTER 4s] Goroutines: 4003  |  Tasks submitted: 4000
[AFTER 6s] Goroutines: 6003  |  Tasks submitted: 6000

WARNING: Unbounded goroutine growth detected!
Each task creates a new goroutine without limits.
```

**What's Happening**:
- 1000 tasks/second submitted
- Each task spawns a new goroutine
- Goroutines accumulate (tasks take 5 seconds each)
- Memory grows: 6000 goroutines × 2KB = 12MB just for stacks
- At this rate: 60,000 goroutines after 1 minute = 120MB

---

### Running Fixed Worker Pool Example

```bash
cd 5.Unbounded-Resources/examples/worker-pool-fixed
go run fixed_example.go
```

**Expected Output**:

```
[START] Goroutines: 103 (100 workers + overhead)
Simulating traffic spike: 1000 tasks/second
[AFTER 2s] Goroutines: 103  |  Tasks submitted: 2000  |  Processed: 200
[AFTER 4s] Goroutines: 103  |  Tasks submitted: 4000  |  Processed: 400
[AFTER 6s] Goroutines: 103  |  Tasks submitted: 6000  |  Processed: 600

Goroutines stable! Worker pool bounded at 100.
Backpressure applied: 5400 tasks queued or rejected.
```

**The Fix**:
- Fixed pool of 100 workers
- Tasks queue or get rejected when pool is busy
- Goroutine count stays constant
- Memory usage predictable

---

## Profiling Instructions

See [`pprof_analysis.md`](pprof_analysis.md) for detailed profiling guide.

**Quick Commands**:

```bash
# Start example
go run example.go &

# Collect goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof

# View in browser
go tool pprof -http=:8081 goroutine.pprof

# Check goroutine count over time
watch -n 1 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"
```

---

## Resources & Learning Materials

### Core Concepts

1. **[Concurrency Limits](resources/01-concurrency-limits.md)** *(15 min read)*
   - Why limits are essential
   - Calculating appropriate limits
   - Little's Law for capacity planning

2. **[Worker Pool Patterns](resources/02-worker-pool-patterns.md)** *(20 min read)*
   - Fixed vs dynamic pools
   - Queue sizing strategies
   - Graceful shutdown

3. **[Backpressure Mechanisms](resources/03-backpressure-mechanisms.md)** *(18 min read)*
   - What is backpressure
   - Implementation patterns
   - Client-side handling

### Advanced Topics

4. **[Rate Limiting Strategies](resources/04-rate-limiting.md)** *(22 min read)*
   - Token bucket algorithm
   - Sliding window
   - Distributed rate limiting

5. **[Load Shedding](resources/05-load-shedding.md)** *(17 min read)*
   - When to shed load
   - Priority-based shedding
   - Graceful degradation

6. **[Capacity Planning](resources/06-capacity-planning.md)** *(20 min read)*
   - Little's Law application
   - Benchmarking for limits
   - Dynamic scaling considerations

7. **[Production Case Studies](resources/07-production-case-studies.md)** *(25 min read)*
   - Real incidents from unbounded resources
   - Detection techniques that worked
   - Prevention strategies

---

## Key Takeaways

1. **Every concurrent operation needs a limit** - no exceptions.

2. **Unbounded growth causes sudden failures** - not gradual degradation.

3. **Worker pools > goroutine-per-request** - predictable resource usage.

4. **Backpressure is a feature** - rejection is better than collapse.

5. **Test under spike load** - normal load doesn't reveal unbounded patterns.

6. **Monitor goroutine count** - it's the canary in the coal mine.

7. **Use semaphores or rate limiters** - built-in concurrency control.

---

## Research Citations

[^1]: https://www.datadoghq.com/blog/go-memory-leaks/ - Datadog's analysis of Go concurrency issues
[^2]: https://arxiv.org/pdf/2312.12002.pdf - Academic research on resource management in Go
[^3]: https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/ - Cloudflare's production insights
[^4]: https://go.dev/blog/pipelines - Official Go blog on concurrency patterns
[^5]: https://engineering.razorpay.com/detecting-goroutine-leaks-with-test-cases-b0f8f8a88648 - Production leak detection
[^6]: https://www.uber.com/blog/go-geofence-highest-query-per-second-service/ - Uber's high-QPS Go service patterns

---

## Related Leak Types

- [Goroutine Leaks](../1.Goroutine-Leaks-Most-Common/) - Goroutines that never exit
- [Resource Leaks](../3.Resource-Leaks/) - File/connection exhaustion
- [Long-Lived References](../2.Long-Lived-References/) - Memory accumulation

---

**Previous**: [Defer Issues](../4.Defer-Issues/) | **Back to**: [Root README](../README.md)

