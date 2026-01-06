# Concurrency Limits: The Foundation of Stable Systems

**Read Time**: 15 minutes

**Prerequisites**: Basic understanding of goroutines and channels

---

## Table of Contents

1. [Why Limits Matter](#why-limits-matter)
2. [Little's Law for Capacity Planning](#littles-law-for-capacity-planning)
3. [Calculating Appropriate Limits](#calculating-appropriate-limits)
4. [Implementation Patterns](#implementation-patterns)
5. [Common Mistakes](#common-mistakes)

---

## Why Limits Matter

### The Unbounded Trap

Without limits, systems fail catastrophically under load:

```go
// BAD: No limits - disaster waiting to happen
func handleRequest(w http.ResponseWriter, r *http.Request) {
    go processAsync(r)  // Creates unlimited goroutines
}
```

**What happens under spike**:
- 10,000 requests/sec → 10,000 goroutines
- Each goroutine: 2KB minimum stack
- Result: 20MB/sec memory consumption
- Within 1 minute: 1.2GB consumed, system crashes

### With Limits

```go
// GOOD: Bounded - predictable behavior
var sem = make(chan struct{}, 100) // Max 100 concurrent

func handleRequest(w http.ResponseWriter, r *http.Request) {
    select {
    case sem <- struct{}{}:
        go func() {
            defer func() { <-sem }()
            processAsync(r)
        }()
    default:
        http.Error(w, "Service busy", http.StatusServiceUnavailable)
    }
}
```

**What happens under spike**:
- 10,000 requests/sec → 100 goroutines (capped)
- Excess requests get 503 response
- Memory stable at ~200KB for goroutines
- System remains responsive

---

## Little's Law for Capacity Planning

### The Formula

**L = λ × W**

Where:
- **L** = Average number of items in system (goroutines, connections)
- **λ** (lambda) = Average arrival rate (requests/second)
- **W** = Average time in system (processing time)

### Practical Application

**Example**: API endpoint processing requests

- Expected traffic: 1000 req/sec
- Average processing time: 100ms (0.1 seconds)
- **L = 1000 × 0.1 = 100 concurrent requests**

This means you need **at least 100 workers** to handle steady-state traffic.

### Adding Safety Margin

For production, add buffer for:
- Traffic spikes (2-3x normal)
- Processing time variance
- GC pauses

**Recommended formula**:
```
Max Workers = (Peak Traffic × P99 Latency) × 1.5
```

**Example**:
- Peak traffic: 3000 req/sec
- P99 latency: 200ms
- Max Workers = (3000 × 0.2) × 1.5 = **900 workers**

---

## Calculating Appropriate Limits

### Step 1: Measure Baseline

```go
func measureBaseline() {
    // Track processing times
    start := time.Now()
    processRequest(req)
    duration := time.Since(start)
    
    // Record metrics
    processingTime.Observe(duration.Seconds())
}
```

### Step 2: Determine Resource Constraints

**Memory per goroutine**:
- Minimum stack: 2KB
- Typical with allocations: 10-50KB
- Heavy processing: 100KB+

**Calculate memory limit**:
```
Max Goroutines = Available Memory / Memory per Goroutine
```

**Example**:
- Available memory: 1GB
- Memory per goroutine: 50KB
- Max Goroutines = 1GB / 50KB = **20,000**

### Step 3: Consider Other Resources

**File descriptors**:
- Default limit: 1024 per process
- Each connection uses 1 FD
- Leave headroom: use 80% of limit

**Database connections**:
- Pool size typically 25-100
- Each query holds connection
- Match worker count to pool size

### Step 4: Set Conservative Limits

```go
const (
    // Based on calculations above
    MaxWorkers     = 500   // Memory-safe
    MaxQueueSize   = 1000  // 2x workers for burst
    MaxConnections = 100   // DB pool size
)
```

---

## Implementation Patterns

### Pattern 1: Semaphore

```go
type Semaphore struct {
    sem chan struct{}
}

func NewSemaphore(max int) *Semaphore {
    return &Semaphore{
        sem: make(chan struct{}, max),
    }
}

func (s *Semaphore) Acquire(ctx context.Context) error {
    select {
    case s.sem <- struct{}{}:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (s *Semaphore) Release() {
    <-s.sem
}

func (s *Semaphore) TryAcquire() bool {
    select {
    case s.sem <- struct{}{}:
        return true
    default:
        return false
    }
}
```

### Pattern 2: Worker Pool

```go
type WorkerPool struct {
    tasks   chan func()
    workers int
    wg      sync.WaitGroup
}

func NewWorkerPool(workers, queueSize int) *WorkerPool {
    pool := &WorkerPool{
        tasks:   make(chan func(), queueSize),
        workers: workers,
    }
    
    for i := 0; i < workers; i++ {
        pool.wg.Add(1)
        go pool.worker()
    }
    
    return pool
}

func (p *WorkerPool) worker() {
    defer p.wg.Done()
    for task := range p.tasks {
        task()
    }
}

func (p *WorkerPool) Submit(task func()) bool {
    select {
    case p.tasks <- task:
        return true
    default:
        return false // Queue full
    }
}
```

### Pattern 3: Rate Limiter

```go
import "golang.org/x/time/rate"

type RateLimiter struct {
    limiter *rate.Limiter
}

func NewRateLimiter(rps int, burst int) *RateLimiter {
    return &RateLimiter{
        limiter: rate.NewLimiter(rate.Limit(rps), burst),
    }
}

func (r *RateLimiter) Allow() bool {
    return r.limiter.Allow()
}

func (r *RateLimiter) Wait(ctx context.Context) error {
    return r.limiter.Wait(ctx)
}
```

---

## Common Mistakes

### Mistake 1: No Limits at All

```go
// BAD: Unlimited goroutines
for req := range requests {
    go process(req)
}
```

### Mistake 2: Limits Too High

```go
// BAD: 1 million is effectively unlimited
sem := make(chan struct{}, 1_000_000)
```

### Mistake 3: Limits Without Backpressure

```go
// BAD: Blocks forever when full
func submit(task Task) {
    sem <- struct{}{} // Blocks caller indefinitely
    go func() {
        defer func() { <-sem }()
        task.Execute()
    }()
}
```

### Mistake 4: Ignoring Resource Dependencies

```go
// BAD: 1000 workers but only 25 DB connections
pool := NewWorkerPool(1000, 5000)
db.SetMaxOpenConns(25) // Bottleneck!
```

---

## Summary

1. **Always set limits** on concurrent operations
2. **Use Little's Law** to calculate baseline capacity
3. **Add safety margin** for spikes and variance
4. **Consider all resources** (memory, FDs, connections)
5. **Implement backpressure** when limits are reached
6. **Monitor and adjust** based on production metrics

---

**Next**: [Worker Pool Patterns](./02-worker-pool-patterns.md)

