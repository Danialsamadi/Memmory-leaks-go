# Capacity Planning for Go Services

**Read Time**: 20 minutes

**Prerequisites**: Understanding of concurrency, profiling, and system metrics

---

## Table of Contents

1. [Little's Law Application](#littles-law-application)
2. [Benchmarking for Limits](#benchmarking-for-limits)
3. [Resource Calculations](#resource-calculations)
4. [Dynamic Scaling](#dynamic-scaling)
5. [Production Guidelines](#production-guidelines)

---

## Little's Law Application

### The Formula

**L = λ × W**

Where:
- **L** = Average number of concurrent requests
- **λ** (lambda) = Average arrival rate (requests/second)
- **W** = Average time per request (seconds)

### Practical Examples

**Example 1: API Service**
```
Traffic: 1,000 requests/second
Latency: 50ms (0.05 seconds)
L = 1000 × 0.05 = 50 concurrent requests

Worker pool size: 50 minimum
With 2x safety margin: 100 workers
```

**Example 2: Database-Heavy Service**
```
Traffic: 500 requests/second
Latency: 200ms (0.2 seconds)
L = 500 × 0.2 = 100 concurrent requests

Worker pool size: 100 minimum
With 2x safety margin: 200 workers
```

**Example 3: External API Calls**
```
Traffic: 200 requests/second
Latency: 500ms (0.5 seconds)
L = 200 × 0.5 = 100 concurrent requests

Worker pool size: 100 minimum
With 2x safety margin: 200 workers
```

### Accounting for Variance

Real systems have variance. Use P99 latency, not average:

```go
// Calculate required capacity
func CalculateCapacity(rps float64, p99Latency time.Duration, safetyMargin float64) int {
    // Base capacity from Little's Law
    baseCapacity := rps * p99Latency.Seconds()
    
    // Add safety margin for spikes
    return int(baseCapacity * safetyMargin)
}

// Example
capacity := CalculateCapacity(
    1000,                    // 1000 req/sec
    200*time.Millisecond,    // P99 latency
    2.0,                     // 2x safety margin
)
// Result: 400 workers
```

---

## Benchmarking for Limits

### Step 1: Baseline Measurement

```go
func BenchmarkHandler(b *testing.B) {
    handler := NewHandler()
    req := createTestRequest()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        handler.Process(req)
    }
}
```

Run benchmark:
```bash
go test -bench=BenchmarkHandler -benchmem -benchtime=10s
```

### Step 2: Load Testing

Use `hey` or `wrk` for realistic load testing:

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Test with increasing concurrency
for c in 10 50 100 200 500 1000; do
    echo "Testing with $c concurrent connections"
    hey -n 10000 -c $c http://localhost:8080/api/endpoint
    sleep 5
done
```

### Step 3: Find Breaking Point

```go
func FindBreakingPoint() {
    concurrency := 10
    
    for {
        results := runLoadTest(concurrency)
        
        // Check for degradation
        if results.P99Latency > 5*time.Second ||
           results.ErrorRate > 0.01 {
            fmt.Printf("Breaking point at %d concurrent\n", concurrency)
            break
        }
        
        concurrency += 10
    }
}
```

### Step 4: Determine Safe Limits

```
Breaking point: 500 concurrent
Safe operating limit: 500 × 0.7 = 350 concurrent
Peak capacity: 500 × 0.9 = 450 concurrent
```

---

## Resource Calculations

### Memory per Goroutine

```go
func MeasureGoroutineMemory() {
    var m1, m2 runtime.MemStats
    
    runtime.GC()
    runtime.ReadMemStats(&m1)
    
    // Create 10,000 goroutines
    var wg sync.WaitGroup
    for i := 0; i < 10000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            time.Sleep(10 * time.Second)
        }()
    }
    
    runtime.ReadMemStats(&m2)
    
    memPerGoroutine := (m2.Alloc - m1.Alloc) / 10000
    fmt.Printf("Memory per goroutine: %d bytes\n", memPerGoroutine)
}

// Typical results:
// - Minimal goroutine: 2-4 KB
// - With allocations: 10-50 KB
// - Heavy processing: 100+ KB
```

### Maximum Goroutines by Memory

```go
func MaxGoroutinesByMemory(availableMemory, memPerGoroutine int64) int64 {
    // Reserve 20% for GC overhead
    usableMemory := int64(float64(availableMemory) * 0.8)
    return usableMemory / memPerGoroutine
}

// Example: 4GB available, 50KB per goroutine
// Max = (4GB × 0.8) / 50KB = 64,000 goroutines
```

### File Descriptor Limits

```bash
# Check current limit
ulimit -n

# Check system max
cat /proc/sys/fs/file-max

# Set for current session
ulimit -n 65535
```

```go
func MaxConnectionsByFD() int {
    // Get soft limit
    var rLimit syscall.Rlimit
    syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
    
    // Reserve some for other operations
    reserved := uint64(100)
    maxConnections := rLimit.Cur - reserved
    
    return int(maxConnections)
}
```

### Database Connection Pool

```go
func CalculateDBPoolSize(
    maxQueries int,        // Max concurrent queries
    avgQueryTime time.Duration,
    targetLatency time.Duration,
) int {
    // Pool should handle max queries within target latency
    poolSize := int(float64(maxQueries) * avgQueryTime.Seconds() / targetLatency.Seconds())
    
    // Minimum pool size
    if poolSize < 5 {
        poolSize = 5
    }
    
    // Maximum practical size
    if poolSize > 100 {
        poolSize = 100
    }
    
    return poolSize
}

// Example: 500 queries/sec, 20ms avg, 100ms target
// Pool = 500 × 0.02 / 0.1 = 100 connections
```

---

## Dynamic Scaling

### Auto-Scaling Worker Pool

```go
type AutoScalingPool struct {
    minWorkers    int
    maxWorkers    int
    currentWorkers int64
    tasks         chan func()
    metrics       *PoolMetrics
    wg            sync.WaitGroup
}

type PoolMetrics struct {
    queueDepth    int64
    avgLatency    time.Duration
    workerUtil    float64
}

func (asp *AutoScalingPool) autoScale() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        current := int(atomic.LoadInt64(&asp.currentWorkers))
        queueDepth := atomic.LoadInt64(&asp.metrics.queueDepth)
        
        // Scale up conditions
        if queueDepth > int64(current)*2 && current < asp.maxWorkers {
            toAdd := min(current/2, asp.maxWorkers-current)
            for i := 0; i < toAdd; i++ {
                asp.addWorker()
            }
            log.Printf("Scaled up to %d workers", current+toAdd)
        }
        
        // Scale down conditions
        if queueDepth < int64(current)/4 && current > asp.minWorkers {
            toRemove := min(current/4, current-asp.minWorkers)
            for i := 0; i < toRemove; i++ {
                asp.removeWorker()
            }
            log.Printf("Scaled down to %d workers", current-toRemove)
        }
    }
}
```

### Metrics-Driven Scaling

```go
type ScalingPolicy struct {
    ScaleUpThreshold   float64 // e.g., 0.8 (80% utilization)
    ScaleDownThreshold float64 // e.g., 0.3 (30% utilization)
    CooldownPeriod     time.Duration
    lastScaleTime      time.Time
}

func (sp *ScalingPolicy) ShouldScaleUp(utilization float64) bool {
    if time.Since(sp.lastScaleTime) < sp.CooldownPeriod {
        return false
    }
    return utilization > sp.ScaleUpThreshold
}

func (sp *ScalingPolicy) ShouldScaleDown(utilization float64) bool {
    if time.Since(sp.lastScaleTime) < sp.CooldownPeriod {
        return false
    }
    return utilization < sp.ScaleDownThreshold
}
```

---

## Production Guidelines

### Capacity Planning Checklist

```markdown
## Pre-Launch Checklist

### Traffic Estimation
- [ ] Expected peak RPS
- [ ] Expected average RPS
- [ ] Traffic patterns (spikes, daily cycles)
- [ ] Growth projections (6 months, 1 year)

### Resource Limits
- [ ] Worker pool size calculated
- [ ] Channel buffer sizes set
- [ ] Database pool size configured
- [ ] File descriptor limits checked
- [ ] Memory limits set

### Safety Margins
- [ ] 2x headroom for traffic spikes
- [ ] 20% memory reserved for GC
- [ ] 80% max utilization target

### Monitoring
- [ ] Goroutine count tracked
- [ ] Queue depths monitored
- [ ] Latency percentiles (P50, P95, P99)
- [ ] Error rates tracked
- [ ] Resource utilization dashboards

### Alerts
- [ ] High goroutine count alert
- [ ] Queue depth threshold alert
- [ ] Latency degradation alert
- [ ] Error rate spike alert
```

### Recommended Defaults

```go
const (
    // Worker pools
    DefaultWorkers   = 100
    DefaultQueueSize = 1000
    
    // Timeouts
    DefaultRequestTimeout = 30 * time.Second
    DefaultDBTimeout      = 5 * time.Second
    DefaultHTTPTimeout    = 10 * time.Second
    
    // Limits
    MaxGoroutines    = 10000
    MaxQueueDepth    = 5000
    MaxMemoryPercent = 80
    
    // Scaling
    ScaleUpThreshold   = 0.8
    ScaleDownThreshold = 0.3
    CooldownPeriod     = 5 * time.Minute
)
```

### Monitoring Queries (Prometheus)

```promql
# Goroutine count
go_goroutines

# Memory usage
go_memstats_alloc_bytes / go_memstats_sys_bytes

# Request rate
rate(http_requests_total[1m])

# Latency P99
histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))

# Queue depth
app_queue_depth

# Worker utilization
app_workers_busy / app_workers_total
```

---

## Summary

| Metric | Calculation | Example |
|--------|-------------|---------|
| Concurrent requests | RPS × Latency | 1000 × 0.1 = 100 |
| Worker pool | Concurrent × 2 | 100 × 2 = 200 |
| Queue size | Workers × 5 | 200 × 5 = 1000 |
| Max goroutines | Memory / Per-goroutine | 4GB / 50KB = 80K |
| DB pool | Queries × Query time / Target latency | 500 × 0.02 / 0.1 = 100 |

**Key Principles**:
1. Use Little's Law for baseline calculations
2. Benchmark to find actual breaking points
3. Add safety margins (2x typical)
4. Monitor and alert on resource metrics
5. Plan for growth (6-12 months ahead)

---

**Next**: [Production Case Studies](./07-production-case-studies.md)

