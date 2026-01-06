# Production Case Studies: Unbounded Resources

**Read Time**: 25 minutes

**Prerequisites**: Understanding of concurrency patterns and system metrics

---

## Table of Contents

1. [Case Study 1: The Thundering Herd](#case-study-1-the-thundering-herd)
2. [Case Study 2: The Silent Buffer](#case-study-2-the-silent-buffer)
3. [Case Study 3: The Auto-Scaling Trap](#case-study-3-the-auto-scaling-trap)
4. [Case Study 4: The Connection Storm](#case-study-4-the-connection-storm)
5. [Lessons Learned](#lessons-learned)

---

## Case Study 1: The Thundering Herd

### The Incident

**Company**: E-commerce platform  
**Impact**: 45-minute outage during flash sale  
**Root Cause**: Unbounded goroutine creation

### Timeline

```
14:00 - Flash sale begins
14:01 - Traffic spikes from 1K to 50K req/sec
14:02 - Goroutine count: 50,000 and climbing
14:03 - Memory usage: 2GB → 8GB
14:05 - First OOM kill, pod restarts
14:06 - Kubernetes restarts pods, traffic hits surviving pods
14:07 - Cascade failure, all pods OOM
14:08 - Service completely down
14:45 - Emergency fix deployed, service restored
```

### The Code

```go
// PROBLEMATIC: Original code
func handleOrder(w http.ResponseWriter, r *http.Request) {
    // Every request spawned a goroutine
    go func() {
        processOrder(r)          // 500ms average
        notifyWarehouse(r)       // 200ms average
        sendConfirmationEmail(r) // 300ms average
    }()
    w.WriteHeader(http.StatusAccepted)
}

// With 50K req/sec and 1 second processing:
// 50,000 goroutines created per second
// Each goroutine: ~10KB memory
// Result: 500MB/sec memory consumption
```

### The Fix

```go
// FIXED: Using worker pool
var orderPool = NewWorkerPool(500, 5000)

func handleOrder(w http.ResponseWriter, r *http.Request) {
    task := func() {
        processOrder(r)
        notifyWarehouse(r)
        sendConfirmationEmail(r)
    }
    
    if !orderPool.Submit(task) {
        http.Error(w, "Service busy, please retry", http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusAccepted)
}
```

### Metrics After Fix

| Metric | Before | After |
|--------|--------|-------|
| Max goroutines | Unbounded | 500 |
| Memory at 50K RPS | 8GB+ (OOM) | 200MB |
| Successful orders | 0% (crashed) | 95% |
| Rejected (503) | N/A | 5% |

---

## Case Study 2: The Silent Buffer

### The Incident

**Company**: Real-time analytics platform  
**Impact**: 6-hour data loss, undetected  
**Root Cause**: Oversized channel buffer hiding backpressure

### Timeline

```
08:00 - Normal operation, 10K events/sec
10:00 - Upstream sends 100K events/sec (10x spike)
10:01 - Events queuing in 1M buffer
12:00 - Buffer at 500K events, no alerts
14:00 - Buffer full (1M events), new events dropped silently
16:00 - Operator notices data gaps in reports
16:30 - Root cause identified
```

### The Code

```go
// PROBLEMATIC: Original code
type EventProcessor struct {
    events chan Event
}

func NewEventProcessor() *EventProcessor {
    return &EventProcessor{
        // "Let's make it big enough to never block"
        events: make(chan Event, 1_000_000),
    }
}

func (p *EventProcessor) Queue(e Event) {
    p.events <- e // Never blocks until 1M events!
    // No error returned, no metrics, no visibility
}
```

### Why It Failed

1. **No backpressure signal**: Producers had no idea consumers were behind
2. **No monitoring**: Queue depth wasn't tracked
3. **Silent drops**: When buffer finally filled, events were lost
4. **Delayed detection**: 6 hours before anyone noticed

### The Fix

```go
// FIXED: Bounded buffer with metrics
type EventProcessor struct {
    events  chan Event
    metrics *ProcessorMetrics
}

func NewEventProcessor() *EventProcessor {
    p := &EventProcessor{
        events: make(chan Event, 10000), // Reasonable size
        metrics: &ProcessorMetrics{},
    }
    
    // Start metrics reporter
    go p.reportMetrics()
    
    return p
}

func (p *EventProcessor) Queue(e Event) error {
    select {
    case p.events <- e:
        p.metrics.queued.Inc()
        p.metrics.queueDepth.Set(float64(len(p.events)))
        return nil
    default:
        p.metrics.dropped.Inc()
        return ErrQueueFull
    }
}

func (p *EventProcessor) reportMetrics() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        depth := len(p.events)
        if depth > 5000 {
            log.Printf("WARN: Event queue depth: %d", depth)
        }
        if depth > 8000 {
            log.Printf("CRITICAL: Event queue near capacity: %d", depth)
            alertOps("Event queue critical")
        }
    }
}
```

### Alerting Rules Added

```yaml
# Prometheus alerts
- alert: EventQueueHigh
  expr: event_queue_depth > 5000
  for: 5m
  labels:
    severity: warning
    
- alert: EventQueueCritical
  expr: event_queue_depth > 8000
  for: 1m
  labels:
    severity: critical
    
- alert: EventsDropped
  expr: rate(events_dropped_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
```

---

## Case Study 3: The Auto-Scaling Trap

### The Incident

**Company**: API Gateway service  
**Impact**: $200K cloud bill, performance degradation  
**Root Cause**: "Dynamic" worker pool without upper limit

### Timeline

```
Week 1: Normal traffic, 100 workers
Week 2: Traffic spike, pool grows to 500 workers
Week 3: Spike ends, pool stays at 500 (no scale-down)
Week 4: Another spike, pool grows to 1000
Week 5: Pattern continues, pool at 5000 workers
Week 6: Cloud bill arrives, 10x normal cost
```

### The Code

```go
// PROBLEMATIC: Original code
type DynamicPool struct {
    workers int
    tasks   chan Task
}

func (p *DynamicPool) Submit(task Task) {
    select {
    case p.tasks <- task:
        return
    default:
        // Queue full, add more workers!
        p.workers++
        go p.worker() // No upper limit!
        p.tasks <- task
    }
}

// Problems:
// 1. No maximum worker limit
// 2. Workers never removed
// 3. Each worker holds resources (memory, connections)
```

### The Fix

```go
// FIXED: Bounded dynamic pool
type BoundedDynamicPool struct {
    minWorkers int
    maxWorkers int
    workers    int64
    tasks      chan Task
    mu         sync.Mutex
}

func (p *BoundedDynamicPool) Submit(task Task) error {
    select {
    case p.tasks <- task:
        return nil
    default:
        // Try to add worker if under max
        if p.tryAddWorker() {
            p.tasks <- task
            return nil
        }
        return ErrPoolFull
    }
}

func (p *BoundedDynamicPool) tryAddWorker() bool {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    if int(p.workers) >= p.maxWorkers {
        return false
    }
    
    atomic.AddInt64(&p.workers, 1)
    go p.worker()
    return true
}

func (p *BoundedDynamicPool) worker() {
    defer atomic.AddInt64(&p.workers, -1)
    
    idleTimeout := time.NewTimer(30 * time.Second)
    defer idleTimeout.Stop()
    
    for {
        select {
        case task := <-p.tasks:
            task.Execute()
            idleTimeout.Reset(30 * time.Second)
        case <-idleTimeout.C:
            // Idle too long, exit if above minimum
            if int(atomic.LoadInt64(&p.workers)) > p.minWorkers {
                return
            }
            idleTimeout.Reset(30 * time.Second)
        }
    }
}
```

### Cost Comparison

| Metric | Before Fix | After Fix |
|--------|------------|-----------|
| Peak workers | 5000 | 500 (max) |
| Idle workers | 4500 | 0 (scaled down) |
| Memory usage | 50GB | 5GB |
| Monthly cost | $200K | $20K |

---

## Case Study 4: The Connection Storm

### The Incident

**Company**: Microservices platform  
**Impact**: Database cluster failure  
**Root Cause**: Unbounded HTTP client connections

### Timeline

```
09:00 - Deploy new service version
09:01 - Each request creates new HTTP client
09:05 - 10K connections to downstream services
09:10 - Downstream DB connection pool exhausted
09:15 - Cascade failure across 12 services
09:30 - Full platform outage
10:30 - Rollback completed, service restored
```

### The Code

```go
// PROBLEMATIC: Original code
func callDownstreamService(url string) (*Response, error) {
    // New client for EVERY request!
    client := &http.Client{
        Timeout: 30 * time.Second,
    }
    
    resp, err := client.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    // Each client maintains its own connection pool
    // With 10K req/sec = 10K connection pools
    // Each pool opens connections to downstream
    // Result: Connection explosion
}
```

### The Fix

```go
// FIXED: Shared, configured client
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        MaxConnsPerHost:     20,
        IdleConnTimeout:     90 * time.Second,
    },
}

func callDownstreamService(url string) (*Response, error) {
    resp, err := httpClient.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    // Connections are reused from pool
    // Maximum 20 connections per host
    // Idle connections cleaned up after 90s
}
```

### Connection Metrics

| Metric | Before | After |
|--------|--------|-------|
| Connections per service | 10K+ | 20 (max) |
| Total downstream connections | 100K+ | 200 |
| Connection setup time | 50ms each | Reused |
| DB pool exhaustion | Yes | No |

---

## Lessons Learned

### 1. Always Set Upper Limits

```go
// BAD: Never do this
workers := 0
for task := range tasks {
    workers++
    go process(task)
}

// GOOD: Always do this
const maxWorkers = 500
sem := make(chan struct{}, maxWorkers)
for task := range tasks {
    sem <- struct{}{}
    go func(t Task) {
        defer func() { <-sem }()
        process(t)
    }(task)
}
```

### 2. Make Backpressure Visible

```go
// BAD: Silent failure
func queue(event Event) {
    select {
    case events <- event:
    default:
        // Silently dropped
    }
}

// GOOD: Visible failure
func queue(event Event) error {
    select {
    case events <- event:
        metrics.queued.Inc()
        return nil
    default:
        metrics.dropped.Inc()
        return ErrQueueFull
    }
}
```

### 3. Monitor Resource Counts

```go
// Essential metrics
prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "app_goroutines",
})
prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "app_queue_depth",
})
prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "app_connections_open",
})
prometheus.NewCounter(prometheus.CounterOpts{
    Name: "app_requests_rejected_total",
})
```

### 4. Test Under Load

```bash
# Regular load testing should be part of CI/CD
hey -n 100000 -c 1000 http://localhost:8080/api/endpoint

# Monitor during test
watch -n 1 "curl -s localhost:6060/debug/pprof/goroutine | head -1"
```

### 5. Implement Circuit Breakers

```go
// Protect downstream services
breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    MaxRequests: 100,
    Interval:    10 * time.Second,
    Timeout:     30 * time.Second,
})

result, err := breaker.Execute(func() (interface{}, error) {
    return callDownstream()
})
```

---

## Prevention Checklist

```markdown
## Before Deploying New Services

### Concurrency Limits
- [ ] Worker pool with max size defined
- [ ] Channel buffers reasonably sized
- [ ] HTTP client connection limits set
- [ ] Database connection pool configured

### Monitoring
- [ ] Goroutine count metric
- [ ] Queue depth metrics
- [ ] Connection count metrics
- [ ] Rejection/drop counters

### Alerting
- [ ] High goroutine count alert
- [ ] Queue depth threshold alert
- [ ] Connection pool exhaustion alert
- [ ] Error rate spike alert

### Testing
- [ ] Load tested at 2x expected peak
- [ ] Tested with artificial resource limits
- [ ] Chaos testing for downstream failures
```

---

## Summary

| Case Study | Root Cause | Impact | Prevention |
|------------|------------|--------|------------|
| Thundering Herd | Unbounded goroutines | 45-min outage | Worker pool |
| Silent Buffer | Oversized channel | 6-hour data loss | Bounded buffer + metrics |
| Auto-Scaling Trap | No max limit | $200K bill | Upper bounds + scale-down |
| Connection Storm | New client per request | Platform outage | Shared client + pool limits |

**Universal Lessons**:
1. Every resource needs an upper limit
2. Backpressure must be visible
3. Monitor resource counts, not just errors
4. Test under realistic load
5. Implement circuit breakers for dependencies

---

**Return to**: [Unbounded Resources README](../README.md)

