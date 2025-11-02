# Detection Methods: Finding Goroutine Leaks

**Read Time**: 20 minutes

**Prerequisites**: Basic understanding of profiling tools

**Related Topics**:
- [pprof Analysis](../pprof_analysis.md)
- [Context Pattern](./04-context-pattern.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Runtime Metrics](#runtime-metrics)
2. [pprof Profiling](#pprof-profiling)
3. [Automated Monitoring](#automated-monitoring)
4. [Testing for Leaks](#testing-for-leaks)
5. [Production Detection](#production-detection)
6. [Summary](#summary)

---

## Runtime Metrics

### NumGoroutine

The primary indicator of goroutine leaks:

```go
import "runtime"

count := runtime.NumGoroutine()
fmt.Printf("Current goroutines: %d\n", count)
```

### Building a Monitor

```go
func monitorGoroutines(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    baseline := runtime.NumGoroutine()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            current := runtime.NumGoroutine()
            delta := current - baseline
            
            fmt.Printf("[%s] Goroutines: %d (baseline: %d, delta: %+d)\n",
                time.Now().Format("15:04:05"), current, baseline, delta)
            
            if delta > 100 {
                fmt.Printf("WARNING: %d goroutines above baseline\n", delta)
            }
        }
    }
}
```

### Memory Statistics

```go
var m runtime.MemStats
runtime.ReadMemStats(&m)

fmt.Printf("Goroutines: %d\n", runtime.NumGoroutine())
fmt.Printf("Heap Alloc: %d MB\n", m.Alloc / 1024 / 1024)
fmt.Printf("Stack Inuse: %d MB\n", m.StackInuse / 1024 / 1024)
fmt.Printf("Num GC: %d\n", m.NumGC)
```

**Stack Inuse** growing with goroutine count = likely goroutine leak.

---

## pprof Profiling

### Setting Up pprof

```go
import (
    "net/http"
    _ "net/http/pprof"
)

func main() {
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    
    // Your application code
}
```

### Collecting Goroutine Profiles

```bash
# Snapshot at time T1
curl http://localhost:6060/debug/pprof/goroutine > goroutine_t1.pprof

# Wait 60 seconds
sleep 60

# Snapshot at time T2
curl http://localhost:6060/debug/pprof/goroutine > goroutine_t2.pprof

# Compare to see growth
go tool pprof -base=goroutine_t1.pprof goroutine_t2.pprof
```

### Analyzing Profiles

```bash
go tool pprof goroutine_t2.pprof

(pprof) top
(pprof) top -cum
(pprof) list functionName
(pprof) traces
```

### Human-Readable Format

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

Output shows:
```
goroutine 123 [chan send, 10 minutes]:
main.leakyFunc.func1()
    /path/to/file.go:42 +0x50
```

**"10 minutes" = Red flag!**

### Web UI

```bash
go tool pprof -http=:8081 goroutine_fixedEX.pprof
```

Features:
- Graph view: Visual call graph
- Flame graph: Hierarchical visualization
- Source view: Annotated source code
- Top: Ranked list

---

## Automated Monitoring

### Prometheus Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var goroutineGauge = promauto.NewGauge(prometheus.GaugeOpts{
    Name: "go_goroutines_current",
    Help: "Current number of goroutines",
})

func recordMetrics() {
    go func() {
        for {
            goroutineGauge.Set(float64(runtime.NumGoroutine()))
            time.Sleep(10 * time.Second)
        }
    }()
}
```

### Custom Alerting

```go
type GoroutineMonitor struct {
    baseline  int
    threshold int
    alertFunc func(current, baseline int)
}

func NewGoroutineMonitor(threshold int, alertFunc func(int, int)) *GoroutineMonitor {
    return &GoroutineMonitor{
        baseline:  runtime.NumGoroutine(),
        threshold: threshold,
        alertFunc: alertFunc,
    }
}

func (m *GoroutineMonitor) Check() {
    current := runtime.NumGoroutine()
    if current > m.baseline + m.threshold {
        m.alertFunc(current, m.baseline)
    }
}
```

### Logging Goroutine Count

```go
func startMetricsLogger(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            
            log.Printf("metrics: goroutines=%d stack_inuse=%dMB heap_alloc=%dMB",
                runtime.NumGoroutine(),
                m.StackInuse/1024/1024,
                m.Alloc/1024/1024)
        }
    }
}
```

---

## Testing for Leaks

### Basic Leak Test

```go
func TestNoGoroutineLeak(t *testing.T) {
    before := runtime.NumGoroutine()
    
    // Run the code that might leak
    ctx, cancel := context.WithCancel(context.Background())
    startWorkers(ctx)
    
    time.Sleep(100 * time.Millisecond)  // Let goroutines start
    cancel()  // Cancel them
    time.Sleep(100 * time.Millisecond)  // Let them stop
    
    after := runtime.NumGoroutine()
    
    if after > before {
        t.Errorf("Goroutine leak: before=%d after=%d leaked=%d", 
            before, after, after-before)
    }
}
```

### Using goleak

```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

func TestMyFunction(t *testing.T) {
    defer goleak.VerifyNone(t)
    
    // Test code that shouldn't leak goroutines
    result := myFunction()
    assert.Equal(t, expected, result)
}
```

### Table-Driven Leak Tests

```go
func TestNoLeaksInHandlers(t *testing.T) {
    tests := []struct {
        name    string
        handler func(context.Context)
    }{
        {"ProcessA", processA},
        {"ProcessB", processB},
        {"ProcessC", processC},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            before := runtime.NumGoroutine()
            
            ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
            tt.handler(ctx)
            cancel()
            
            time.Sleep(100 * time.Millisecond)
            after := runtime.NumGoroutine()
            
            if after > before {
                t.Errorf("%s leaked %d goroutines", tt.name, after-before)
            }
        })
    }
}
```

### Stress Testing for Leaks

```go
func TestNoLeakUnderLoad(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping stress test in short mode")
    }
    
    baseline := runtime.NumGoroutine()
    
    // Run many iterations
    for i := 0; i < 10000; i++ {
        processRequest()
        
        if i%1000 == 0 {
            current := runtime.NumGoroutine()
            if current > baseline + 10 {
                t.Fatalf("Leak detected at iteration %d: %d goroutines (baseline %d)",
                    i, current, baseline)
            }
        }
    }
    
    // Final check
    runtime.GC()
    time.Sleep(100 * time.Millisecond)
    final := runtime.NumGoroutine()
    
    if final > baseline + 5 {
        t.Errorf("Final leak: %d goroutines (baseline %d)", final, baseline)
    }
}
```

---

## Production Detection

### Health Check Endpoint

```go
func healthHandler(w http.ResponseWriter, r *http.Request) {
    goroutines := runtime.NumGoroutine()
    
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    health := map[string]interface{}{
        "goroutines":  goroutines,
        "stack_mb":    m.StackInuse / 1024 / 1024,
        "heap_mb":     m.Alloc / 1024 / 1024,
        "num_gc":      m.NumGC,
    }
    
    status := http.StatusOK
    if goroutines > 10000 {
        status = http.StatusServiceUnavailable
        health["warning"] = "High goroutine count"
    }
    
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(health)
}
```

### Continuous Profiling

```go
func startContinuousProfiling(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            collectProfile()
        }
    }
}

func collectProfile() {
    timestamp := time.Now().Format("20060102-150405")
    filename := fmt.Sprintf("goroutine-%s.pprof", timestamp)
    
    f, err := os.Create(filename)
    if err != nil {
        log.Printf("Failed to create profile: %v", err)
        return
    }
    defer f.Close()
    
    if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
        log.Printf("Failed to write profile: %v", err)
    }
}
```

### Alerting on Growth

```bash
#!/bin/bash
# Monitor goroutine count and alert

THRESHOLD=1000
ALERT_URL="https://alerts.example.com/webhook"

while true; do
    COUNT=$(curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1 | grep -oE '[0-9]+' | head -1)
    
    if [ "$COUNT" -gt "$THRESHOLD" ]; then
        echo "ALERT: High goroutine count: $COUNT"
        
        # Collect profile
        curl -s http://localhost:6060/debug/pprof/goroutine > "alert-$(date +%s).pprof"
        
        # Send alert
        curl -X POST "$ALERT_URL" -d "{\"goroutines\": $COUNT, \"threshold\": $THRESHOLD}"
    fi
    
    sleep 60
done
```

### Dashboard Metrics

Metrics to track:
1. **Goroutine count** (absolute)
2. **Goroutine growth rate** (per minute)
3. **Stack memory usage**
4. **Blocked goroutine count**
5. **Goroutine creation rate**

Example Grafana query (with Prometheus):
```promql
# Current goroutines
go_goroutines

# Growth rate
rate(go_goroutines[5m])

# Alert on sustained growth
increase(go_goroutines[1h]) > 100
```

---

## Summary

### Detection Strategy

1. **Development**: Use `goleak` in tests
2. **Staging**: Monitor goroutine count, collect profiles
3. **Production**: Continuous profiling, alerting, dashboards

### Red Flags

- Goroutine count growing monotonically
- Stack memory increasing with goroutines
- Many goroutines in "chan send/receive" for > 1 minute
- Identical stack traces repeated hundreds of times
- Goroutine count doesn't decrease during idle periods

### Quick Detection Checklist

```bash
# 1. Check current count
curl http://localhost:6060/debug/pprof/goroutine?debug=2 | head -1

# 2. Collect profile
curl http://localhost:6060/debug/pprof/goroutine > now.pprof

# 3. Wait and collect again
sleep 60
curl http://localhost:6060/debug/pprof/goroutine > later.pprof

# 4. Compare
go tool pprof -base=now.pprof later.pprof

# 5. If growth detected, examine in detail
go tool pprof -http=:8081 later.pprof
```

### Tools Summary

| Tool | Use Case | When to Use |
|------|----------|-------------|
| `runtime.NumGoroutine()` | Quick count | Always, in monitoring |
| `debug/pprof` | Detailed analysis | When leak suspected |
| `goleak` | Test-time detection | All tests |
| Prometheus | Time-series tracking | Production |
| `go tool trace` | Execution timeline | Understanding behavior |

---

## Further Reading

- [pprof Analysis Guide](../pprof_analysis.md)
- [Visual Diagrams](./06-visual-diagrams.md)
- [Real-World Examples](./07-real-world-examples.md)
- [pprof Complete Guide](../../tools-setup/pprof-complete-guide.md)

---

**Return to**: [Goroutine Leaks README](../README.md)

