# Prometheus Setup for Go Applications

**Metrics collection and alerting for leak detection**

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Go Application Integration](#go-application-integration)
3. [Essential Metrics](#essential-metrics)
4. [Alerting Rules](#alerting-rules)
5. [Docker Setup](#docker-setup)

---

## Quick Start

### Install Prometheus

**macOS**:
```bash
brew install prometheus
```

**Linux**:
```bash
wget https://github.com/prometheus/prometheus/releases/download/v2.45.0/prometheus-2.45.0.linux-amd64.tar.gz
tar xvfz prometheus-*.tar.gz
cd prometheus-*
./prometheus --config.file=prometheus.yml
```

**Docker**:
```bash
docker run -p 9090:9090 prom/prometheus
```

### Basic Configuration

Create `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'go-app'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
```

---

## Go Application Integration

### Add Prometheus Client

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
```

### Basic Setup

```go
package main

import (
    "net/http"
    
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
    // Expose metrics endpoint
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":8080", nil)
}
```

### With Custom Metrics

```go
package main

import (
    "net/http"
    "runtime"
    "time"
    
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
    // Counters
    requestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "app_requests_total",
            Help: "Total number of requests",
        },
        []string{"method", "endpoint", "status"},
    )
    
    // Gauges
    goroutinesGauge = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "app_goroutines",
            Help: "Current number of goroutines",
        },
    )
    
    queueDepth = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "app_queue_depth",
            Help: "Current queue depth",
        },
    )
    
    // Histograms
    requestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "app_request_duration_seconds",
            Help:    "Request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"endpoint"},
    )
)

func recordMetrics() {
    go func() {
        for {
            goroutinesGauge.Set(float64(runtime.NumGoroutine()))
            time.Sleep(5 * time.Second)
        }
    }()
}

func main() {
    recordMetrics()
    
    http.Handle("/metrics", promhttp.Handler())
    http.HandleFunc("/api/", instrumentedHandler)
    http.ListenAndServe(":8080", nil)
}

func instrumentedHandler(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    
    // Your handler logic here
    w.WriteHeader(http.StatusOK)
    
    // Record metrics
    duration := time.Since(start).Seconds()
    requestDuration.WithLabelValues(r.URL.Path).Observe(duration)
    requestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
}
```

---

## Essential Metrics

### Memory Leak Detection Metrics

```go
var (
    // Heap memory
    heapAlloc = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_heap_alloc_bytes",
        Help: "Current heap allocation in bytes",
    })
    
    heapObjects = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_heap_objects",
        Help: "Current number of heap objects",
    })
    
    // GC metrics
    gcPauseTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "app_gc_pause_total_seconds",
        Help: "Total GC pause time",
    })
)

func collectMemoryMetrics() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    heapAlloc.Set(float64(m.Alloc))
    heapObjects.Set(float64(m.HeapObjects))
}
```

### Goroutine Leak Detection Metrics

```go
var (
    goroutines = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_goroutines",
        Help: "Current number of goroutines",
    })
    
    goroutinesByState = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "app_goroutines_by_function",
            Help: "Goroutines by function",
        },
        []string{"function"},
    )
)
```

### Resource Leak Detection Metrics

```go
var (
    openFiles = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_open_files",
        Help: "Current number of open files",
    })
    
    openConnections = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_open_connections",
        Help: "Current number of open connections",
    })
    
    dbPoolActive = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_db_pool_active",
        Help: "Active database connections",
    })
    
    dbPoolIdle = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_db_pool_idle",
        Help: "Idle database connections",
    })
)
```

### Queue and Backpressure Metrics

```go
var (
    queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_queue_depth",
        Help: "Current queue depth",
    })
    
    queueCapacity = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_queue_capacity",
        Help: "Queue capacity",
    })
    
    tasksRejected = promauto.NewCounter(prometheus.CounterOpts{
        Name: "app_tasks_rejected_total",
        Help: "Total rejected tasks due to backpressure",
    })
)
```

---

## Alerting Rules

Create `alerts.yml`:

```yaml
groups:
  - name: go-app-leaks
    rules:
      # Goroutine leak detection
      - alert: GoroutineLeakSuspected
        expr: rate(app_goroutines[5m]) > 10
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Goroutine count growing"
          description: "Goroutine count is increasing at {{ $value }}/sec"
      
      - alert: GoroutineCountHigh
        expr: app_goroutines > 5000
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High goroutine count"
          description: "Goroutine count is {{ $value }}"
      
      # Memory leak detection
      - alert: MemoryLeakSuspected
        expr: rate(app_heap_alloc_bytes[1h]) > 10000000
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "Memory growing steadily"
          description: "Heap growing at {{ $value | humanize }}B/sec"
      
      - alert: HighMemoryUsage
        expr: app_heap_alloc_bytes > 1073741824
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High memory usage"
          description: "Heap is {{ $value | humanize }}B"
      
      # Queue backpressure
      - alert: QueueBackpressure
        expr: app_queue_depth / app_queue_capacity > 0.8
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Queue near capacity"
          description: "Queue is {{ $value | humanizePercentage }} full"
      
      - alert: TasksBeingRejected
        expr: rate(app_tasks_rejected_total[5m]) > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Tasks being rejected"
          description: "{{ $value }} tasks/sec being rejected"
      
      # Resource leaks
      - alert: FileDescriptorLeak
        expr: app_open_files > 500
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High file descriptor count"
          description: "{{ $value }} files open"
      
      - alert: ConnectionPoolExhausted
        expr: app_db_pool_active / (app_db_pool_active + app_db_pool_idle) > 0.9
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Database pool near exhaustion"
          description: "Pool is {{ $value | humanizePercentage }} utilized"
```

Add to `prometheus.yml`:

```yaml
rule_files:
  - "alerts.yml"
```

---

## Docker Setup

### docker-compose.yml

```yaml
version: '3.8'

services:
  prometheus:
    image: prom/prometheus:v2.45.0
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./alerts.yml:/etc/prometheus/alerts.yml
      - prometheus_data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.enable-lifecycle'
    networks:
      - monitoring

  alertmanager:
    image: prom/alertmanager:v0.25.0
    ports:
      - "9093:9093"
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
    networks:
      - monitoring

  go-app:
    build: .
    ports:
      - "8080:8080"
    networks:
      - monitoring

networks:
  monitoring:
    driver: bridge

volumes:
  prometheus_data:
```

### alertmanager.yml

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'slack-notifications'

receivers:
  - name: 'slack-notifications'
    slack_configs:
      - api_url: 'YOUR_SLACK_WEBHOOK_URL'
        channel: '#alerts'
        send_resolved: true
```

---

## Useful PromQL Queries

### Goroutine Analysis

```promql
# Current goroutine count
app_goroutines

# Goroutine growth rate (per minute)
rate(app_goroutines[5m]) * 60

# Goroutine count change over 1 hour
app_goroutines - app_goroutines offset 1h
```

### Memory Analysis

```promql
# Current heap size
app_heap_alloc_bytes

# Memory growth rate
rate(app_heap_alloc_bytes[5m])

# Heap size as percentage of system memory
app_heap_alloc_bytes / go_memstats_sys_bytes * 100
```

### Queue Analysis

```promql
# Queue utilization percentage
app_queue_depth / app_queue_capacity * 100

# Task rejection rate
rate(app_tasks_rejected_total[5m])

# Average queue depth over time
avg_over_time(app_queue_depth[1h])
```

---

**Return to**: [Tools Setup](./README.md)

