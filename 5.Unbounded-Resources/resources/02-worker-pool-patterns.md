# Worker Pool Patterns in Go

**Read Time**: 20 minutes

**Prerequisites**: Understanding of goroutines, channels, and sync package

---

## Table of Contents

1. [Why Worker Pools](#why-worker-pools)
2. [Basic Worker Pool](#basic-worker-pool)
3. [Advanced Patterns](#advanced-patterns)
4. [Graceful Shutdown](#graceful-shutdown)
5. [Production Considerations](#production-considerations)

---

## Why Worker Pools

### The Problem with Goroutine-per-Request

```go
// BAD: Creates unlimited goroutines
func handleRequests(requests <-chan Request) {
    for req := range requests {
        go process(req)
    }
}
```

**Issues**:
- No control over concurrency
- Memory grows without bound
- No backpressure mechanism
- Difficult to shut down gracefully

### The Worker Pool Solution

```go
// GOOD: Fixed number of workers
func handleRequests(requests <-chan Request, workers int) {
    for i := 0; i < workers; i++ {
        go func() {
            for req := range requests {
                process(req)
            }
        }()
    }
}
```

**Benefits**:
- Predictable resource usage
- Built-in backpressure (channel blocks when full)
- Easy to scale up/down
- Clean shutdown via channel close

---

## Basic Worker Pool

### Implementation

```go
package workerpool

import (
    "context"
    "sync"
)

type Task func()

type Pool struct {
    tasks    chan Task
    wg       sync.WaitGroup
    shutdown chan struct{}
}

func New(workers, queueSize int) *Pool {
    p := &Pool{
        tasks:    make(chan Task, queueSize),
        shutdown: make(chan struct{}),
    }
    
    for i := 0; i < workers; i++ {
        p.wg.Add(1)
        go p.worker(i)
    }
    
    return p
}

func (p *Pool) worker(id int) {
    defer p.wg.Done()
    
    for {
        select {
        case task, ok := <-p.tasks:
            if !ok {
                return // Channel closed
            }
            task()
        case <-p.shutdown:
            return
        }
    }
}

// Submit adds a task to the pool
// Returns false if pool is full (non-blocking)
func (p *Pool) Submit(task Task) bool {
    select {
    case p.tasks <- task:
        return true
    default:
        return false
    }
}

// SubmitWait adds a task, blocking until space available
func (p *Pool) SubmitWait(ctx context.Context, task Task) error {
    select {
    case p.tasks <- task:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Close shuts down the pool gracefully
func (p *Pool) Close() {
    close(p.tasks)
    p.wg.Wait()
}

// Shutdown stops workers immediately
func (p *Pool) Shutdown() {
    close(p.shutdown)
    p.wg.Wait()
}
```

### Usage

```go
func main() {
    // Create pool with 10 workers, queue of 100
    pool := workerpool.New(10, 100)
    defer pool.Close()
    
    // Submit tasks
    for i := 0; i < 1000; i++ {
        task := func() {
            // Do work
            time.Sleep(100 * time.Millisecond)
        }
        
        if !pool.Submit(task) {
            log.Println("Pool full, task rejected")
        }
    }
}
```

---

## Advanced Patterns

### Pattern 1: Worker Pool with Results

```go
type Result struct {
    Value interface{}
    Err   error
}

type TaskWithResult func() Result

type ResultPool struct {
    tasks   chan TaskWithResult
    results chan Result
    wg      sync.WaitGroup
}

func NewResultPool(workers, queueSize int) *ResultPool {
    p := &ResultPool{
        tasks:   make(chan TaskWithResult, queueSize),
        results: make(chan Result, queueSize),
    }
    
    for i := 0; i < workers; i++ {
        p.wg.Add(1)
        go p.worker()
    }
    
    return p
}

func (p *ResultPool) worker() {
    defer p.wg.Done()
    for task := range p.tasks {
        p.results <- task()
    }
}

func (p *ResultPool) Submit(task TaskWithResult) bool {
    select {
    case p.tasks <- task:
        return true
    default:
        return false
    }
}

func (p *ResultPool) Results() <-chan Result {
    return p.results
}

func (p *ResultPool) Close() {
    close(p.tasks)
    p.wg.Wait()
    close(p.results)
}
```

### Pattern 2: Priority Queue Pool

```go
type Priority int

const (
    Low Priority = iota
    Normal
    High
    Critical
)

type PriorityTask struct {
    Priority Priority
    Task     func()
}

type PriorityPool struct {
    queues   [4]chan func() // One per priority level
    wg       sync.WaitGroup
    shutdown chan struct{}
}

func NewPriorityPool(workers int) *PriorityPool {
    p := &PriorityPool{
        shutdown: make(chan struct{}),
    }
    
    // Initialize priority queues
    for i := range p.queues {
        p.queues[i] = make(chan func(), 100)
    }
    
    // Start workers
    for i := 0; i < workers; i++ {
        p.wg.Add(1)
        go p.worker()
    }
    
    return p
}

func (p *PriorityPool) worker() {
    defer p.wg.Done()
    
    for {
        // Check queues in priority order
        select {
        case task := <-p.queues[Critical]:
            task()
        case <-p.shutdown:
            return
        default:
            select {
            case task := <-p.queues[Critical]:
                task()
            case task := <-p.queues[High]:
                task()
            case <-p.shutdown:
                return
            default:
                select {
                case task := <-p.queues[Critical]:
                    task()
                case task := <-p.queues[High]:
                    task()
                case task := <-p.queues[Normal]:
                    task()
                case task := <-p.queues[Low]:
                    task()
                case <-p.shutdown:
                    return
                }
            }
        }
    }
}

func (p *PriorityPool) Submit(priority Priority, task func()) bool {
    select {
    case p.queues[priority] <- task:
        return true
    default:
        return false
    }
}
```

### Pattern 3: Dynamic Pool (with limits)

```go
type DynamicPool struct {
    minWorkers int
    maxWorkers int
    tasks      chan func()
    workers    int64
    mu         sync.Mutex
    wg         sync.WaitGroup
}

func NewDynamicPool(min, max, queueSize int) *DynamicPool {
    p := &DynamicPool{
        minWorkers: min,
        maxWorkers: max,
        tasks:      make(chan func(), queueSize),
    }
    
    // Start minimum workers
    for i := 0; i < min; i++ {
        p.addWorker()
    }
    
    return p
}

func (p *DynamicPool) addWorker() {
    p.mu.Lock()
    if int(p.workers) >= p.maxWorkers {
        p.mu.Unlock()
        return
    }
    atomic.AddInt64(&p.workers, 1)
    p.mu.Unlock()
    
    p.wg.Add(1)
    go p.worker()
}

func (p *DynamicPool) worker() {
    defer p.wg.Done()
    defer atomic.AddInt64(&p.workers, -1)
    
    idleTimeout := time.NewTimer(30 * time.Second)
    defer idleTimeout.Stop()
    
    for {
        select {
        case task, ok := <-p.tasks:
            if !ok {
                return
            }
            task()
            idleTimeout.Reset(30 * time.Second)
        case <-idleTimeout.C:
            // Idle timeout - exit if above minimum
            if int(atomic.LoadInt64(&p.workers)) > p.minWorkers {
                return
            }
            idleTimeout.Reset(30 * time.Second)
        }
    }
}

func (p *DynamicPool) Submit(task func()) bool {
    select {
    case p.tasks <- task:
        return true
    default:
        // Queue full, try to add worker
        p.addWorker()
        select {
        case p.tasks <- task:
            return true
        default:
            return false
        }
    }
}
```

---

## Graceful Shutdown

### Pattern: Drain Queue Before Exit

```go
func (p *Pool) GracefulShutdown(ctx context.Context) error {
    // Stop accepting new tasks
    close(p.tasks)
    
    // Wait for workers to finish with timeout
    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Usage with HTTP Server

```go
func main() {
    pool := workerpool.New(100, 1000)
    
    server := &http.Server{
        Addr: ":8080",
        Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !pool.Submit(func() {
                processRequest(r)
            }) {
                http.Error(w, "Service busy", http.StatusServiceUnavailable)
            }
        }),
    }
    
    // Graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan
        
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        server.Shutdown(ctx)
        pool.GracefulShutdown(ctx)
    }()
    
    server.ListenAndServe()
}
```

---

## Production Considerations

### 1. Queue Sizing

```go
// Rule of thumb: queue = 2-3x worker count
workers := 100
queueSize := workers * 2 // 200

// For bursty traffic: larger queue
queueSize := workers * 10 // 1000
```

### 2. Monitoring

```go
type MonitoredPool struct {
    *Pool
    submitted  prometheus.Counter
    completed  prometheus.Counter
    rejected   prometheus.Counter
    queueSize  prometheus.Gauge
    processing prometheus.Gauge
}

func (p *MonitoredPool) Submit(task func()) bool {
    if p.Pool.Submit(func() {
        p.processing.Inc()
        defer p.processing.Dec()
        task()
        p.completed.Inc()
    }) {
        p.submitted.Inc()
        p.queueSize.Set(float64(len(p.tasks)))
        return true
    }
    p.rejected.Inc()
    return false
}
```

### 3. Panic Recovery

```go
func (p *Pool) worker() {
    defer p.wg.Done()
    
    for task := range p.tasks {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    log.Printf("Worker panic recovered: %v\n%s", r, debug.Stack())
                }
            }()
            task()
        }()
    }
}
```

### 4. Context Propagation

```go
type ContextTask struct {
    Ctx  context.Context
    Task func(context.Context)
}

func (p *Pool) SubmitWithContext(ctx context.Context, task func(context.Context)) bool {
    return p.Submit(func() {
        // Check if context already cancelled
        if ctx.Err() != nil {
            return
        }
        task(ctx)
    })
}
```

---

## Summary

| Pattern | Use Case | Complexity |
|---------|----------|------------|
| Basic Pool | Simple task processing | Low |
| Result Pool | Need return values | Medium |
| Priority Pool | Different task priorities | Medium |
| Dynamic Pool | Variable load | High |

**Best Practices**:
1. Always set maximum limits
2. Implement graceful shutdown
3. Add monitoring metrics
4. Handle panics in workers
5. Propagate context for cancellation

---

**Next**: [Backpressure Mechanisms](./03-backpressure-mechanisms.md)

