# Production Case Studies

**Read Time**: ~25 minutes

**Prerequisites**: Understanding of resource leaks and monitoring

**Summary**: Real-world production incidents involving resource leaks, their detection, root cause analysis, and long-term prevention strategies.

---

## Introduction

This collection of production case studies demonstrates how resource leaks manifest in real systems, the detective work required to identify them, and the engineering solutions that prevent recurrence. Each case study includes timeline, symptoms, investigation process, and lessons learned.

---

## Case Study 1: The $50K HTTP Response Body Leak

**Company**: Financial services platform  
**Service**: Payment processing API  
**Impact**: $50,000 in lost transactions, 6-hour outage  
**Root Cause**: Unclosed HTTP response bodies in fraud detection service

### Background

A payment processing service made fraud detection calls to external APIs for every transaction. The service handled 10,000 transactions per minute during peak hours.

### Timeline

**Day 1 - 14:00**: Service deployed with new fraud detection integration  
**Day 1 - 16:00**: First "connection refused" errors appear in logs (dismissed as network issues)  
**Day 2 - 08:00**: Intermittent transaction failures during morning peak  
**Day 2 - 12:00**: Service becomes completely unresponsive  
**Day 2 - 12:15**: Emergency restart restores service temporarily  
**Day 2 - 14:30**: Service fails again, investigation begins  
**Day 2 - 18:45**: Root cause identified and fixed

### The Problematic Code

```go
// fraud_checker.go - The leaky implementation
func checkFraud(transactionID string, amount float64) (bool, error) {
    payload := FraudCheckRequest{
        TransactionID: transactionID,
        Amount:       amount,
        Timestamp:    time.Now(),
    }
    
    body, _ := json.Marshal(payload)
    
    // BUG #1: Using default HTTP client (no timeouts)
    resp, err := http.Post(fraudAPIURL, "application/json", bytes.NewBuffer(body))
    if err != nil {
        return false, err
    }
    
    // BUG #2: Early return without closing body
    if resp.StatusCode != 200 {
        log.Printf("Fraud API returned status %d", resp.StatusCode)
        return false, fmt.Errorf("fraud check failed")
        // ❌ resp.Body never closed!
    }
    
    // BUG #3: Even success path doesn't always close
    var result FraudCheckResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return false, err
        // ❌ resp.Body never closed on decode error!
    }
    
    resp.Body.Close() // Only reached if everything succeeds
    return result.IsFraud, nil
}
```

### Investigation Process

**Step 1: Initial Symptoms**
```bash
# Error logs showed:
2023-10-15 12:00:15 ERROR: dial tcp: lookup fraud-api.internal: no such host
2023-10-15 12:00:16 ERROR: dial tcp 10.0.1.50:443: connect: connection refused
2023-10-15 12:00:17 ERROR: dial tcp 10.0.1.50:443: socket: too many open files
```

**Step 2: System Resource Check**
```bash
# Check file descriptors
$ lsof -p $(pgrep payment-service) | wc -l
15847

# Check limit
$ ulimit -n
1024

# The service had opened 15x more files than the OS limit allowed!
```

**Step 3: Connection Analysis**
```bash
# Check network connections
$ netstat -an | grep 10.0.1.50 | head -20
tcp4  0  0  10.0.2.100.52341  10.0.1.50.443  CLOSE_WAIT
tcp4  0  0  10.0.2.100.52342  10.0.1.50.443  CLOSE_WAIT
tcp4  0  0  10.0.2.100.52343  10.0.1.50.443  CLOSE_WAIT
... (thousands more in CLOSE_WAIT state)

# CLOSE_WAIT indicates the remote server closed the connection
# but our application never called close() on the socket
```

**Step 4: Code Review**
The investigation revealed three critical bugs:
1. No timeouts on HTTP client
2. Response body not closed on error paths
3. Response body not closed on JSON decode errors

### The Fix

```go
// fraud_checker_fixed.go - The corrected implementation
var fraudClient = &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        10,
        MaxIdleConnsPerHost: 2,
        IdleConnTimeout:     30 * time.Second,
        TLSHandshakeTimeout: 5 * time.Second,
    },
}

func checkFraud(transactionID string, amount float64) (bool, error) {
    payload := FraudCheckRequest{
        TransactionID: transactionID,
        Amount:       amount,
        Timestamp:    time.Now(),
    }
    
    body, _ := json.Marshal(payload)
    
    // ✅ Use configured client with timeouts
    resp, err := fraudClient.Post(fraudAPIURL, "application/json", bytes.NewBuffer(body))
    if err != nil {
        return false, err
    }
    
    // ✅ Always close response body
    defer func() {
        // Drain and close body to enable connection reuse
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()
    
    if resp.StatusCode != 200 {
        return false, fmt.Errorf("fraud check failed: status %d", resp.StatusCode)
        // Body closed by defer
    }
    
    var result FraudCheckResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return false, err
        // Body closed by defer
    }
    
    return result.IsFraud, nil
}
```

### Prevention Measures

1. **Code Review Checklist**: Added HTTP response body closure to mandatory review items
2. **Static Analysis**: Integrated `bodyclose` linter into CI pipeline
3. **Load Testing**: Added sustained load tests to catch resource leaks before production
4. **Monitoring**: Added file descriptor and connection count metrics with alerts

### Lessons Learned

- **HTTP response bodies MUST be closed** even if you don't read them
- **Default HTTP client has no timeouts** - always configure your own
- **CLOSE_WAIT connections indicate application-side leaks**
- **Resource limits in production are often much lower than development**

---

## Case Study 2: The Goroutine Explosion

**Company**: Video streaming platform  
**Service**: Real-time chat service  
**Impact**: 2-hour service degradation, 50% user disconnect rate  
**Root Cause**: Unbounded goroutine creation during traffic spike

### Background

A chat service for live video streams created goroutines to handle each incoming message. During a popular event, message volume increased 100x normal levels.

### Timeline

**19:00**: Popular streamer begins event, chat traffic increases  
**19:15**: Response times increase from 50ms to 200ms  
**19:30**: CPU usage spikes to 100% across all instances  
**19:45**: Service becomes unresponsive, users start disconnecting  
**20:00**: Auto-scaling triggers, but new instances also become unresponsive  
**20:30**: Investigation reveals goroutine count > 500,000  
**21:00**: Emergency fix deployed with goroutine limiting

### The Problematic Code

```go
// chat_handler.go - The explosive implementation
func (s *ChatService) HandleMessage(ctx context.Context, msg *ChatMessage) error {
    // BUG: Creates unbounded goroutines during traffic spikes
    go func() {
        // Process message (takes 100-500ms)
        if err := s.processMessage(msg); err != nil {
            log.Printf("Failed to process message: %v", err)
        }
        
        // Broadcast to subscribers (takes 50-200ms)
        s.broadcastMessage(msg)
        
        // Store in database (takes 10-100ms)
        s.storeMessage(msg)
        
        // Update analytics (takes 20-50ms)
        s.updateAnalytics(msg)
    }()
    
    return nil // Returns immediately while work happens in background
}

// During the event:
// - 50,000 messages/second
// - Each message creates 1 goroutine
// - Each goroutine lives 200-850ms
// - Peak concurrent goroutines: 50,000 * 0.85 = 42,500
// - But traffic kept increasing...
```

### Investigation Process

**Step 1: CPU and Memory Analysis**
```bash
# CPU was pegged at 100% but memory usage was normal
$ top -p $(pgrep chat-service)
PID    USER   %CPU  %MEM    VSZ    RSS  TTY  STAT START   TIME COMMAND
12345  app    100.0  15.2  2.1GB  800MB ?   Sl   19:00  45:32 chat-service

# This suggested CPU-bound issue, not memory leak
```

**Step 2: Goroutine Analysis**
```bash
# Check goroutine count via pprof
$ curl http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1
goroutine profile: total 547892

# Nearly 550,000 goroutines!
```

**Step 3: Goroutine Stack Analysis**
```bash
$ curl http://localhost:6060/debug/pprof/goroutine?debug=1 | grep -A 5 "HandleMessage"

# Showed thousands of goroutines stuck in:
# - Database connection waits
# - Network I/O for broadcasts
# - JSON marshaling/unmarshaling
```

**Step 4: Go Scheduler Analysis**
```bash
$ go tool pprof http://localhost:6060/debug/pprof/profile
(pprof) top
Showing nodes accounting for 45.2s, 89.1% of 50.7s total
      flat  flat%   sum%        cum   cum%
    12.3s 24.26% 24.26%     12.3s 24.26%  runtime.schedule
     8.9s 17.55% 41.81%      8.9s 17.55%  runtime.findrunnable
     7.2s 14.20% 56.01%      7.2s 14.20%  runtime.runqgrab
```

The Go scheduler was spending 60% of CPU time just managing goroutines!

### The Fix

```go
// chat_handler_fixed.go - Worker pool implementation
type ChatService struct {
    workerPool chan *ChatMessage
    workers    int
}

func NewChatService(workerCount int) *ChatService {
    s := &ChatService{
        workerPool: make(chan *ChatMessage, workerCount*2), // Buffered queue
        workers:    workerCount,
    }
    
    // Start fixed number of worker goroutines
    for i := 0; i < workerCount; i++ {
        go s.worker(i)
    }
    
    return s
}

func (s *ChatService) worker(id int) {
    for msg := range s.workerPool {
        start := time.Now()
        
        // Process message
        if err := s.processMessage(msg); err != nil {
            log.Printf("Worker %d failed to process message: %v", id, err)
            continue
        }
        
        // Broadcast to subscribers
        s.broadcastMessage(msg)
        
        // Store in database
        s.storeMessage(msg)
        
        // Update analytics
        s.updateAnalytics(msg)
        
        // Metrics
        processingTime.Observe(time.Since(start).Seconds())
        messagesProcessed.Inc()
    }
}

func (s *ChatService) HandleMessage(ctx context.Context, msg *ChatMessage) error {
    select {
    case s.workerPool <- msg:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    default:
        // Queue full - apply backpressure
        queueFullErrors.Inc()
        return errors.New("message queue full, try again later")
    }
}
```

### Performance Impact

| Metric | Before Fix | After Fix | Improvement |
|--------|------------|-----------|-------------|
| **Max Goroutines** | 547,892 | 100 | 99.98% reduction |
| **CPU Usage** | 100% | 25% | 75% reduction |
| **Response Time P99** | 5.2s | 180ms | 96% improvement |
| **Memory Usage** | 2.1GB | 400MB | 81% reduction |
| **Messages/sec** | 15,000 (degraded) | 80,000 | 5.3x increase |

### Prevention Measures

1. **Load Testing**: Added tests simulating 10x normal traffic
2. **Goroutine Monitoring**: Added alerts for goroutine count > 10,000
3. **Worker Pool Pattern**: Standardized across all async processing
4. **Backpressure Handling**: Added circuit breakers and queue limits

---

## Case Study 3: The Database Connection Pool Starvation

**Company**: E-commerce platform  
**Service**: Inventory management system  
**Impact**: 4-hour checkout outage during Black Friday  
**Root Cause**: Database rows not closed, exhausting connection pool

### Background

An inventory service checked product availability for every checkout attempt. During Black Friday traffic, the service experienced complete database connection pool exhaustion.

### Timeline

**Black Friday 00:00**: Traffic begins increasing  
**02:00**: First database timeout errors appear  
**04:00**: Checkout success rate drops to 60%  
**06:00**: Complete checkout outage begins  
**06:15**: Database connection pool monitoring shows 0 available connections  
**08:30**: Root cause identified in inventory check code  
**10:00**: Emergency fix deployed, service restored

### The Problematic Code

```go
// inventory_service.go - The connection-leaking implementation
func (s *InventoryService) CheckAvailability(productID int, quantity int) (bool, error) {
    // BUG: Query without proper cleanup
    rows, err := s.db.Query(`
        SELECT warehouse_id, available_quantity 
        FROM inventory 
        WHERE product_id = ? AND available_quantity > 0
    `, productID)
    
    if err != nil {
        log.Printf("Query failed: %v", err)
        return false, err
        // ❌ Connection never returned to pool!
    }
    
    totalAvailable := 0
    for rows.Next() {
        var warehouseID int
        var available int
        
        if err := rows.Scan(&warehouseID, &available); err != nil {
            log.Printf("Scan failed: %v", err)
            return false, err
            // ❌ rows.Close() never called, connection still borrowed!
        }
        
        totalAvailable += available
        
        // Early exit optimization
        if totalAvailable >= quantity {
            return true, nil
            // ❌ rows.Close() never called, connection leaked!
        }
    }
    
    // Only reached if we scan all rows
    rows.Close()
    return totalAvailable >= quantity, nil
}

// Called 50,000 times/minute during Black Friday
// Connection pool size: 25 connections
// Average query time: 50ms
// Leak rate: ~30% of queries (early exits + errors)
// Time to pool exhaustion: 25 / (50,000/60 * 0.30) = 6 seconds!
```

### Investigation Process

**Step 1: Database Connection Analysis**
```go
// Added monitoring to track connection pool stats
func (s *InventoryService) logDBStats() {
    stats := s.db.Stats()
    log.Printf("DB Stats - Open: %d, InUse: %d, Idle: %d, WaitCount: %d, WaitDuration: %v",
        stats.OpenConnections,
        stats.InUse,
        stats.Idle,
        stats.WaitCount,
        stats.WaitDuration)
}

// Output during incident:
// DB Stats - Open: 25, InUse: 25, Idle: 0, WaitCount: 15847, WaitDuration: 2m30s
// All connections in use, massive wait queue!
```

**Step 2: Query Analysis**
```sql
-- Database side analysis
SELECT 
    state,
    COUNT(*) as connection_count,
    AVG(EXTRACT(EPOCH FROM (now() - state_change))) as avg_duration_seconds
FROM pg_stat_activity 
WHERE datname = 'inventory_db'
GROUP BY state;

/*
     state      | connection_count | avg_duration_seconds
----------------+------------------+---------------------
 active         |               25 |                 180
 idle in transaction |            0 |                   0
*/

-- All connections active for 3+ minutes (way too long for simple queries)
```

**Step 3: Application Tracing**
```go
// Added tracing to identify leak sources
func (s *InventoryService) CheckAvailabilityWithTracing(productID int, quantity int) (bool, error) {
    span := trace.StartSpan("inventory.check")
    defer span.End()
    
    connStart := time.Now()
    rows, err := s.db.Query(/* query */)
    span.SetAttribute("db.connection_wait_ms", time.Since(connStart).Milliseconds())
    
    if err != nil {
        span.SetAttribute("error", true)
        span.SetAttribute("error.type", "query_failed")
        return false, err
    }
    
    // This revealed that successful queries were taking 180+ seconds
    // instead of expected 50ms, indicating connections weren't being returned
}
```

### The Fix

```go
// inventory_service_fixed.go - Proper resource management
func (s *InventoryService) CheckAvailability(productID int, quantity int) (bool, error) {
    // ✅ Use QueryContext with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    rows, err := s.db.QueryContext(ctx, `
        SELECT warehouse_id, available_quantity 
        FROM inventory 
        WHERE product_id = ? AND available_quantity > 0
    `, productID)
    
    if err != nil {
        return false, err
    }
    
    // ✅ Always close rows to return connection to pool
    defer rows.Close()
    
    totalAvailable := 0
    for rows.Next() {
        var warehouseID int
        var available int
        
        if err := rows.Scan(&warehouseID, &available); err != nil {
            return false, err
            // Connection returned by defer
        }
        
        totalAvailable += available
        
        // Early exit is now safe
        if totalAvailable >= quantity {
            return true, nil
            // Connection returned by defer
        }
    }
    
    // Check for iteration errors
    if err := rows.Err(); err != nil {
        return false, err
    }
    
    return totalAvailable >= quantity, nil
}

// Alternative: Use QueryRow for single-value queries
func (s *InventoryService) GetTotalAvailability(productID int) (int, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    var total int
    err := s.db.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(available_quantity), 0)
        FROM inventory 
        WHERE product_id = ?
    `, productID).Scan(&total)
    
    // QueryRow automatically handles connection return
    return total, err
}
```

### Database Configuration Improvements

```go
// Optimized connection pool configuration
func setupDatabase() (*sql.DB, error) {
    db, err := sql.Open("postgres", connectionString)
    if err != nil {
        return nil, err
    }
    
    // ✅ Increased pool size for high-traffic periods
    db.SetMaxOpenConns(100)        // Up from 25
    db.SetMaxIdleConns(20)         // Keep connections ready
    db.SetConnMaxLifetime(1 * time.Hour)  // Cycle connections
    db.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections
    
    return db, nil
}
```

### Monitoring and Alerting

```go
// Added comprehensive database monitoring
func startDBMonitoring(db *sql.DB) {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
        for range ticker.C {
            stats := db.Stats()
            
            // Export metrics
            dbConnectionsOpen.Set(float64(stats.OpenConnections))
            dbConnectionsInUse.Set(float64(stats.InUse))
            dbConnectionsIdle.Set(float64(stats.Idle))
            dbConnectionWaitCount.Set(float64(stats.WaitCount))
            dbConnectionWaitDuration.Set(stats.WaitDuration.Seconds())
            
            // Alert on pool exhaustion
            utilizationPercent := float64(stats.InUse) / float64(stats.OpenConnections) * 100
            if utilizationPercent > 90 {
                log.Printf("WARNING: DB connection pool %0.1f%% utilized", utilizationPercent)
            }
            
            // Alert on wait queue buildup
            if stats.WaitCount > 100 {
                log.Printf("CRITICAL: %d queries waiting for DB connections", stats.WaitCount)
            }
        }
    }()
}
```

### Prevention Measures

1. **Static Analysis**: Added `sqlclosecheck` linter to CI pipeline
2. **Load Testing**: Black Friday traffic simulation in staging environment
3. **Connection Pool Monitoring**: Real-time dashboards and alerts
4. **Query Timeouts**: All database operations have context timeouts
5. **Circuit Breakers**: Fail fast when database is overloaded

---

## Case Study 4: The File Descriptor Avalanche

**Company**: Log analytics platform  
**Service**: Log ingestion service  
**Impact**: 12-hour data loss, customer SLA violations  
**Root Cause**: Log files opened but never closed during rotation

### Background

A log ingestion service processed millions of log entries per hour, writing them to daily rotating files. A bug in the file rotation logic caused old files to never be closed.

### The Problematic Code

```go
// log_writer.go - The file-leaking implementation
type LogWriter struct {
    currentFile *os.File
    currentDate string
    mu          sync.Mutex
}

func (lw *LogWriter) WriteLog(entry LogEntry) error {
    lw.mu.Lock()
    defer lw.mu.Unlock()
    
    today := time.Now().Format("2006-01-02")
    
    // BUG: File rotation without closing old file
    if today != lw.currentDate {
        filename := fmt.Sprintf("logs-%s.json", today)
        
        newFile, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
        if err != nil {
            return err
        }
        
        // ❌ Old file never closed!
        lw.currentFile = newFile
        lw.currentDate = today
    }
    
    data, _ := json.Marshal(entry)
    _, err := lw.currentFile.Write(append(data, '\n'))
    return err
}

// Result: After 30 days, 30 files open simultaneously
// After 365 days, would hit file descriptor limit
```

### The Fix and Prevention Strategy

```go
// log_writer_fixed.go - Proper file lifecycle management
type LogWriter struct {
    currentFile *os.File
    currentDate string
    mu          sync.Mutex
}

func (lw *LogWriter) WriteLog(entry LogEntry) error {
    lw.mu.Lock()
    defer lw.mu.Unlock()
    
    today := time.Now().Format("2006-01-02")
    
    if today != lw.currentDate {
        // ✅ Close old file before opening new one
        if lw.currentFile != nil {
            if err := lw.currentFile.Close(); err != nil {
                log.Printf("Failed to close old log file: %v", err)
            }
        }
        
        filename := fmt.Sprintf("logs-%s.json", today)
        newFile, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
        if err != nil {
            return err
        }
        
        lw.currentFile = newFile
        lw.currentDate = today
        log.Printf("Rotated to new log file: %s", filename)
    }
    
    data, _ := json.Marshal(entry)
    _, err := lw.currentFile.Write(append(data, '\n'))
    return err
}

// ✅ Graceful shutdown
func (lw *LogWriter) Close() error {
    lw.mu.Lock()
    defer lw.mu.Unlock()
    
    if lw.currentFile != nil {
        return lw.currentFile.Close()
    }
    return nil
}
```

---

## Common Patterns and Prevention

### Detection Patterns

| Symptom | Likely Cause | Investigation Steps |
|---------|--------------|-------------------|
| **"Too many open files"** | File descriptor leak | `lsof -p <pid>`, check file close patterns |
| **"Connection refused"** | Network connection leak | `netstat -an`, check HTTP client usage |
| **High CPU, normal memory** | Goroutine explosion | `pprof goroutine`, check unbounded creation |
| **Database timeouts** | Connection pool exhaustion | Check `db.Stats()`, audit query cleanup |
| **Gradual memory growth** | Memory + resource leak | `pprof heap`, check for combined leaks |

### Prevention Checklist

- [ ] **Static Analysis**: `bodyclose`, `sqlclosecheck`, `errcheck` linters
- [ ] **Code Reviews**: Mandatory resource cleanup checks
- [ ] **Testing**: Load tests with resource monitoring
- [ ] **Monitoring**: Real-time resource usage dashboards
- [ ] **Alerting**: Progressive alerts (warning → critical)
- [ ] **Documentation**: Runbooks for common leak scenarios
- [ ] **Training**: Team education on resource management patterns

### Emergency Response Playbook

**Immediate Actions (0-15 minutes)**:
1. Check system resource usage (`lsof`, `netstat`)
2. Restart affected services if critical
3. Scale horizontally if possible
4. Enable debug endpoints (`pprof`)

**Investigation Phase (15-60 minutes)**:
1. Capture profiles before/after restart
2. Analyze recent deployments
3. Check monitoring dashboards for trends
4. Review error logs for patterns

**Resolution Phase (1-4 hours)**:
1. Identify root cause in code
2. Implement temporary fix
3. Deploy with monitoring
4. Verify fix effectiveness

**Post-Incident (1-2 weeks)**:
1. Conduct blameless post-mortem
2. Add prevention measures
3. Update monitoring/alerting
4. Share lessons learned

## Key Takeaways

1. **Resource leaks cause sudden, catastrophic failures** unlike gradual memory leaks

2. **Production environments have much lower limits** than development machines

3. **Always close resources with defer** immediately after acquisition

4. **Monitor resource usage proactively** with alerts and dashboards

5. **Load test with realistic traffic patterns** to expose leaks before production

6. **Use static analysis tools** to catch common patterns during development

7. **Have emergency procedures ready** - resource exhaustion requires immediate action

8. **Learn from incidents** - each leak teaches patterns to prevent in the future

---

## References

- https://github.com/uber-go/goleak - Goroutine leak detection
- https://github.com/kisielk/errcheck - Error checking linter
- https://github.com/timakin/bodyclose - HTTP response body linter
- https://pkg.go.dev/database/sql - Database connection management

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - Prevention fundamentals
- [Monitoring Strategies](06-monitoring-strategies.md) - Proactive detection
- [HTTP Connection Pooling](03-http-connection-pooling.md) - HTTP-specific patterns
