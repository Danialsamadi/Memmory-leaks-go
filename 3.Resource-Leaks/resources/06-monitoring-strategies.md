# Resource Monitoring Strategies

**Read Time**: ~20 minutes

**Prerequisites**: Basic understanding of system monitoring and metrics

**Summary**: Learn comprehensive strategies for monitoring resource usage in production Go applications, setting up alerts, and implementing proactive detection systems.

---

## Introduction

Resource monitoring is critical for preventing production incidents. Unlike memory leaks that grow gradually, resource leaks cause sudden failures when OS limits are hit. This guide covers monitoring strategies from system-level tools to application metrics.

## Multi-Layer Monitoring Architecture

```
┌─────────────────────────────────────────────┐
│           Application Layer                 │
│   • Go runtime metrics                     │
│   • Custom resource counters               │
│   • pprof profiling endpoints              │
└─────────────────────────────────────────────┘
              ↓ reports to ↓
┌─────────────────────────────────────────────┐
│           Metrics Layer                     │
│   • Prometheus/Grafana                     │
│   • StatsD/DataDog                         │
│   • Custom dashboards                      │
└─────────────────────────────────────────────┘
              ↓ alerts via ↓
┌─────────────────────────────────────────────┐
│           Alerting Layer                    │
│   • PagerDuty/OpsGenie                     │
│   • Slack notifications                    │
│   • Email alerts                           │
└─────────────────────────────────────────────┘
              ↓ escalates to ↓
┌─────────────────────────────────────────────┐
│           System Layer                      │
│   • OS-level monitoring (lsof, netstat)    │
│   • Container metrics (Docker/K8s)         │
│   • Kernel-level tracing                   │
└─────────────────────────────────────────────┘
```

## System-Level Monitoring

### File Descriptor Monitoring

**Real-time FD tracking**:

```bash
#!/bin/bash
# fd_monitor.sh - Track file descriptor usage

PID=$1
THRESHOLD=800
LIMIT=$(ulimit -n)

while true; do
    FD_COUNT=$(lsof -p $PID 2>/dev/null | wc -l)
    PERCENTAGE=$((FD_COUNT * 100 / LIMIT))
    
    echo "$(date): PID $PID using $FD_COUNT/$LIMIT FDs ($PERCENTAGE%)"
    
    if [ $FD_COUNT -gt $THRESHOLD ]; then
        echo "WARNING: High FD usage detected!"
        # Trigger alert
        curl -X POST "https://hooks.slack.com/..." \
             -d "{\"text\":\"FD leak detected: $FD_COUNT/$LIMIT\"}"
    fi
    
    sleep 10
done
```

**Categorize open files**:

```bash
#!/bin/bash
# categorize_fds.sh - Analyze what types of FDs are open

PID=$1

echo "=== File Descriptor Analysis for PID $PID ==="
echo "Regular files:"
lsof -p $PID | grep REG | wc -l

echo "Network sockets:"
lsof -p $PID | grep -E "(IPv4|IPv6)" | wc -l

echo "Pipes:"
lsof -p $PID | grep FIFO | wc -l

echo "Character devices:"
lsof -p $PID | grep CHR | wc -l

echo "Top 10 most opened files:"
lsof -p $PID | awk '{print $NF}' | sort | uniq -c | sort -rn | head -10
```

### Network Connection Monitoring

**Connection state analysis**:

```bash
#!/bin/bash
# connection_monitor.sh - Monitor TCP connection states

APP_PORT=8080

echo "=== Connection Analysis ==="
echo "ESTABLISHED connections:"
netstat -an | grep ":$APP_PORT" | grep ESTABLISHED | wc -l

echo "CLOSE_WAIT connections (potential leak):"
netstat -an | grep ":$APP_PORT" | grep CLOSE_WAIT | wc -l

echo "TIME_WAIT connections:"
netstat -an | grep ":$APP_PORT" | grep TIME_WAIT | wc -l

echo "Connection states breakdown:"
netstat -an | grep ":$APP_PORT" | awk '{print $6}' | sort | uniq -c
```

**Continuous monitoring with alerting**:

```bash
#!/bin/bash
# network_leak_detector.sh

CLOSE_WAIT_THRESHOLD=100
TIME_WAIT_THRESHOLD=1000

while true; do
    CLOSE_WAIT=$(netstat -an | grep CLOSE_WAIT | wc -l)
    TIME_WAIT=$(netstat -an | grep TIME_WAIT | wc -l)
    
    if [ $CLOSE_WAIT -gt $CLOSE_WAIT_THRESHOLD ]; then
        echo "ALERT: $CLOSE_WAIT connections in CLOSE_WAIT (threshold: $CLOSE_WAIT_THRESHOLD)"
        # This indicates application-side connection leaks
    fi
    
    if [ $TIME_WAIT -gt $TIME_WAIT_THRESHOLD ]; then
        echo "WARNING: $TIME_WAIT connections in TIME_WAIT (threshold: $TIME_WAIT_THRESHOLD)"
        # This might indicate high connection churn
    fi
    
    sleep 30
done
```

## Application-Level Monitoring

### Go Runtime Metrics

**Comprehensive runtime monitoring**:

```go
package main

import (
    "context"
    "log"
    "net/http"
    "runtime"
    "time"
    
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    // Goroutine metrics
    goroutineGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "go_goroutines_current",
        Help: "Current number of goroutines",
    })
    
    // Memory metrics
    heapAllocGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "go_memory_heap_alloc_bytes",
        Help: "Current heap allocation in bytes",
    })
    
    heapSysGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "go_memory_heap_sys_bytes", 
        Help: "Heap system memory in bytes",
    })
    
    // GC metrics
    gcRunsCounter = promauto.NewCounter(prometheus.CounterOpts{
        Name: "go_gc_runs_total",
        Help: "Total number of GC runs",
    })
    
    // Custom resource metrics
    filesOpenGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_files_open_current",
        Help: "Current number of open files",
    })
    
    httpConnectionsGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_http_connections_current",
        Help: "Current number of HTTP connections",
    })
)

func startMetricsCollection() {
    ticker := time.NewTicker(15 * time.Second)
    go func() {
        var lastGC uint32
        
        for range ticker.C {
            // Goroutine count
            goroutineGauge.Set(float64(runtime.NumGoroutine()))
            
            // Memory stats
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            
            heapAllocGauge.Set(float64(m.Alloc))
            heapSysGauge.Set(float64(m.HeapSys))
            
            // GC runs (increment counter)
            if m.NumGC > lastGC {
                gcRunsCounter.Add(float64(m.NumGC - lastGC))
                lastGC = m.NumGC
            }
            
            // Custom metrics
            filesOpenGauge.Set(float64(countOpenFiles()))
            httpConnectionsGauge.Set(float64(countHTTPConnections()))
        }
    }()
}

func countOpenFiles() int {
    // Implementation depends on OS
    // On Linux: count files in /proc/self/fd
    // On macOS: use lsof or estimate
    return 42 // Placeholder
}

func countHTTPConnections() int {
    // Count active HTTP connections
    // This requires custom tracking in your HTTP client
    return 10 // Placeholder
}

func main() {
    startMetricsCollection()
    
    // Expose metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    
    log.Println("Metrics server starting on :9090")
    log.Fatal(http.ListenAndServe(":9090", nil))
}
```

### Custom Resource Tracking

**HTTP client with connection tracking**:

```go
type TrackedHTTPClient struct {
    client      *http.Client
    activeConns int64
    totalReqs   int64
    failedReqs  int64
    mu          sync.RWMutex
}

func NewTrackedHTTPClient() *TrackedHTTPClient {
    transport := &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    }
    
    return &TrackedHTTPClient{
        client: &http.Client{
            Transport: transport,
            Timeout:   30 * time.Second,
        },
    }
}

func (c *TrackedHTTPClient) Do(req *http.Request) (*http.Response, error) {
    c.mu.Lock()
    c.activeConns++
    c.totalReqs++
    c.mu.Unlock()
    
    start := time.Now()
    resp, err := c.client.Do(req)
    duration := time.Since(start)
    
    c.mu.Lock()
    c.activeConns--
    if err != nil {
        c.failedReqs++
    }
    c.mu.Unlock()
    
    // Record metrics
    httpRequestDuration.Observe(duration.Seconds())
    httpActiveConnections.Set(float64(c.activeConns))
    
    if resp != nil {
        httpRequestsTotal.WithLabelValues(
            req.Method,
            fmt.Sprintf("%d", resp.StatusCode),
        ).Inc()
    }
    
    return resp, err
}

func (c *TrackedHTTPClient) GetStats() (active, total, failed int64) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.activeConns, c.totalReqs, c.failedReqs
}
```

**Database connection pool monitoring**:

```go
type TrackedDB struct {
    *sql.DB
    stats *sql.DBStats
}

func NewTrackedDB(driverName, dataSourceName string) (*TrackedDB, error) {
    db, err := sql.Open(driverName, dataSourceName)
    if err != nil {
        return nil, err
    }
    
    // Configure connection pool
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)
    
    tracked := &TrackedDB{DB: db}
    
    // Start monitoring goroutine
    go tracked.monitorStats()
    
    return tracked, nil
}

func (db *TrackedDB) monitorStats() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := db.DB.Stats()
        
        // Export to Prometheus
        dbConnectionsOpen.Set(float64(stats.OpenConnections))
        dbConnectionsInUse.Set(float64(stats.InUse))
        dbConnectionsIdle.Set(float64(stats.Idle))
        dbConnectionWaitCount.Set(float64(stats.WaitCount))
        dbConnectionWaitDuration.Set(stats.WaitDuration.Seconds())
        dbConnectionsMaxIdleClosed.Set(float64(stats.MaxIdleClosed))
        dbConnectionsMaxLifetimeClosed.Set(float64(stats.MaxLifetimeClosed))
        
        // Log warnings
        if stats.WaitCount > 0 {
            log.Printf("DB connection pool pressure: %d waits, avg duration: %v",
                stats.WaitCount, stats.WaitDuration/time.Duration(stats.WaitCount))
        }
        
        if float64(stats.OpenConnections)/25.0 > 0.8 { // 80% of max
            log.Printf("DB connection pool nearly full: %d/25 connections",
                stats.OpenConnections)
        }
    }
}
```

## Prometheus Metrics and Alerting

### Key Metrics to Track

```yaml
# prometheus.yml - Key metrics for resource leak detection

groups:
- name: resource_leaks
  rules:
  
  # Goroutine leak detection
  - alert: GoroutineLeakDetected
    expr: go_goroutines_current > 10000
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "Potential goroutine leak detected"
      description: "{{ $labels.instance }} has {{ $value }} goroutines"
  
  # File descriptor leak
  - alert: FileDescriptorLeakDetected
    expr: app_files_open_current > 800
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "High file descriptor usage"
      description: "{{ $labels.instance }} has {{ $value }} open files"
  
  # HTTP connection leak
  - alert: HTTPConnectionLeakDetected
    expr: app_http_connections_current > 100
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "HTTP connection leak suspected"
      description: "{{ $labels.instance }} has {{ $value }} active connections"
  
  # Database connection pool exhaustion
  - alert: DBConnectionPoolExhausted
    expr: db_connections_in_use / db_connections_max_open > 0.9
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "Database connection pool nearly exhausted"
      description: "{{ $labels.instance }} using {{ $value }}% of DB connections"
  
  # Memory growth rate (potential leak)
  - alert: MemoryGrowthRate
    expr: rate(go_memory_heap_alloc_bytes[5m]) > 1048576  # 1MB/sec
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "High memory growth rate"
      description: "Memory growing at {{ $value }} bytes/sec"
```

### Grafana Dashboard Configuration

```json
{
  "dashboard": {
    "title": "Go Resource Monitoring",
    "panels": [
      {
        "title": "Goroutines Over Time",
        "type": "graph",
        "targets": [
          {
            "expr": "go_goroutines_current",
            "legendFormat": "Goroutines"
          }
        ],
        "yAxes": [
          {
            "label": "Count",
            "min": 0
          }
        ],
        "alert": {
          "conditions": [
            {
              "query": {"queryType": "", "refId": "A"},
              "reducer": {"type": "last"},
              "evaluator": {"params": [10000], "type": "gt"}
            }
          ],
          "executionErrorState": "alerting",
          "for": "5m",
          "frequency": "10s",
          "handler": 1,
          "name": "Goroutine Leak Alert",
          "noDataState": "no_data"
        }
      },
      {
        "title": "File Descriptors",
        "type": "stat",
        "targets": [
          {
            "expr": "app_files_open_current",
            "legendFormat": "Open Files"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "thresholds": {
              "steps": [
                {"color": "green", "value": null},
                {"color": "yellow", "value": 500},
                {"color": "red", "value": 800}
              ]
            }
          }
        }
      },
      {
        "title": "HTTP Connection Pool",
        "type": "graph",
        "targets": [
          {
            "expr": "app_http_connections_current",
            "legendFormat": "Active Connections"
          },
          {
            "expr": "rate(http_requests_total[5m])",
            "legendFormat": "Request Rate"
          }
        ]
      },
      {
        "title": "Database Connection Pool",
        "type": "graph",
        "targets": [
          {
            "expr": "db_connections_open",
            "legendFormat": "Open"
          },
          {
            "expr": "db_connections_in_use", 
            "legendFormat": "In Use"
          },
          {
            "expr": "db_connections_idle",
            "legendFormat": "Idle"
          }
        ]
      }
    ]
  }
}
```

## Automated Detection Scripts

### Leak Detection Automation

```go
// leak_detector.go - Automated leak detection service
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "runtime"
    "time"
)

type LeakDetector struct {
    thresholds Thresholds
    alerts     AlertManager
}

type Thresholds struct {
    MaxGoroutines    int
    MaxFileHandles   int
    MaxHTTPConns     int
    MemoryGrowthRate float64 // MB per minute
}

type AlertManager interface {
    SendAlert(level string, message string) error
}

func NewLeakDetector() *LeakDetector {
    return &LeakDetector{
        thresholds: Thresholds{
            MaxGoroutines:    10000,
            MaxFileHandles:   800,
            MaxHTTPConns:     100,
            MemoryGrowthRate: 10.0, // 10MB/min
        },
        alerts: &SlackAlertManager{},
    }
}

func (ld *LeakDetector) Start(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    var lastMemory uint64
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            ld.checkForLeaks(&lastMemory)
        }
    }
}

func (ld *LeakDetector) checkForLeaks(lastMemory *uint64) {
    // Check goroutines
    goroutines := runtime.NumGoroutine()
    if goroutines > ld.thresholds.MaxGoroutines {
        ld.alerts.SendAlert("CRITICAL", 
            fmt.Sprintf("Goroutine leak detected: %d goroutines", goroutines))
    }
    
    // Check memory growth
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    if *lastMemory > 0 {
        growth := float64(m.Alloc-*lastMemory) / 1024 / 1024 // MB
        if growth > ld.thresholds.MemoryGrowthRate {
            ld.alerts.SendAlert("WARNING",
                fmt.Sprintf("High memory growth: %.2f MB/min", growth))
        }
    }
    *lastMemory = m.Alloc
    
    // Check file handles (OS-specific implementation needed)
    fileHandles := countFileHandles()
    if fileHandles > ld.thresholds.MaxFileHandles {
        ld.alerts.SendAlert("WARNING",
            fmt.Sprintf("High file handle usage: %d handles", fileHandles))
    }
}

type SlackAlertManager struct {
    webhookURL string
}

func (s *SlackAlertManager) SendAlert(level, message string) error {
    // Implementation for Slack webhook
    payload := fmt.Sprintf(`{"text": "[%s] %s"}`, level, message)
    
    resp, err := http.Post(s.webhookURL, "application/json", 
        strings.NewReader(payload))
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    return nil
}

func countFileHandles() int {
    // OS-specific implementation
    // Linux: count files in /proc/self/fd
    // macOS: use lsof or estimate
    return 42 // Placeholder
}
```

## Container and Kubernetes Monitoring

### Docker Container Monitoring

```bash
#!/bin/bash
# docker_resource_monitor.sh

CONTAINER_NAME=$1

echo "=== Docker Container Resource Monitor ==="
echo "Container: $CONTAINER_NAME"

while true; do
    # Get container stats
    STATS=$(docker stats $CONTAINER_NAME --no-stream --format "table {{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}")
    
    # Get process info from inside container
    PID=$(docker inspect $CONTAINER_NAME | jq -r '.[0].State.Pid')
    
    if [ "$PID" != "null" ]; then
        FD_COUNT=$(lsof -p $PID 2>/dev/null | wc -l)
        echo "$(date): $STATS | FDs: $FD_COUNT"
        
        # Check for resource pressure
        if [ $FD_COUNT -gt 500 ]; then
            echo "WARNING: High FD usage in container"
        fi
    fi
    
    sleep 30
done
```

### Kubernetes Resource Monitoring

```yaml
# k8s-resource-monitor.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: resource-monitor-script
data:
  monitor.sh: |
    #!/bin/bash
    while true; do
      echo "=== Resource Monitor $(date) ==="
      echo "File descriptors: $(ls /proc/self/fd | wc -l)"
      echo "Goroutines: $(curl -s localhost:6060/debug/pprof/goroutine?debug=1 | grep -c 'goroutine')"
      echo "Memory: $(cat /proc/meminfo | grep MemAvailable)"
      sleep 60
    done

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-with-monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: app
        image: myapp:latest
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "250m"
        # Resource monitoring sidecar
      - name: resource-monitor
        image: alpine:latest
        command: ["/bin/sh", "/scripts/monitor.sh"]
        volumeMounts:
        - name: monitor-script
          mountPath: /scripts
        - name: proc
          mountPath: /host/proc
          readOnly: true
      volumes:
      - name: monitor-script
        configMap:
          name: resource-monitor-script
          defaultMode: 0755
      - name: proc
        hostPath:
          path: /proc
```

## Best Practices Summary

### Monitoring Hierarchy

1. **Real-time Alerts** (< 1 minute detection)
   - Critical resource exhaustion
   - Service unavailability

2. **Trend Monitoring** (5-15 minute windows)
   - Resource growth patterns
   - Performance degradation

3. **Capacity Planning** (daily/weekly analysis)
   - Resource usage trends
   - Scaling decisions

### Alert Thresholds

| Resource | Warning | Critical | Action |
|----------|---------|----------|---------|
| **Goroutines** | > 5,000 | > 10,000 | Investigate/restart |
| **File Descriptors** | > 70% limit | > 90% limit | Immediate action |
| **HTTP Connections** | > 50 active | > 100 active | Check for leaks |
| **DB Connections** | > 80% pool | > 95% pool | Scale or fix leaks |
| **Memory Growth** | > 5MB/min | > 20MB/min | Memory leak likely |

### Monitoring Checklist

- [ ] **System-level monitoring** (lsof, netstat) in production
- [ ] **Application metrics** exported to monitoring system
- [ ] **Automated alerting** on resource thresholds
- [ ] **Dashboard visualization** for trend analysis
- [ ] **Runbook procedures** for common leak scenarios
- [ ] **Regular monitoring review** and threshold tuning

## Key Takeaways

1. **Monitor at multiple levels** - system, application, and business metrics

2. **Set progressive alerts** - warning before critical thresholds

3. **Automate detection** - don't rely on manual monitoring

4. **Track trends over time** - sudden changes indicate problems

5. **Test your monitoring** - ensure alerts fire when expected

6. **Document procedures** - clear runbooks for incident response

7. **Regular review** - adjust thresholds based on actual usage patterns

---

## References

- https://prometheus.io/docs/practices/naming/ - Prometheus metric naming
- https://grafana.com/docs/grafana/latest/dashboards/ - Grafana dashboard best practices
- https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ - K8s resource management

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - Basic resource management
- [Production Case Studies](07-production-case-studies.md) - Real monitoring scenarios
- [File Descriptor Internals](02-file-descriptor-internals.md) - OS-level monitoring details