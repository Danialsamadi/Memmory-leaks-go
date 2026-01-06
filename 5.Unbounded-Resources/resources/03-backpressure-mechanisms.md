# Backpressure Mechanisms in Go

**Read Time**: 18 minutes

**Prerequisites**: Understanding of channels and concurrency patterns

---

## Table of Contents

1. [What is Backpressure](#what-is-backpressure)
2. [Why Backpressure Matters](#why-backpressure-matters)
3. [Implementation Strategies](#implementation-strategies)
4. [Client-Side Handling](#client-side-handling)
5. [Production Patterns](#production-patterns)

---

## What is Backpressure

Backpressure is a mechanism that **slows down or rejects** producers when consumers can't keep up. It's a feedback signal that prevents system overload.

### Without Backpressure

```
Producer (fast)    →    Buffer (grows)    →    Consumer (slow)
   1000/sec              [growing...]            100/sec
                              ↓
                         Memory exhaustion
                              ↓
                           CRASH
```

### With Backpressure

```
Producer (fast)    →    Buffer (bounded)    →    Consumer (slow)
   1000/sec              [full - 1000]            100/sec
       ↑                      ↓
       └── SLOW DOWN ←── Signal
```

---

## Why Backpressure Matters

### The Unbounded Buffer Problem

```go
// BAD: No backpressure - disaster
type BadProcessor struct {
    events chan Event
}

func NewBadProcessor() *BadProcessor {
    return &BadProcessor{
        events: make(chan Event, 1_000_000), // Huge buffer!
    }
}

func (p *BadProcessor) Queue(e Event) {
    p.events <- e // Never blocks until 1M events!
}
```

**Problems**:
1. Memory consumed by buffered events
2. No signal to producer to slow down
3. When buffer fills, sudden failure
4. Latency spikes as buffer drains

### With Backpressure

```go
// GOOD: Bounded with backpressure
type GoodProcessor struct {
    events chan Event
}

func NewGoodProcessor() *GoodProcessor {
    return &GoodProcessor{
        events: make(chan Event, 1000), // Reasonable buffer
    }
}

func (p *GoodProcessor) Queue(ctx context.Context, e Event) error {
    select {
    case p.events <- e:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    default:
        return ErrQueueFull // Backpressure signal!
    }
}
```

---

## Implementation Strategies

### Strategy 1: Non-Blocking with Rejection

```go
func (p *Processor) Submit(task Task) error {
    select {
    case p.tasks <- task:
        return nil
    default:
        return ErrServiceBusy
    }
}

// Usage
if err := processor.Submit(task); err == ErrServiceBusy {
    // Handle rejection: retry, queue elsewhere, or fail
    return http.StatusServiceUnavailable
}
```

### Strategy 2: Blocking with Timeout

```go
func (p *Processor) SubmitWithTimeout(task Task, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    select {
    case p.tasks <- task:
        return nil
    case <-ctx.Done():
        return ErrTimeout
    }
}
```

### Strategy 3: Blocking with Context

```go
func (p *Processor) Submit(ctx context.Context, task Task) error {
    select {
    case p.tasks <- task:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Strategy 4: Rate Limiting

```go
import "golang.org/x/time/rate"

type RateLimitedProcessor struct {
    limiter *rate.Limiter
    tasks   chan Task
}

func NewRateLimitedProcessor(rps int) *RateLimitedProcessor {
    return &RateLimitedProcessor{
        limiter: rate.NewLimiter(rate.Limit(rps), rps*2),
        tasks:   make(chan Task, 1000),
    }
}

func (p *RateLimitedProcessor) Submit(ctx context.Context, task Task) error {
    // Wait for rate limiter
    if err := p.limiter.Wait(ctx); err != nil {
        return err
    }
    
    // Submit to queue
    select {
    case p.tasks <- task:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Strategy 5: Semaphore-Based Limiting

```go
type SemaphoreProcessor struct {
    sem   chan struct{}
    tasks chan Task
}

func NewSemaphoreProcessor(maxConcurrent, queueSize int) *SemaphoreProcessor {
    return &SemaphoreProcessor{
        sem:   make(chan struct{}, maxConcurrent),
        tasks: make(chan Task, queueSize),
    }
}

func (p *SemaphoreProcessor) Submit(ctx context.Context, task Task) error {
    // Acquire semaphore
    select {
    case p.sem <- struct{}{}:
        // Got slot
    case <-ctx.Done():
        return ctx.Err()
    }
    
    // Submit task
    select {
    case p.tasks <- func() {
        defer func() { <-p.sem }() // Release semaphore
        task()
    }:
        return nil
    case <-ctx.Done():
        <-p.sem // Release on context cancel
        return ctx.Err()
    }
}
```

---

## Client-Side Handling

### Pattern 1: Exponential Backoff

```go
func submitWithBackoff(processor *Processor, task Task) error {
    backoff := 100 * time.Millisecond
    maxBackoff := 10 * time.Second
    maxRetries := 5
    
    for i := 0; i < maxRetries; i++ {
        err := processor.Submit(task)
        if err == nil {
            return nil
        }
        
        if err != ErrServiceBusy {
            return err // Non-retryable error
        }
        
        // Exponential backoff with jitter
        jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
        time.Sleep(backoff + jitter)
        
        backoff *= 2
        if backoff > maxBackoff {
            backoff = maxBackoff
        }
    }
    
    return ErrMaxRetriesExceeded
}
```

### Pattern 2: Circuit Breaker

```go
type CircuitBreaker struct {
    failures    int64
    threshold   int64
    resetAfter  time.Duration
    lastFailure time.Time
    mu          sync.Mutex
}

func (cb *CircuitBreaker) Allow() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    // Check if circuit is open
    if cb.failures >= cb.threshold {
        // Check if reset time has passed
        if time.Since(cb.lastFailure) > cb.resetAfter {
            cb.failures = 0
            return true
        }
        return false
    }
    
    return true
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.failures++
    cb.lastFailure = time.Now()
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.failures = 0
}

// Usage
func submitWithCircuitBreaker(cb *CircuitBreaker, p *Processor, task Task) error {
    if !cb.Allow() {
        return ErrCircuitOpen
    }
    
    err := p.Submit(task)
    if err == ErrServiceBusy {
        cb.RecordFailure()
        return err
    }
    
    cb.RecordSuccess()
    return err
}
```

### Pattern 3: Load Shedding

```go
type LoadShedder struct {
    currentLoad int64
    maxLoad     int64
}

func (ls *LoadShedder) ShouldShed() bool {
    load := atomic.LoadInt64(&ls.currentLoad)
    return load >= ls.maxLoad
}

func (ls *LoadShedder) Acquire() bool {
    for {
        current := atomic.LoadInt64(&ls.currentLoad)
        if current >= ls.maxLoad {
            return false
        }
        if atomic.CompareAndSwapInt64(&ls.currentLoad, current, current+1) {
            return true
        }
    }
}

func (ls *LoadShedder) Release() {
    atomic.AddInt64(&ls.currentLoad, -1)
}

// Usage in HTTP handler
func handler(ls *LoadShedder) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !ls.Acquire() {
            http.Error(w, "Service overloaded", http.StatusServiceUnavailable)
            return
        }
        defer ls.Release()
        
        // Process request
        processRequest(w, r)
    }
}
```

---

## Production Patterns

### Pattern 1: Adaptive Rate Limiting

```go
type AdaptiveRateLimiter struct {
    limiter     *rate.Limiter
    baseRate    float64
    currentRate float64
    errorRate   float64
    mu          sync.Mutex
}

func NewAdaptiveRateLimiter(baseRPS float64) *AdaptiveRateLimiter {
    return &AdaptiveRateLimiter{
        limiter:     rate.NewLimiter(rate.Limit(baseRPS), int(baseRPS)*2),
        baseRate:    baseRPS,
        currentRate: baseRPS,
    }
}

func (a *AdaptiveRateLimiter) RecordError() {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Reduce rate on errors
    a.currentRate *= 0.9
    if a.currentRate < a.baseRate*0.1 {
        a.currentRate = a.baseRate * 0.1
    }
    a.limiter.SetLimit(rate.Limit(a.currentRate))
}

func (a *AdaptiveRateLimiter) RecordSuccess() {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Slowly increase rate on success
    a.currentRate *= 1.01
    if a.currentRate > a.baseRate {
        a.currentRate = a.baseRate
    }
    a.limiter.SetLimit(rate.Limit(a.currentRate))
}

func (a *AdaptiveRateLimiter) Wait(ctx context.Context) error {
    return a.limiter.Wait(ctx)
}
```

### Pattern 2: Priority-Based Shedding

```go
type Priority int

const (
    PriorityLow Priority = iota
    PriorityNormal
    PriorityHigh
    PriorityCritical
)

type PriorityShedder struct {
    currentLoad int64
    thresholds  map[Priority]int64
}

func NewPriorityShedder(maxLoad int64) *PriorityShedder {
    return &PriorityShedder{
        thresholds: map[Priority]int64{
            PriorityLow:      int64(float64(maxLoad) * 0.5),  // Shed at 50%
            PriorityNormal:   int64(float64(maxLoad) * 0.7),  // Shed at 70%
            PriorityHigh:     int64(float64(maxLoad) * 0.9),  // Shed at 90%
            PriorityCritical: maxLoad,                         // Never shed
        },
    }
}

func (ps *PriorityShedder) ShouldShed(priority Priority) bool {
    load := atomic.LoadInt64(&ps.currentLoad)
    threshold := ps.thresholds[priority]
    return load >= threshold
}
```

### Pattern 3: Metrics-Driven Backpressure

```go
type MetricsBackpressure struct {
    queueSize    prometheus.Gauge
    rejected     prometheus.Counter
    latencyHist  prometheus.Histogram
    processor    *Processor
}

func (m *MetricsBackpressure) Submit(ctx context.Context, task Task) error {
    start := time.Now()
    
    err := m.processor.Submit(ctx, task)
    
    m.latencyHist.Observe(time.Since(start).Seconds())
    m.queueSize.Set(float64(m.processor.QueueLength()))
    
    if err == ErrServiceBusy {
        m.rejected.Inc()
    }
    
    return err
}
```

---

## Summary

| Strategy | Behavior | Best For |
|----------|----------|----------|
| Non-blocking rejection | Immediate fail | High-throughput APIs |
| Blocking with timeout | Wait then fail | Interactive requests |
| Rate limiting | Smooth traffic | External API calls |
| Semaphore | Limit concurrency | Resource-heavy tasks |
| Circuit breaker | Fail fast on errors | Dependent services |
| Load shedding | Drop excess | Overload protection |

**Key Principles**:
1. Always have a backpressure mechanism
2. Make rejection explicit and measurable
3. Propagate backpressure to clients
4. Use metrics to tune thresholds
5. Test under overload conditions

---

**Next**: [Rate Limiting Strategies](./04-rate-limiting.md)

