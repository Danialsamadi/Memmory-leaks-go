# Production Examples: Real-World Memory Leak Cases

**Read Time**: 30 minutes

**Prerequisites**: Understanding all previous resources

**Related Topics**: 
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [Cache Patterns](./04-cache-patterns.md)
- [Eviction Strategies](./05-eviction-strategies.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Case Study 1: E-Commerce Session Store](#case-study-1-e-commerce-session-store)
2. [Case Study 2: Image Processing Service](#case-study-2-image-processing-service)
3. [Case Study 3: Metrics Aggregation System](#case-study-3-metrics-aggregation-system)
4. [Case Study 4: WebSocket Connection Manager](#case-study-4-websocket-connection-manager)
5. [Case Study 5: Log Processing Pipeline](#case-study-5-log-processing-pipeline)
6. [Cache Sizing Strategies](#cache-sizing-strategies)
7. [Memory Monitoring in Production](#memory-monitoring-in-production)
8. [Performance vs Memory Tradeoffs](#performance-vs-memory-tradeoffs)
9. [Summary](#summary)

---

## Case Study 1: E-Commerce Session Store

### The Problem

A high-traffic e-commerce site stored user sessions in memory for fast access. After 2 weeks of uptime, the application consumed 12 GB of memory and started experiencing OOM kills.

```go
// PROBLEMATIC CODE
package session

var sessions = make(map[string]*Session)
var mu sync.RWMutex

type Session struct {
    UserID      string
    Items       []CartItem
    CreatedAt   time.Time
    LastAccess  time.Time
}

func Create(userID string) string {
    mu.Lock()
    defer mu.Unlock()
    
    sessionID := generateID()
    sessions[sessionID] = &Session{
        UserID:     userID,
        Items:      make([]CartItem, 0),
        CreatedAt:  time.Now(),
        LastAccess: time.Now(),
    }
    
    return sessionID
}

func Get(sessionID string) (*Session, bool) {
    mu.RLock()
    defer mu.RUnlock()
    
    session, ok := sessions[sessionID]
    if ok {
        session.LastAccess = time.Now()
    }
    return session, ok
}

// Problem: No cleanup mechanism!
// Sessions accumulate indefinitely
```

### The Impact

```
Timeline:
Day 1:   10,000 sessions  → 50 MB
Day 7:   70,000 sessions  → 350 MB
Day 14:  250,000 sessions → 1.2 GB
Week 4:  500,000 sessions → 2.5 GB
Week 8:  1M sessions      → 5 GB
Week 12: OOM Kill         → Outage

Metrics observed:
- Memory grew 200 MB/day
- GC ran every 30 seconds (vs 5 minutes initially)
- Request latency p99: 500ms (vs 50ms initially)
- 3 production incidents in 2 months
```

### The Solution

```go
package session

import (
    "sync"
    "time"
    "github.com/hashicorp/golang-lru"
)

type SessionStore struct {
    cache      *lru.Cache
    maxAge     time.Duration
    cleanupMu  sync.Mutex
}

type Session struct {
    UserID      string
    Items       []CartItem
    CreatedAt   time.Time
    LastAccess  time.Time
}

func NewSessionStore(maxSessions int, maxAge time.Duration) *SessionStore {
    cache, _ := lru.NewWithEvict(maxSessions, func(key interface{}, value interface{}) {
        // Called when session is evicted
        logSessionEviction(key.(string))
    })
    
    store := &SessionStore{
        cache:  cache,
        maxAge: maxAge,
    }
    
    // Start cleanup goroutine
    go store.cleanupLoop()
    
    return store
}

func (s *SessionStore) Create(userID string) string {
    sessionID := generateID()
    session := &Session{
        UserID:     userID,
        Items:      make([]CartItem, 0),
        CreatedAt:  time.Now(),
        LastAccess: time.Now(),
    }
    
    s.cache.Add(sessionID, session)
    return sessionID
}

func (s *SessionStore) Get(sessionID string) (*Session, bool) {
    value, ok := s.cache.Get(sessionID)
    if !ok {
        return nil, false
    }
    
    session := value.(*Session)
    
    // Check if expired
    if time.Since(session.LastAccess) > s.maxAge {
        s.cache.Remove(sessionID)
        return nil, false
    }
    
    session.LastAccess = time.Now()
    return session, true
}

func (s *SessionStore) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        s.cleanup()
    }
}

func (s *SessionStore) cleanup() {
    s.cleanupMu.Lock()
    defer s.cleanupMu.Unlock()
    
    // Iterate and remove expired sessions
    keys := s.cache.Keys()
    for _, key := range keys {
        if value, ok := s.cache.Peek(key); ok {
            session := value.(*Session)
            if time.Since(session.LastAccess) > s.maxAge {
                s.cache.Remove(key)
            }
        }
    }
}

// Usage
func main() {
    // Max 100,000 sessions, 30-minute TTL
    store := NewSessionStore(100000, 30*time.Minute)
    
    // Sessions automatically evicted when:
    // 1. Cache exceeds 100,000 entries (LRU)
    // 2. Session not accessed for 30 minutes (TTL)
}
```

### Results

```
After fix:
- Memory stable at 500 MB (vs 5+ GB growing)
- GC frequency: every 5 minutes (vs 30 seconds)
- Request latency p99: 60ms (vs 500ms)
- Zero OOM incidents

Cost savings:
- Reduced instance size from 16 GB to 4 GB
- $1,200/month infrastructure savings
- Zero incident costs
```

---

## Case Study 2: Image Processing Service

### The Problem

An image processing service cached thumbnails in memory. After processing 100,000 images, memory usage reached 8 GB and performance degraded significantly.

```go
// PROBLEMATIC CODE
var thumbnailCache = make(map[string][]byte)
var cacheMu sync.Mutex

func GetThumbnail(imageID string) ([]byte, error) {
    cacheMu.Lock()
    defer cacheMu.Unlock()
    
    if thumb, ok := thumbnailCache[imageID]; ok {
        return thumb, nil  // Cache hit
    }
    
    // Generate thumbnail (expensive)
    original, err := loadImage(imageID)
    if err != nil {
        return nil, err
    }
    
    thumb := resize(original, 200, 200)
    
    // Cache forever
    thumbnailCache[imageID] = thumb
    
    return thumb, nil
}

// Problem: Unbounded cache
// 100,000 images × 80 KB/thumbnail = 8 GB
```

### The Solution

```go
package thumbnail

import (
    "github.com/allegro/bigcache"
    "time"
)

type ThumbnailService struct {
    cache *bigcache.BigCache
}

func NewThumbnailService() (*ThumbnailService, error) {
    config := bigcache.Config{
        Shards:             1024,
        LifeWindow:         24 * time.Hour,  // Expire after 24 hours
        CleanWindow:        5 * time.Minute,
        MaxEntriesInWindow: 1000 * 10 * 60,
        MaxEntrySize:       200,  // 200 KB max per entry
        HardMaxCacheSize:   512,  // 512 MB max
    }
    
    cache, err := bigcache.NewBigCache(config)
    if err != nil {
        return nil, err
    }
    
    return &ThumbnailService{cache: cache}, nil
}

func (s *ThumbnailService) GetThumbnail(imageID string) ([]byte, error) {
    // Try cache first
    thumb, err := s.cache.Get(imageID)
    if err == nil {
        return thumb, nil  // Cache hit
    }
    
    // Cache miss - generate
    original, err := loadImage(imageID)
    if err != nil {
        return nil, err
    }
    
    thumb = resize(original, 200, 200)
    
    // Cache with size limit
    s.cache.Set(imageID, thumb)
    
    return thumb, nil
}

// Alternative: Two-tier caching
type TwoTierCache struct {
    memory *lru.Cache  // 100 MB in-memory
    disk   *DiskCache  // 10 GB on disk
}

func (c *TwoTierCache) Get(key string) ([]byte, bool) {
    // L1: Memory cache (fast)
    if data, ok := c.memory.Get(key); ok {
        return data.([]byte), true
    }
    
    // L2: Disk cache (slower but larger)
    if data, ok := c.disk.Get(key); ok {
        // Promote to memory cache
        c.memory.Add(key, data)
        return data, true
    }
    
    return nil, false
}
```

### Results

```
Before fix:
- Memory: 8 GB (100K thumbnails)
- Cache hit rate: 65%
- p99 latency: 800ms (due to GC pauses)

After fix (size-bounded):
- Memory: 512 MB (capped)
- Cache hit rate: 60% (slightly lower but acceptable)
- p99 latency: 120ms (much more consistent)

After fix (two-tier):
- Memory: 100 MB (L1 in-memory)
- Disk usage: 4 GB (L2 on disk)
- Cache hit rate: 85% (L1) + 12% (L2) = 97% total
- p99 latency: 150ms (L1) / 350ms (L2)
```

---

## Case Study 3: Metrics Aggregation System

### The Problem

A monitoring system collected metrics from thousands of services. It stored all metrics in memory for real-time dashboards, leading to 20+ GB memory usage.

```go
// PROBLEMATIC CODE
var metrics = make(map[string][]MetricPoint)
var metricsMu sync.RWMutex

type MetricPoint struct {
    Timestamp time.Time
    Value     float64
    Tags      map[string]string
}

func RecordMetric(name string, value float64, tags map[string]string) {
    metricsMu.Lock()
    defer metricsMu.Unlock()
    
    point := MetricPoint{
        Timestamp: time.Now(),
        Value:     value,
        Tags:      tags,
    }
    
    metrics[name] = append(metrics[name], point)
    
    // Problem: Keeps ALL data points forever
    // 1000 metrics × 1 point/sec × 86400 sec/day = 86M points/day
}
```

### The Solution

```go
package metrics

import (
    "sync"
    "time"
)

type MetricsStore struct {
    data      map[string]*MetricSeries
    mu        sync.RWMutex
    retention time.Duration
}

type MetricSeries struct {
    points    []MetricPoint
    mu        sync.Mutex
    maxPoints int
}

func NewMetricsStore(retention time.Duration) *MetricsStore {
    store := &MetricsStore{
        data:      make(map[string]*MetricSeries),
        retention: retention,
    }
    
    go store.cleanupLoop()
    return store
}

func (s *MetricsStore) Record(name string, value float64, tags map[string]string) {
    s.mu.RLock()
    series, ok := s.data[name]
    s.mu.RUnlock()
    
    if !ok {
        s.mu.Lock()
        series = &MetricSeries{
            points:    make([]MetricPoint, 0, 1000),
            maxPoints: 10000,  // Max 10K points per metric
        }
        s.data[name] = series
        s.mu.Unlock()
    }
    
    series.mu.Lock()
    defer series.mu.Unlock()
    
    point := MetricPoint{
        Timestamp: time.Now(),
        Value:     value,
        Tags:      tags,
    }
    
    series.points = append(series.points, point)
    
    // Limit points per series
    if len(series.points) > series.maxPoints {
        // Keep only recent points
        series.points = series.points[len(series.points)-series.maxPoints:]
    }
}

func (s *MetricsStore) cleanupLoop() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        s.cleanup()
    }
}

func (s *MetricsStore) cleanup() {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    cutoff := time.Now().Add(-s.retention)
    
    for name, series := range s.data {
        series.mu.Lock()
        
        // Remove old points
        validIdx := 0
        for i, point := range series.points {
            if point.Timestamp.After(cutoff) {
                validIdx = i
                break
            }
        }
        
        if validIdx > 0 {
            series.points = series.points[validIdx:]
        }
        
        // Remove empty series
        if len(series.points) == 0 {
            delete(s.data, name)
        }
        
        series.mu.Unlock()
    }
}

// Alternative: Downsampling
func (s *MetricSeries) Downsample() {
    if len(s.points) < 1000 {
        return
    }
    
    // Keep every 10th point for old data
    now := time.Now()
    newPoints := make([]MetricPoint, 0, len(s.points)/2)
    
    for i, point := range s.points {
        age := now.Sub(point.Timestamp)
        
        if age < 1*time.Hour {
            // Keep all recent points (last hour)
            newPoints = append(newPoints, point)
        } else if age < 24*time.Hour && i%10 == 0 {
            // Keep every 10th point (last 24 hours)
            newPoints = append(newPoints, point)
        } else if age < 7*24*time.Hour && i%100 == 0 {
            // Keep every 100th point (last week)
            newPoints = append(newPoints, point)
        }
    }
    
    s.points = newPoints
}
```

### Results

```
Before fix:
- Memory: 20 GB (all historical data)
- Points stored: 50M
- Query latency: 2-5 seconds (scanning large datasets)

After fix (retention + limits):
- Memory: 800 MB (24-hour retention)
- Points stored: 2M (per series limits)
- Query latency: 50-200ms

After fix (with downsampling):
- Memory: 400 MB (aggressive downsampling)
- Points stored: 1M
- Query latency: 20-100ms
- Acceptable accuracy for dashboards
```

---

## Case Study 4: WebSocket Connection Manager

### The Problem

A real-time chat application stored connection metadata in memory. Connections from disconnected clients were never cleaned up.

```go
// PROBLEMATIC CODE
var connections = make(map[string]*Connection)
var connMu sync.RWMutex

type Connection struct {
    UserID      string
    Conn        *websocket.Conn
    LastPing    time.Time
    MessageBuf  []Message
}

func Register(userID string, conn *websocket.Conn) {
    connMu.Lock()
    defer connMu.Unlock()
    
    connections[userID] = &Connection{
        UserID:     userID,
        Conn:       conn,
        LastPing:   time.Now(),
        MessageBuf: make([]Message, 0, 100),
    }
    
    // Problem: Never removed when client disconnects
}
```

### The Solution

```go
package websocket

import (
    "context"
    "sync"
    "time"
)

type ConnectionManager struct {
    connections map[string]*Connection
    mu          sync.RWMutex
}

type Connection struct {
    UserID      string
    Conn        *websocket.Conn
    LastPing    time.Time
    MessageBuf  []Message
    ctx         context.Context
    cancel      context.CancelFunc
}

func NewConnectionManager() *ConnectionManager {
    cm := &ConnectionManager{
        connections: make(map[string]*Connection),
    }
    
    go cm.cleanupLoop()
    return cm
}

func (cm *ConnectionManager) Register(userID string, conn *websocket.Conn) {
    ctx, cancel := context.WithCancel(context.Background())
    
    connection := &Connection{
        UserID:     userID,
        Conn:       conn,
        LastPing:   time.Now(),
        MessageBuf: make([]Message, 0, 100),
        ctx:        ctx,
        cancel:     cancel,
    }
    
    cm.mu.Lock()
    // Clean up old connection if exists
    if old, ok := cm.connections[userID]; ok {
        old.Close()
    }
    cm.connections[userID] = connection
    cm.mu.Unlock()
    
    // Start ping handler
    go cm.handleConnection(connection)
}

func (cm *ConnectionManager) Unregister(userID string) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    if conn, ok := cm.connections[userID]; ok {
        conn.Close()
        delete(cm.connections, userID)
    }
}

func (cm *ConnectionManager) handleConnection(conn *Connection) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                cm.Unregister(conn.UserID)
                return
            }
            conn.LastPing = time.Now()
            
        case <-conn.ctx.Done():
            return
        }
    }
}

func (cm *ConnectionManager) cleanupLoop() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        cm.cleanup()
    }
}

func (cm *ConnectionManager) cleanup() {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    now := time.Now()
    for userID, conn := range cm.connections {
        // Remove stale connections (no ping for 2 minutes)
        if now.Sub(conn.LastPing) > 2*time.Minute {
            conn.Close()
            delete(cm.connections, userID)
        }
        
        // Limit message buffer
        if len(conn.MessageBuf) > 1000 {
            conn.MessageBuf = conn.MessageBuf[len(conn.MessageBuf)-100:]
        }
    }
}

func (c *Connection) Close() {
    c.cancel()
    c.Conn.Close()
    c.MessageBuf = nil  // Release buffer
}
```

### Results

```
Before fix:
- Memory: 4 GB (300K "zombie" connections)
- Active connections: 50K
- Zombie connections: 250K
- Cleanup: Manual restart required

After fix:
- Memory: 600 MB (only active connections)
- Active connections: 50K
- Zombie connections: 0
- Cleanup: Automatic
```

---

## Cache Sizing Strategies

### Method 1: Working Set Size

```go
// Estimate based on usage patterns

// Example: API response caching
// - 1000 unique endpoints
// - 100 requests/sec
// - 80% hit same 100 endpoints
// - Cache 200 endpoints → 90% hit rate

cache, _ := lru.New(200)
```

### Method 2: Memory Budget

```go
// Allocate percentage of available memory

totalMemory := 4 * 1024 * 1024 * 1024  // 4 GB container
cachePercent := 0.25                     // 25% for cache
cacheMemory := totalMemory * cachePercent // 1 GB

avgEntrySize := 5 * 1024  // 5 KB per entry
cacheSize := cacheMemory / avgEntrySize  // 200K entries

cache, _ := lru.New(cacheSize)
```

### Method 3: Performance Testing

```go
func benchmarkCacheSize(sizes []int, workload []Request) {
    for _, size := range sizes {
        cache, _ := lru.New(size)
        
        hitCount := 0
        for _, req := range workload {
            if _, ok := cache.Get(req.Key); ok {
                hitCount++
            } else {
                cache.Add(req.Key, req.Value)
            }
        }
        
        hitRate := float64(hitCount) / float64(len(workload))
        fmt.Printf("Size: %d, Hit Rate: %.2f%%\n", size, hitRate*100)
    }
}

// Run with production-like workload
// Choose size where hit rate plateaus
```

---

## Memory Monitoring in Production

### Key Metrics to Track

```go
package monitoring

import (
    "runtime"
    "time"
)

type MemoryMetrics struct {
    HeapAlloc      uint64
    HeapSys        uint64
    HeapObjects    uint64
    GCCount        uint32
    GCPauseTotal   time.Duration
    LastGC         time.Time
}

func CollectMetrics() MemoryMetrics {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    
    return MemoryMetrics{
        HeapAlloc:    m.Alloc,
        HeapSys:      m.HeapSys,
        HeapObjects:  m.HeapObjects,
        GCCount:      m.NumGC,
        GCPauseTotal: time.Duration(m.PauseTotalNs),
        LastGC:       time.Unix(0, int64(m.LastGC)),
    }
}

// Prometheus metrics
func exposeMetrics() {
    go func() {
        ticker := time.NewTicker(10 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            metrics := CollectMetrics()
            
            prometheus.heapAlloc.Set(float64(metrics.HeapAlloc))
            prometheus.heapObjects.Set(float64(metrics.HeapObjects))
            prometheus.gcCount.Set(float64(metrics.GCCount))
        }
    }()
}
```

### Alerting Thresholds

```yaml
# Example Prometheus alerts

# Memory growth alert
- alert: MemoryGrowth
  expr: increase(go_memstats_alloc_bytes[1h]) > 500MB
  annotations:
    summary: "Memory growing rapidly"
    
# High GC frequency
- alert: HighGCFrequency
  expr: rate(go_gc_duration_seconds_count[5m]) > 10
  annotations:
    summary: "GC running too frequently"
    
# Low GC reclaim rate
- alert: LowGCReclaimRate
  expr: |
    (go_memstats_alloc_bytes - go_memstats_alloc_bytes offset 1m)
    / go_memstats_alloc_bytes > 0.9
  annotations:
    summary: "GC not reclaiming memory effectively"
```

---

## Performance vs Memory Tradeoffs

### Tradeoff Matrix

```
Cache Size | Hit Rate | Memory | GC Overhead | Latency
-----------|----------|--------|-------------|--------
No cache   | 0%       | 10 MB  | Minimal     | High (DB)
Small      | 40%      | 50 MB  | Low         | Medium
Medium     | 80%      | 200 MB | Medium      | Low
Large      | 95%      | 1 GB   | High        | Very Low
Unbounded  | 98%      | 10+ GB | Very High   | Variable

Sweet spot: Medium (80% hit rate, manageable memory)
```

### When to Optimize for Memory

- Container memory limits
- Multi-tenant environments
- Cost-sensitive deployments
- Memory is bottleneck

### When to Optimize for Performance

- SLA requirements critical
- Memory is abundant
- Cache hit rate paramount
- Latency-sensitive application

---

## Summary

### Lessons Learned

1. **Always add bounds**: Every cache needs a size limit
2. **Monitor in production**: Memory leaks manifest slowly
3. **Test with real workloads**: Synthetic tests miss patterns
4. **Cleanup is critical**: TTL, eviction, and periodic cleanup
5. **Size appropriately**: Neither too small nor unbounded

### Best Practices Checklist

- [ ] All caches have size limits
- [ ] TTL for time-sensitive data
- [ ] Cleanup goroutines for expired entries
- [ ] Monitoring and alerting configured
- [ ] Performance tested with production-like load
- [ ] Memory budget allocated per service
- [ ] Eviction policy chosen based on workload
- [ ] Documented cache behavior and limits

---

## Next Steps

- **Implement fixes**: Apply patterns from case studies
- **Add monitoring**: Track memory metrics in production
- **Size caches**: Use strategies from this guide
- **Review code**: Check for unbounded collections
- **Test thoroughly**: Simulate production load

---

**Return to**: [Long-Lived References README](../README.md)
