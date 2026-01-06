# Load Shedding in Go

**Read Time**: 17 minutes

**Prerequisites**: Understanding of rate limiting and backpressure

---

## Table of Contents

1. [What is Load Shedding](#what-is-load-shedding)
2. [When to Shed Load](#when-to-shed-load)
3. [Implementation Strategies](#implementation-strategies)
4. [Graceful Degradation](#graceful-degradation)
5. [Production Patterns](#production-patterns)

---

## What is Load Shedding

Load shedding is the practice of **intentionally dropping requests** when a system is overloaded to protect overall system health. Unlike rate limiting (which limits per-client), load shedding is a global protection mechanism.

### The Philosophy

> "It's better to serve 80% of requests successfully than to serve 100% poorly."

### Without Load Shedding

```
Overload Scenario:
  Requests: 10,000/sec
  Capacity: 5,000/sec
  Result: All 10,000 requests get slow responses
          System degrades for everyone
          Cascading failures possible
```

### With Load Shedding

```
Overload Scenario:
  Requests: 10,000/sec
  Capacity: 5,000/sec
  Result: 5,000 requests served normally
          5,000 requests rejected immediately (503)
          System remains healthy
```

---

## When to Shed Load

### Metrics to Monitor

```go
type LoadMetrics struct {
    CPUUsage        float64
    MemoryUsage     float64
    GoroutineCount  int
    QueueDepth      int
    P99Latency      time.Duration
    ErrorRate       float64
}

func (m *LoadMetrics) ShouldShed() bool {
    return m.CPUUsage > 0.8 ||          // CPU > 80%
           m.MemoryUsage > 0.85 ||       // Memory > 85%
           m.GoroutineCount > 10000 ||   // Too many goroutines
           m.QueueDepth > 1000 ||        // Queue backing up
           m.P99Latency > 5*time.Second || // Latency spike
           m.ErrorRate > 0.1             // Error rate > 10%
}
```

### Thresholds by Resource

| Resource | Warning | Shed Load | Critical |
|----------|---------|-----------|----------|
| CPU | 70% | 80% | 90% |
| Memory | 75% | 85% | 95% |
| Goroutines | 5000 | 10000 | 20000 |
| Queue Depth | 500 | 1000 | 2000 |
| P99 Latency | 2x normal | 5x normal | 10x normal |

---

## Implementation Strategies

### Strategy 1: Simple Load Shedder

```go
type LoadShedder struct {
    currentLoad int64
    maxLoad     int64
}

func NewLoadShedder(maxLoad int64) *LoadShedder {
    return &LoadShedder{maxLoad: maxLoad}
}

func (ls *LoadShedder) Acquire() bool {
    for {
        current := atomic.LoadInt64(&ls.currentLoad)
        if current >= ls.maxLoad {
            return false // Shed load
        }
        if atomic.CompareAndSwapInt64(&ls.currentLoad, current, current+1) {
            return true
        }
    }
}

func (ls *LoadShedder) Release() {
    atomic.AddInt64(&ls.currentLoad, -1)
}

// HTTP Middleware
func LoadSheddingMiddleware(ls *LoadShedder) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !ls.Acquire() {
                w.Header().Set("Retry-After", "5")
                http.Error(w, "Service overloaded", http.StatusServiceUnavailable)
                return
            }
            defer ls.Release()
            next.ServeHTTP(w, r)
        })
    }
}
```

### Strategy 2: Adaptive Load Shedding

```go
type AdaptiveLoadShedder struct {
    currentLoad  int64
    maxLoad      int64
    baseMax      int64
    latencyP99   time.Duration
    targetLatency time.Duration
    mu           sync.Mutex
}

func NewAdaptiveLoadShedder(maxLoad int64, targetLatency time.Duration) *AdaptiveLoadShedder {
    return &AdaptiveLoadShedder{
        maxLoad:       maxLoad,
        baseMax:       maxLoad,
        targetLatency: targetLatency,
    }
}

func (als *AdaptiveLoadShedder) RecordLatency(latency time.Duration) {
    als.mu.Lock()
    defer als.mu.Unlock()
    
    // Simple exponential moving average
    als.latencyP99 = time.Duration(float64(als.latencyP99)*0.9 + float64(latency)*0.1)
    
    // Adjust max load based on latency
    if als.latencyP99 > als.targetLatency*2 {
        // Latency too high, reduce capacity
        als.maxLoad = int64(float64(als.maxLoad) * 0.9)
        if als.maxLoad < als.baseMax/4 {
            als.maxLoad = als.baseMax / 4 // Floor at 25%
        }
    } else if als.latencyP99 < als.targetLatency {
        // Latency good, increase capacity
        als.maxLoad = int64(float64(als.maxLoad) * 1.1)
        if als.maxLoad > als.baseMax {
            als.maxLoad = als.baseMax
        }
    }
}

func (als *AdaptiveLoadShedder) Acquire() bool {
    als.mu.Lock()
    max := als.maxLoad
    als.mu.Unlock()
    
    current := atomic.AddInt64(&als.currentLoad, 1)
    if current > max {
        atomic.AddInt64(&als.currentLoad, -1)
        return false
    }
    return true
}
```

### Strategy 3: LIFO Queue (Tail Drop)

Drop oldest requests first - they're most likely to timeout anyway.

```go
type LIFOShedder struct {
    queue    []Request
    maxSize  int
    mu       sync.Mutex
    notEmpty *sync.Cond
}

func NewLIFOShedder(maxSize int) *LIFOShedder {
    ls := &LIFOShedder{
        queue:   make([]Request, 0, maxSize),
        maxSize: maxSize,
    }
    ls.notEmpty = sync.NewCond(&ls.mu)
    return ls
}

func (ls *LIFOShedder) Push(req Request) bool {
    ls.mu.Lock()
    defer ls.mu.Unlock()
    
    if len(ls.queue) >= ls.maxSize {
        // Drop oldest (tail)
        ls.queue = ls.queue[1:]
    }
    
    // Add newest at front
    ls.queue = append([]Request{req}, ls.queue...)
    ls.notEmpty.Signal()
    return true
}

func (ls *LIFOShedder) Pop() Request {
    ls.mu.Lock()
    defer ls.mu.Unlock()
    
    for len(ls.queue) == 0 {
        ls.notEmpty.Wait()
    }
    
    // Take newest (front)
    req := ls.queue[0]
    ls.queue = ls.queue[1:]
    return req
}
```

---

## Graceful Degradation

Instead of complete rejection, return degraded responses.

### Pattern 1: Feature Degradation

```go
type DegradationLevel int

const (
    LevelFull DegradationLevel = iota
    LevelReduced
    LevelMinimal
    LevelEmergency
)

type DegradableService struct {
    level atomic.Value // DegradationLevel
}

func (ds *DegradableService) SetLevel(level DegradationLevel) {
    ds.level.Store(level)
}

func (ds *DegradableService) GetLevel() DegradationLevel {
    return ds.level.Load().(DegradationLevel)
}

func (ds *DegradableService) GetUserProfile(userID string) (*UserProfile, error) {
    level := ds.GetLevel()
    
    switch level {
    case LevelFull:
        // Full response with all data
        return ds.getFullProfile(userID)
    
    case LevelReduced:
        // Skip expensive operations
        return ds.getBasicProfile(userID)
    
    case LevelMinimal:
        // Return cached data only
        return ds.getCachedProfile(userID)
    
    case LevelEmergency:
        // Return static response
        return &UserProfile{ID: userID, Status: "service_degraded"}, nil
    }
    
    return nil, errors.New("unknown degradation level")
}
```

### Pattern 2: Timeout-Based Degradation

```go
func (s *Service) HandleRequest(ctx context.Context, req Request) Response {
    // Try full response with timeout
    ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
    defer cancel()
    
    fullResp, err := s.getFullResponse(ctx, req)
    if err == nil {
        return fullResp
    }
    
    // Fallback to cached/partial response
    if ctx.Err() == context.DeadlineExceeded {
        cachedResp, err := s.getCachedResponse(req)
        if err == nil {
            cachedResp.Degraded = true
            return cachedResp
        }
    }
    
    // Ultimate fallback
    return Response{
        Error:    "service_busy",
        Degraded: true,
    }
}
```

### Pattern 3: Circuit Breaker with Fallback

```go
type CircuitBreaker struct {
    failures    int64
    threshold   int64
    state       atomic.Value // "closed", "open", "half-open"
    lastFailure time.Time
    mu          sync.Mutex
}

func (cb *CircuitBreaker) Execute(primary, fallback func() error) error {
    state := cb.state.Load().(string)
    
    switch state {
    case "open":
        // Check if we should try half-open
        cb.mu.Lock()
        if time.Since(cb.lastFailure) > 30*time.Second {
            cb.state.Store("half-open")
            cb.mu.Unlock()
        } else {
            cb.mu.Unlock()
            return fallback()
        }
        fallthrough
    
    case "half-open", "closed":
        err := primary()
        if err != nil {
            cb.recordFailure()
            return fallback()
        }
        cb.recordSuccess()
        return nil
    }
    
    return fallback()
}

func (cb *CircuitBreaker) recordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    cb.failures++
    cb.lastFailure = time.Now()
    
    if cb.failures >= cb.threshold {
        cb.state.Store("open")
    }
}

func (cb *CircuitBreaker) recordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    cb.failures = 0
    cb.state.Store("closed")
}
```

---

## Production Patterns

### Pattern 1: Health-Based Shedding

```go
type HealthBasedShedder struct {
    cpuThreshold    float64
    memoryThreshold float64
    latencyThreshold time.Duration
}

func (hbs *HealthBasedShedder) ShouldShed() bool {
    // Check CPU
    if getCPUUsage() > hbs.cpuThreshold {
        return true
    }
    
    // Check memory
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    memUsage := float64(m.Alloc) / float64(m.Sys)
    if memUsage > hbs.memoryThreshold {
        return true
    }
    
    // Check goroutines
    if runtime.NumGoroutine() > 10000 {
        return true
    }
    
    return false
}

// Middleware
func HealthSheddingMiddleware(hbs *HealthBasedShedder) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if hbs.ShouldShed() {
                // Return 503 with retry hint
                w.Header().Set("Retry-After", "10")
                http.Error(w, "Service overloaded", http.StatusServiceUnavailable)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
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
    return load >= ps.thresholds[priority]
}

func (ps *PriorityShedder) Acquire(priority Priority) bool {
    if ps.ShouldShed(priority) {
        return false
    }
    atomic.AddInt64(&ps.currentLoad, 1)
    return true
}

func (ps *PriorityShedder) Release() {
    atomic.AddInt64(&ps.currentLoad, -1)
}
```

---

## Summary

| Strategy | Behavior | Best For |
|----------|----------|----------|
| Simple Count | Fixed limit | Predictable load |
| Adaptive | Adjusts to latency | Variable load |
| LIFO | Drop oldest | Latency-sensitive |
| Priority | Protect important | Mixed traffic |
| Health-based | System metrics | Resource protection |

**Key Principles**:
1. Shed load early rather than late
2. Return fast failures (503) rather than slow timeouts
3. Include `Retry-After` header
4. Monitor shed rate as a key metric
5. Test shedding behavior under load

---

**Next**: [Capacity Planning](./06-capacity-planning.md)

