# Conceptual Explanation: Goroutine Leaks

**Read Time**: 15 minutes

**Prerequisites**: Basic understanding of goroutines and channels

**Related Topics**: 
- [Goroutine Internals](./02-goroutine-internals.md)
- [Channel Mechanics](./03-channel-mechanics.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [What Exactly Is a Goroutine Leak?](#what-exactly-is-a-goroutine-leak)
2. [The Lifecycle Problem](#the-lifecycle-problem)
3. [Why Goroutine Leaks Are Common](#why-goroutine-leaks-are-common)
4. [Detailed Examples](#detailed-examples)
5. [The Cost of Leaked Goroutines](#the-cost-of-leaked-goroutines)
6. [Common Misconceptions](#common-misconceptions)
7. [Summary](#summary)

---

## What Exactly Is a Goroutine Leak?

A goroutine leak occurs when a goroutine is created but never terminates, remaining in memory indefinitely. Unlike memory allocated on the heap that can be garbage collected, goroutines are only cleaned up when they complete execution and return.

### The Technical Definition

A goroutine is considered **leaked** when:

1. **It will never terminate naturally** - The goroutine is in an infinite loop or permanently blocked
2. **No mechanism exists to unblock it** - No code path will cause the goroutine to exit
3. **It consumes resources indefinitely** - Stack memory, scheduler overhead, and potentially references to heap objects

### Why Goroutines Don't Auto-Cleanup

Go's garbage collector can reclaim heap memory, but it **cannot** terminate goroutines. This is by design:

- The GC doesn't know if a blocked goroutine might become unblocked later
- Forcefully terminating goroutines could leave shared state inconsistent
- Goroutines might hold locks or other resources that need explicit cleanup

Therefore, **goroutine lifecycle management is the developer's responsibility**.

### A Simple Analogy

Think of goroutines like threads in an operating system:

- **Creating**: Easy and cheap (like spawning threads)
- **Running**: Managed by the scheduler (like thread scheduling)
- **Terminating**: Must exit naturally or be signaled (like joining threads)

You wouldn't spawn a thread and forget about it - the same discipline applies to goroutines.

---

## The Lifecycle Problem

### Normal Goroutine Lifecycle

A healthy goroutine goes through these stages:

```
Created → Running → Blocked (optional) → Running → Completed → Cleaned Up
```

Example of a healthy lifecycle:

```go
func healthyWorker() {
    go func() {
        // 1. Created
        data := fetchData()      // 2. Running
        time.Sleep(time.Second)  // 3. Blocked (temporary)
        process(data)            // 4. Running again
    }()                          // 5. Completes and exits
    // 6. GC cleans up the goroutine's stack
}
```

### Leaked Goroutine Lifecycle

A leaked goroutine gets stuck:

```
Created → Running → Blocked → [STUCK FOREVER]
```

Example of a leak:

```go
func leakyWorker() {
    ch := make(chan int)
    go func() {
        // 1. Created
        data := fetchData()   // 2. Running
        ch <- data            // 3. Blocked - FOREVER
        // Never reaches completion
    }()
    // Caller doesn't read from ch, so goroutine never unblocks
}
```

The goroutine at step 3 will:
- Remain in the scheduler's goroutine list
- Continue consuming stack memory
- Never be cleaned up
- Accumulate if called repeatedly

---

## Why Goroutine Leaks Are Common

### 1. Goroutines Are Too Easy to Create

Go makes goroutine creation trivially easy:

```go
go someFunction()  // That's it!
```

This low barrier leads to casual creation without considering termination:

```go
// Dangerous pattern - spawning without lifecycle consideration
http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
    go sendMetrics(r)  // When does this terminate?
    go logRequest(r)   // What if logging service is down?
    go updateCache(r)  // What if cache is full?
    
    // Handler returns, but goroutines may live forever
})
```

### 2. Channels Can Block Silently

Channel operations look simple but can block forever:

```go
ch := make(chan int)

// This blocks forever if no receiver exists
ch <- 42

// This blocks forever if no sender exists and channel isn't closed
val := <-ch
```

The problem: **The code looks synchronous but behaves asynchronously**, leading to unexpected blocking.

### 3. No Compiler Warnings

The Go compiler doesn't warn about potential goroutine leaks:

```go
func problematic() {
    ch := make(chan int)
    go func() {
        ch <- 42  // No compiler warning this might block forever
    }()
    // No warning that channel is never read
}
```

Compare this to unused variables, which the compiler catches. Goroutine leaks require runtime analysis.

### 4. Asynchronous Error Handling Is Hard

When a goroutine encounters an error, propagating it to the caller is non-trivial:

```go
func fetchData() error {
    go func() {
        err := database.Query()
        if err != nil {
            // How do we report this error?
            // Caller has moved on!
            return  // Goroutine exits, but error is lost
        }
    }()
    return nil  // Can't return the goroutine's error
}
```

Without error channels or contexts, goroutines may silently fail and never report issues.

### 5. Testing Doesn't Catch Leaks Easily

Unit tests often don't run long enough to detect leaks:

```go
func TestFetchData(t *testing.T) {
    result := fetchData()
    if result != expected {
        t.Fail()
    }
    // Test passes, but goroutine is still running in background
}
```

Tests need explicit goroutine count checks or longer run times to detect leaks.

---

## Detailed Examples

### Example 1: The Classic Channel Send Leak

**Scenario**: Worker sends result on channel, but no one reads it.

```go
func processTask(id int) {
    resultCh := make(chan Result)
    
    go func() {
        result := expensiveComputation(id)
        resultCh <- result  // BLOCKS FOREVER
    }()
    
    // Function returns immediately
    // Goroutine is orphaned and never unblocks
}
```

**Why It Leaks**:
- Unbuffered channel requires a receiver to be ready
- Caller doesn't read from `resultCh`
- Goroutine blocks at send and never exits

**How to Fix**:
```go
func processTaskFixed(id int) Result {
    resultCh := make(chan Result, 1)  // Buffered channel
    
    go func() {
        result := expensiveComputation(id)
        resultCh <- result  // Sends immediately (non-blocking)
    }()
    
    return <-resultCh  // Receive the result
}
```

Or better, use context for cancellation:
```go
func processTaskBetter(ctx context.Context, id int) (Result, error) {
    resultCh := make(chan Result, 1)
    
    go func() {
        result := expensiveComputation(id)
        select {
        case resultCh <- result:
        case <-ctx.Done():
            // Context cancelled, exit without sending
        }
    }()
    
    select {
    case result := <-resultCh:
        return result, nil
    case <-ctx.Done():
        return Result{}, ctx.Err()
    }
}
```

### Example 2: The Infinite Loop Without Cancellation

**Scenario**: Background worker runs forever with no stop mechanism.

```go
func startMonitor() {
    go func() {
        for {
            checkSystemHealth()
            time.Sleep(10 * time.Second)
        }
        // Never exits
    }()
}
```

**Why It Leaks**:
- Infinite loop with no break condition
- No way to signal the goroutine to stop
- Goroutine runs until program exits

**How to Fix**:
```go
func startMonitorFixed(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(10 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                checkSystemHealth()
            case <-ctx.Done():
                return  // Exit when context is cancelled
            }
        }
    }()
}
```

### Example 3: The HTTP Request Goroutine Leak

**Scenario**: HTTP handler spawns goroutine but doesn't wait for it.

```go
func handler(w http.ResponseWriter, r *http.Request) {
    go func() {
        // Make external API call
        resp, err := http.Get("https://api.example.com/data")
        if err != nil {
            return
        }
        defer resp.Body.Close()
        
        // Process response
        processAPIResponse(resp)
    }()
    
    // Handler returns immediately
    w.Write([]byte("Request received"))
}
```

**Why It Leaks**:
- If `http.Get` hangs (no timeout), goroutine waits forever
- If handler is called 1000 times/minute, creates 1000 goroutines/minute
- Goroutines accumulate faster than they complete

**How to Fix**:
```go
func handlerFixed(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    
    go func() {
        req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.example.com/data", nil)
        
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return
        }
        defer resp.Body.Close()
        
        processAPIResponse(resp)
    }()
    
    w.Write([]byte("Request received"))
}
```

Better: Don't spawn the goroutine at all, or use a worker pool pattern.

### Example 4: The Select Without Default or Context

**Scenario**: Select statement waits on channels that might never be ready.

```go
func processEvents(eventCh, controlCh chan Event) {
    go func() {
        for {
            select {
            case evt := <-eventCh:
                handleEvent(evt)
            case ctrl := <-controlCh:
                handleControl(ctrl)
            // No default, no context - can block forever
            }
        }
    }()
}
```

**Why It Leaks**:
- If both channels are never written to, goroutine blocks forever
- If channels are closed but goroutine doesn't handle closure, it may panic or busy-loop

**How to Fix**:
```go
func processEventsFixed(ctx context.Context, eventCh, controlCh chan Event) {
    go func() {
        for {
            select {
            case evt, ok := <-eventCh:
                if !ok {
                    return  // Channel closed
                }
                handleEvent(evt)
            case ctrl, ok := <-controlCh:
                if !ok {
                    return  // Channel closed
                }
                handleControl(ctrl)
            case <-ctx.Done():
                return  // Cancellation requested
            }
        }
    }()
}
```

---

## The Cost of Leaked Goroutines

### Memory Cost

**Stack Memory**:
- Initial stack: 2 KB per goroutine (Go 1.14+)
- Can grow up to 1 GB per goroutine
- 10,000 leaked goroutines: 20 MB minimum, potentially 10+ GB

**Heap References**:
- Goroutine stack holds pointers to heap objects
- These objects can't be garbage collected
- Compounds memory usage

**Example Calculation**:
```
1000 requests/minute × 1 leaked goroutine/request
= 1000 goroutines/minute
= 60,000 goroutines/hour
= 1,440,000 goroutines/day

At 2 KB each: 2.88 GB/day just for stacks
Plus any heap objects they reference
```

### CPU Cost

**Scheduler Overhead**:
- Scheduler must track all goroutines
- More goroutines = more scheduling work
- Degraded performance even if goroutines are blocked

**Context Switching**:
- Blocked goroutines still participate in scheduling
- CPU cycles wasted checking if they're ready

### Operational Cost

**Debugging Difficulty**:
- Hard to distinguish leaked from legitimate goroutines
- Large pprof profiles difficult to analyze
- Production incidents harder to diagnose

**System Instability**:
- Out-of-memory kills
- Cascading failures as system struggles
- Difficult to recover without restart

---

## Common Misconceptions

### Misconception 1: "The Garbage Collector Will Clean Them Up"

**False**. The GC only reclaims heap memory. Goroutines must exit naturally.

### Misconception 2: "Blocked Goroutines Don't Use Resources"

**False**. Blocked goroutines:
- Still consume stack memory
- Are tracked by the scheduler
- May hold references to heap objects
- Contribute to scheduler overhead

### Misconception 3: "Small Leaks Don't Matter"

**False**. Small leaks accumulate:
- 1 goroutine/request × 100 requests/sec = 360,000 goroutines/hour
- Production systems run for days or weeks
- Slow leaks are harder to detect and more dangerous

### Misconception 4: "Buffered Channels Prevent Leaks"

**Partially False**. Buffered channels prevent blocking only if:
- Buffer is large enough for all sends
- Receivers eventually read all data
- But goroutine still doesn't exit unless it has completion logic

### Misconception 5: "This Only Affects Long-Running Services"

**False**. Even short-lived programs can leak:
- CLI tools that process many files
- Test suites with leaky tests
- Batch jobs that run for hours
- Scripts that spawn goroutines per item

---

## Summary

### Key Points

1. **Goroutine leaks are permanent** - Goroutines don't auto-terminate
2. **Blocking is the usual cause** - Channel operations and infinite loops
3. **Context is the standard solution** - Use `context.Context` for cancellation
4. **Prevention beats debugging** - Design for termination from the start
5. **Monitor goroutine count** - Watch `runtime.NumGoroutine()` in production

### Checklist for Preventing Goroutine Leaks

When creating a goroutine, ask:

- [ ] Does this goroutine have a clear exit condition?
- [ ] Can it be cancelled via context?
- [ ] Will it unblock if the caller returns?
- [ ] Is there a timeout for blocking operations?
- [ ] Does the select statement include `<-ctx.Done()`?
- [ ] Have I tested goroutine count before and after this code?

### When to Use Goroutines

**Good Use Cases**:
- Fixed worker pools with known size
- Request handlers with proper cancellation
- Background tasks with explicit lifecycle
- Fan-out with WaitGroup coordination

**Risky Use Cases** (require extra care):
- One goroutine per external request/item
- Goroutines in library code
- Nested goroutine spawning
- Goroutines without timeout or cancellation

---

## Next Steps

- **Understand the runtime**: Read [Goroutine Internals](./02-goroutine-internals.md)
- **Learn channel details**: Read [Channel Mechanics](./03-channel-mechanics.md)
- **Master the fix**: Read [Context Pattern](./04-context-pattern.md)
- **See it in action**: Review [Visual Diagrams](./06-visual-diagrams.md)
- **Study production cases**: Read [Real-World Examples](./07-real-world-examples.md)

---

**Return to**: [Goroutine Leaks README](../README.md)

