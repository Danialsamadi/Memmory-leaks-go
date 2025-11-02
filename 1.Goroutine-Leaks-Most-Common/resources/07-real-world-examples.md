# Real-World Examples: Production Goroutine Leaks

**Read Time**: 30 minutes

**Related Topics**:
- [Conceptual Explanation](./01-conceptual-explanation.md)
- [Context Pattern](./04-context-pattern.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [HTTP Handler Goroutine Leak](#http-handler-goroutine-leak)
2. [WebSocket Connection Leak](#websocket-connection-leak)
3. [Message Queue Consumer](#message-queue-consumer)
4. [Background Job Processor](#background-job-processor)
5. [gRPC Streaming Leak](#grpc-streaming-leak)
6. [Database Connection Pool](#database-connection-pool)
7. [Lessons Learned](#lessons-learned)

---

## HTTP Handler Goroutine Leak

### The Problem

A high-traffic API service spawned a goroutine per request to log analytics asynchronously. Under normal load, this worked fine, but when the analytics service went down, goroutines accumulated rapidly.

### The Code (Leaky)

```go
func analyticsHandler(w http.ResponseWriter, r *http.Request) {
    // Spawn goroutine for async analytics
    go func() {
        // This call could hang indefinitely if service is down
        err := sendAnalytics(r.URL.Path, r.Method, r.UserAgent())
        if err != nil {
            log.Printf("Analytics error: %v", err)
        }
    }()
    
    // Return immediately
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func sendAnalytics(path, method, userAgent string) error {
    // HTTP call with no timeout
    resp, err := http.Post("https://analytics.internal/events",
        "application/json",
        createPayload(path, method, userAgent))
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    return nil
}
```

### What Happened

**Timeline**:
- **Day 1, 10:00 AM**: Analytics service deploys, has a bug causing slow responses
- **Day 1, 10:05 AM**: API response times increase slightly
- **Day 1, 10:15 AM**: Goroutine count reaches 50,000
- **Day 1, 10:20 AM**: API starts returning 500 errors, memory pressure
- **Day 1, 10:25 AM**: Application crashes with OOM

**Metrics**:
- Normal load: 1000 req/s
- Goroutine accumulation: 1000/second
- Time to failure: 25 minutes
- Total leaked goroutines: 1.5 million

### The Fix

```go
func analyticsHandlerFixed(w http.ResponseWriter, r *http.Request) {
    // Use request context with timeout
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()
    
    go func(ctx context.Context) {
        // Goroutine respects context cancellation
        if err := sendAnalyticsWithContext(ctx, r.URL.Path, r.Method, r.UserAgent()); err != nil {
            log.Printf("Analytics error: %v", err)
        }
    }(ctx)
    
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func sendAnalyticsWithContext(ctx context.Context, path, method, userAgent string) error {
    req, err := http.NewRequestWithContext(ctx, "POST",
        "https://analytics.internal/events",
        createPayload(path, method, userAgent))
    if err != nil {
        return err
    }
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    return nil
}
```

### Even Better: Worker Pool

```go
var analyticsQueue chan AnalyticsEvent

func init() {
    analyticsQueue = make(chan AnalyticsEvent, 1000)
    
    // Fixed number of workers
    for i := 0; i < 10; i++ {
        go analyticsWorker(i)
    }
}

func analyticsWorker(id int) {
    for event := range analyticsQueue {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        _ = sendAnalyticsWithContext(ctx, event.Path, event.Method, event.UserAgent)
        cancel()
    }
}

func analyticsHandlerBest(w http.ResponseWriter, r *http.Request) {
    event := AnalyticsEvent{
        Path:      r.URL.Path,
        Method:    r.Method,
        UserAgent: r.UserAgent(),
    }
    
    // Non-blocking send
    select {
    case analyticsQueue <- event:
        // Queued successfully
    default:
        // Queue full, drop event
        log.Println("Analytics queue full, dropping event")
    }
    
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Benefits**:
- Fixed number of goroutines (10)
- Bounded queue prevents memory growth
- Graceful degradation when analytics service is slow

---

## WebSocket Connection Leak

### The Problem

A real-time notification service managed WebSocket connections. When clients disconnected abruptly (network issues, browser close), the read and write goroutines leaked.

### The Code (Leaky)

```go
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    // Spawn reader goroutine
    go func() {
        for {
            _, message, err := conn.ReadMessage()
            if err != nil {
                log.Println("Read error:", err)
                return  // Goroutine exits
            }
            processMessage(message)
        }
    }()
    
    // Spawn writer goroutine
    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        
        for {
            <-ticker.C
            if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return  // Goroutine exits
            }
        }
    }()
    
    // Handler returns, but goroutines continue
}
```

**Problem**: If connection dies without clean close, both goroutines may hang.

### The Fix

```go
type WebSocketConnection struct {
    conn   *websocket.Conn
    send   chan []byte
    ctx    context.Context
    cancel context.CancelFunc
}

func handleWebSocketFixed(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    ctx, cancel := context.WithCancel(r.Context())
    
    wsc := &WebSocketConnection{
        conn:   conn,
        send:   make(chan []byte, 256),
        ctx:    ctx,
        cancel: cancel,
    }
    
    // Spawn managed goroutines
    go wsc.readPump()
    go wsc.writePump()
    
    // Wait for context cancellation
    <-ctx.Done()
    
    // Clean up
    conn.Close()
}

func (wsc *WebSocketConnection) readPump() {
    defer wsc.cancel()  // Cancel context on exit
    
    wsc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
    wsc.conn.SetPongHandler(func(string) error {
        wsc.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
        return nil
    })
    
    for {
        select {
        case <-wsc.ctx.Done():
            return
        default:
            _, message, err := wsc.conn.ReadMessage()
            if err != nil {
                if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                    log.Printf("WebSocket error: %v", err)
                }
                return
            }
            processMessage(message)
        }
    }
}

func (wsc *WebSocketConnection) writePump() {
    defer wsc.cancel()  // Cancel context on exit
    
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-wsc.ctx.Done():
            return
            
        case message := <-wsc.send:
            if err := wsc.conn.WriteMessage(websocket.TextMessage, message); err != nil {
                return
            }
            
        case <-ticker.C:
            if err := wsc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}
```

**Key improvements**:
- Context coordinates both goroutines
- Either goroutine can trigger cleanup
- Connection always closed
- No goroutine leaks

---

## Message Queue Consumer

### The Problem

A Kafka consumer spawned a goroutine per message for parallel processing. During a deployment with a slow rollout, messages were rebalanced between instances, but goroutines kept processing duplicate messages.

### The Code (Leaky)

```go
func consumeMessages(consumer *kafka.Consumer) {
    for {
        msg, err := consumer.ReadMessage(-1)
        if err != nil {
            log.Printf("Consumer error: %v", err)
            continue
        }
        
        // Spawn goroutine per message
        go func(msg *kafka.Message) {
            processMessage(msg.Value)
            // No commit - message may be reprocessed
        }(msg)
    }
}

func processMessage(data []byte) {
    // Long-running processing (30 seconds)
    result := heavyProcessing(data)
    saveToDatabase(result)
}
```

**Problems**:
- No limit on concurrent processing
- No cancellation mechanism
- No coordination with consumer rebalancing
- Goroutines outlive partition assignment

### The Fix

```go
func consumeMessagesFixed(ctx context.Context, consumer *kafka.Consumer) error {
    // Create worker pool
    const numWorkers = 50
    workQueue := make(chan *kafka.Message, numWorkers*2)
    
    var wg sync.WaitGroup
    
    // Start workers
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            messageWorker(ctx, id, workQueue, consumer)
        }(i)
    }
    
    // Consume messages
    go func() {
        for {
            select {
            case <-ctx.Done():
                close(workQueue)
                return
            default:
                msg, err := consumer.ReadMessage(100 * time.Millisecond)
                if err != nil {
                    if err.(kafka.Error).Code() == kafka.ErrTimedOut {
                        continue
                    }
                    log.Printf("Consumer error: %v", err)
                    continue
                }
                
                select {
                case workQueue <- msg:
                case <-ctx.Done():
                    close(workQueue)
                    return
                }
            }
        }
    }()
    
    // Wait for workers to finish
    wg.Wait()
    return nil
}

func messageWorker(ctx context.Context, id int, workQueue <-chan *kafka.Message, consumer *kafka.Consumer) {
    for {
        select {
        case <-ctx.Done():
            return
            
        case msg, ok := <-workQueue:
            if !ok {
                return  // Channel closed
            }
            
            // Process with timeout
            processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
            err := processMessageWithContext(processCtx, msg.Value)
            cancel()
            
            if err != nil {
                log.Printf("Worker %d failed to process message: %v", id, err)
            } else {
                // Commit offset
                consumer.CommitMessages(msg)
            }
        }
    }
}

func processMessageWithContext(ctx context.Context, data []byte) error {
    resultCh := make(chan error, 1)
    
    go func() {
        result := heavyProcessing(data)
        err := saveToDatabase(result)
        resultCh <- err
    }()
    
    select {
    case err := <-resultCh:
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

**Improvements**:
- Fixed worker pool (50 workers)
- Context-aware processing
- Proper shutdown sequence
- No goroutine leaks on rebalance

---

## Background Job Processor

### The Problem

A cron-like system scheduled background jobs. Jobs were spawned without tracking, and slow jobs accumulated goroutines.

### The Code (Leaky)

```go
type Job struct {
    Schedule string
    Task     func()
}

func startScheduler(jobs []Job) {
    for _, job := range jobs {
        go func(j Job) {
            ticker := time.NewTicker(parseSchedule(j.Schedule))
            defer ticker.Stop()
            
            for range ticker.C {
                go j.Task()  // Spawn unbounded goroutines
            }
        }(job)
    }
}
```

**Problems**:
- Unbounded goroutine creation
- No cancellation
- Slow jobs accumulate
- No visibility into running jobs

### The Fix

```go
type JobScheduler struct {
    jobs   []Job
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
    sem    chan struct{}  // Semaphore for limiting concurrent jobs
}

func NewJobScheduler(ctx context.Context, maxConcurrent int) *JobScheduler {
    ctx, cancel := context.WithCancel(ctx)
    return &JobScheduler{
        ctx:    ctx,
        cancel: cancel,
        sem:    make(chan struct{}, maxConcurrent),
    }
}

func (js *JobScheduler) AddJob(schedule string, task func(context.Context) error) {
    job := Job{
        Schedule: schedule,
        Task:     task,
    }
    js.jobs = append(js.jobs, job)
}

func (js *JobScheduler) Start() {
    for _, job := range js.jobs {
        js.wg.Add(1)
        go js.runJob(job)
    }
}

func (js *JobScheduler) runJob(job Job) {
    defer js.wg.Done()
    
    ticker := time.NewTicker(parseSchedule(job.Schedule))
    defer ticker.Stop()
    
    for {
        select {
        case <-js.ctx.Done():
            return
            
        case <-ticker.C:
            // Acquire semaphore (blocks if limit reached)
            select {
            case js.sem <- struct{}{}:
                js.wg.Add(1)
                go js.executeTask(job)
            case <-js.ctx.Done():
                return
            default:
                log.Printf("Job %s skipped: too many concurrent jobs", job.Schedule)
            }
        }
    }
}

func (js *JobScheduler) executeTask(job Job) {
    defer js.wg.Done()
    defer func() { <-js.sem }()  // Release semaphore
    
    ctx, cancel := context.WithTimeout(js.ctx, 5*time.Minute)
    defer cancel()
    
    if err := job.Task(ctx); err != nil {
        log.Printf("Job failed: %v", err)
    }
}

func (js *JobScheduler) Stop() {
    js.cancel()
    js.wg.Wait()
}
```

**Improvements**:
- Semaphore limits concurrent jobs
- Graceful shutdown with context
- Job timeouts
- Proper cleanup with WaitGroup

---

## gRPC Streaming Leak

### The Problem

A gRPC streaming service leaked goroutines when clients disconnected without properly closing streams.

### The Code (Leaky)

```go
func (s *Server) StreamData(req *pb.StreamRequest, stream pb.Service_StreamDataServer) error {
    eventCh := make(chan *pb.Event)
    
    // Spawn event generator
    go func() {
        for {
            event := generateEvent()
            eventCh <- event  // Blocks if stream goroutine exits
        }
    }()
    
    // Send events to client
    for event := range eventCh {
        if err := stream.Send(event); err != nil {
            return err  // Stream goroutine exits, but generator still running
        }
    }
    
    return nil
}
```

### The Fix

```go
func (s *Server) StreamDataFixed(req *pb.StreamRequest, stream pb.Service_StreamDataServer) error {
    ctx := stream.Context()
    eventCh := make(chan *pb.Event, 10)
    
    // Spawn event generator with cancellation
    go func() {
        defer close(eventCh)
        
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ctx.Done():
                return  // Client disconnected
                
            case <-ticker.C:
                event := generateEvent()
                
                select {
                case eventCh <- event:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    
    // Send events to client
    for event := range eventCh {
        if err := stream.Send(event); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## Database Connection Pool

### The Problem

Custom connection pool spawned goroutines to maintain idle connections, but never cleaned them up when connections were closed.

### The Fix

```go
type ConnectionPool struct {
    ctx     context.Context
    cancel  context.CancelFunc
    conns   chan *sql.Conn
    wg      sync.WaitGroup
}

func NewConnectionPool(ctx context.Context, db *sql.DB, size int) *ConnectionPool {
    ctx, cancel := context.WithCancel(ctx)
    
    pool := &ConnectionPool{
        ctx:    ctx,
        cancel: cancel,
        conns:  make(chan *sql.Conn, size),
    }
    
    // Initialize connections
    for i := 0; i < size; i++ {
        pool.wg.Add(1)
        go pool.maintainConnection(db)
    }
    
    return pool
}

func (p *ConnectionPool) maintainConnection(db *sql.DB) {
    defer p.wg.Done()
    
    conn, err := db.Conn(p.ctx)
    if err != nil {
        log.Printf("Failed to create connection: %v", err)
        return
    }
    defer conn.Close()
    
    for {
        select {
        case <-p.ctx.Done():
            return
            
        case p.conns <- conn:
            // Connection taken
            <-p.ctx.Done()
            return
        }
    }
}

func (p *ConnectionPool) Close() {
    p.cancel()
    p.wg.Wait()
    close(p.conns)
}
```

---

## Lessons Learned

### Key Takeaways

1. **Always use context**: Every goroutine should receive and respect a context
2. **Bounded resources**: Use worker pools, not unbounded goroutine creation
3. **Timeouts everywhere**: Network calls, database queries, external services
4. **Proper cleanup**: Use defer, WaitGroups, and context cancellation
5. **Monitor in production**: Track goroutine count, set up alerts
6. **Test for leaks**: Use goleak, check goroutine count in tests
7. **Design for failure**: External services will fail, clients will disconnect

### Common Patterns That Leak

```go
// Pattern 1: Fire and forget
go doSomething()  // No coordination

// Pattern 2: No timeout
http.Get(url)  // Can hang forever

// Pattern 3: Unbounded spawning
for item := range items {
    go process(item)  // Creates unlimited goroutines
}

// Pattern 4: Missing context
for {
    doWork()  // No exit condition
}
```

### Patterns That Don't Leak

```go
// Pattern 1: Context-aware
go func(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            doWork()
        }
    }
}(ctx)

// Pattern 2: Timeout
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
http.Get(url)

// Pattern 3: Worker pool
sem := make(chan struct{}, maxWorkers)
for item := range items {
    sem <- struct{}{}
    go func() {
        defer func() { <-sem }()
        process(item)
    }()
}

// Pattern 4: Proper coordination
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    doWork()
}()
wg.Wait()
```

---

## Further Reading

- [Context Pattern](./04-context-pattern.md)
- [Detection Methods](./05-detection-methods.md)
- [Unbounded Resources](../../5.Unbounded-Resources/)

---

**Return to**: [Goroutine Leaks README](../README.md)

