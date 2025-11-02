# Goroutine Leaks - Most Common Memory Leak in Go

## Quick Links

- [← Back to Root](../)
- [Next: Long-Lived References →](../2.Long-Lived-References/)
- [Conceptual Explanation](#conceptual-explanation)
- [How to Detect](#how-to-detect-it)
- [Examples](#examples)
- [Resources](#resources--learning-materials)

---

## Conceptual Explanation

### What is a Goroutine Leak?

A goroutine leak occurs when goroutines are created but never terminate, accumulating in memory over time. Each goroutine consumes memory (typically 2-8 KB for the stack, though this can grow), and thousands of leaked goroutines can quickly exhaust available memory and degrade application performance.

Unlike traditional thread leaks in other languages, goroutine leaks are particularly insidious because:

1. **Lightweight Nature**: Goroutines are so cheap to create that developers spawn them liberally without considering lifecycle management
2. **Silent Accumulation**: The application continues running normally until resource exhaustion occurs
3. **Delayed Symptoms**: Leaks may not manifest for hours or days in production
4. **Complex Debugging**: Identifying which goroutines are leaked among hundreds of legitimate ones can be challenging

Goroutine leaks are the most common type of memory leak in Go applications because goroutines are a fundamental building block of Go concurrency. Any service that handles requests, processes events, or performs background work likely creates goroutines, and without proper lifecycle management, these can easily leak.

The Go runtime manages goroutines efficiently, but it cannot automatically terminate goroutines that are blocked or running infinite loops. This responsibility falls to the developer to ensure every created goroutine has a clear termination condition.

### Why Does It Happen?

Goroutine leaks typically occur due to one of these patterns:

**1. Channel Operations Without Exit Conditions**

The most common cause is a goroutine waiting to send or receive on a channel that will never be ready:

```go
ch := make(chan int)
go func() {
    result := expensiveOperation()
    ch <- result  // Blocks forever if no one reads
}()
// Caller moves on without reading from ch
```

**2. Missing Context Cancellation**

When goroutines don't respect context cancellation:

```go
func process(ctx context.Context) {
    go func() {
        for {
            doWork()  // Never checks ctx.Done()
        }
    }()
}
```

**3. Blocking Operations Without Timeouts**

Operations that block indefinitely:

```go
go func() {
    conn.Read(buffer)  // No timeout, may block forever
}()
```

**4. Select Statements Missing Context**

Select statements that don't include a cancellation case:

```go
go func() {
    select {
    case data := <-ch:
        process(data)
    // Missing: case <-ctx.Done(): return
    }
}()
```

The fundamental issue is that goroutines require explicit coordination for termination. Unlike some languages with automatic thread cleanup, Go requires developers to design goroutine lifecycles carefully. This is a tradeoff for Go's performance and simplicity: the runtime doesn't impose overhead tracking goroutine relationships, but developers must be diligent.

### Real-World Scenarios

**Scenario 1: HTTP Server with Background Processing**

A web service spawns a goroutine for each request to send analytics asynchronously:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    go sendAnalytics(r)  // No cancellation if client disconnects
    
    // Process request
    w.Write([]byte("OK"))
}
```

If `sendAnalytics` makes a network call that hangs, and requests come in at 100/second, you'll accumulate 360,000 leaked goroutines per hour.

**Scenario 2: Event Processing Pipeline**

A message processing system that spawns workers:

```go
func startWorkers(messages <-chan Message) {
    for msg := range messages {
        go processMessage(msg)  // Unbounded goroutine creation
    }
}
```

If messages arrive faster than they're processed, or if `processMessage` blocks, goroutines accumulate without bound.

**Scenario 3: Microservice with RPC Calls**

A service that makes RPC calls to other services:

```go
func fetchData(id string) Data {
    resultCh := make(chan Data)
    
    go func() {
        data := rpcClient.Fetch(id)  // May hang on network issues
        resultCh <- data
    }()
    
    return <-resultCh  // Caller has no timeout
}
```

If the RPC hangs, both the caller and the goroutine leak.

**Scenario 4: WebSocket Connection Handler**

WebSocket servers often spawn goroutines per connection:

```go
func handleConnection(conn *websocket.Conn) {
    go readMessages(conn)
    go writeMessages(conn)
    // No cleanup if connection dies unexpectedly
}
```

If connections drop without proper cleanup, goroutines accumulate waiting on dead connections.

---

## Technical Deep Dive

For in-depth understanding of the underlying mechanisms:

- [Conceptual Explanation](./resources/01-conceptual-explanation.md) - Extended discussion with more examples
- [Goroutine Internals](./resources/02-goroutine-internals.md) - How goroutines work in the Go runtime
- [Channel Mechanics](./resources/03-channel-mechanics.md) - Deep dive into channel blocking behavior
- [Context Pattern](./resources/04-context-pattern.md) - Using context for goroutine lifecycle management

---

## How to Detect It

### Specific Metrics

**Primary Indicator**: Growing `runtime.NumGoroutine()` count

```go
import "runtime"

func printGoroutineCount() {
    fmt.Printf("Current goroutines: %d\n", runtime.NumGoroutine())
}
```

**What to Look For**:
- Baseline: Small applications might have 5-10 goroutines at rest
- Concern: Count growing unbounded over time (not just during traffic spikes)
- Critical: Thousands of goroutines during idle periods

**Secondary Indicators**:
- Flat or slowly growing heap memory (goroutines use stack, not heap)
- Increasing number of goroutines in "blocked" state
- Application slowdown without corresponding CPU or memory pressure
- Eventually: Out of memory errors or system instability

### Tools to Use

**1. Runtime Metrics in Application**

Add monitoring to your application:

```go
import (
    "runtime"
    "time"
)

func monitorGoroutines() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        count := runtime.NumGoroutine()
        log.Printf("Goroutines: %d", count)
        
        if count > threshold {
            log.Printf("WARNING: High goroutine count!")
        }
    }
}
```

**2. pprof Goroutine Profile**

The goroutine profile shows all goroutines and their stack traces:

```bash
# Collect profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof

# View in browser
go tool pprof -http=:8081 goroutine_fixedEX.pprof

# Or text mode
go tool pprof goroutine_fixedEX.pprof
(pprof) top
(pprof) list functionName
```

**3. Debug Endpoint**

View goroutine stacks in plain text:

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

This shows stack traces for all goroutines, making it easy to spot patterns.

**4. Trace Analysis**

For detailed goroutine lifecycle analysis:

```bash
curl http://localhost:6060/debug/pprof/trace?seconds=10 > trace.out
go tool trace trace.out
```

The trace viewer shows goroutine creation, blocking, and termination events.

### Expected Values

**Healthy Application**:
- Goroutine count stabilizes after startup
- Count increases during traffic spikes, then decreases
- Typical range: 10-100 goroutines for small services
- Scales with active requests but has an upper bound

**Leaking Application**:
- Goroutine count grows monotonically
- Count never decreases even during idle periods
- Growth rate correlates with request rate or time
- Eventually reaches thousands or tens of thousands

**Detection Thresholds**:
- **Warning**: > 100 goroutines above baseline during idle
- **Critical**: > 1000 goroutines above baseline
- **Emergency**: > 10000 goroutines total

More detailed detection strategies: [Detection Methods](./resources/05-detection-methods.md)

---

## Examples

### Running Leaky Version

This example demonstrates a classic goroutine leak where goroutines are spawned to send on a channel, but no receiver exists.

```bash
cd 1.Goroutine-Leaks-Most-Common
go run example.go
```

**Expected Output**:

```
[START] Goroutines: 1
[AFTER 2s] Goroutines: 101
[AFTER 4s] Goroutines: 201
[AFTER 6s] Goroutines: 301
[AFTER 8s] Goroutines: 401
[AFTER 10s] Goroutines: 501

pprof server running on http://localhost:6060
Press Ctrl+C to stop
```

**What's Happening**:
- Application spawns 50 goroutines per second
- Each goroutine tries to send on an unbuffered channel
- No receiver exists, so goroutines block forever
- Goroutine count grows linearly: 50 goroutines/second × time

**In Another Terminal**:

```bash
# Collect goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine_leak.pprof

# View the leak
go tool pprof -http=:8081 goroutine_leak.pprof
```

You'll see hundreds of goroutines stuck in `chan send` operations.

---

### Running Fixed Version

This example shows the proper pattern using context for cancellation and graceful goroutine termination.

```bash
go run fixed_example.go
```

**Expected Output**:

```
[START] Goroutines: 1
[AFTER 2s] Goroutines: 1
[AFTER 4s] Goroutines: 1
[AFTER 6s] Goroutines: 1
[AFTER 8s] Goroutines: 1
[AFTER 10s] Goroutines: 1

pprof server running on http://localhost:6060
All goroutines cleaned up successfully
Press Ctrl+C to stop
```

**What's Different**:
- Uses `context.Context` for cancellation signaling
- Goroutines check `ctx.Done()` in select statements
- Buffered channel prevents blocking
- Proper cleanup ensures goroutines terminate

**Verification**:

```bash
# Collect profile from fixed version
curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixed.pprof

# Compare with leaked version
go tool pprof -base=goroutine_leak.pprof goroutine_fixed.pprof
```

The diff should show near-zero growth.

---

## Profiling Instructions

Comprehensive profiling guide: [pprof Analysis](./pprof_analysis.md)

**Quick Reference**:

```bash
# 1. Start the leaky example
go run example.go &

# 2. Collect initial profile
curl http://localhost:6060/debug/pprof/goroutine > profile1.pprof

# 3. Wait 30 seconds
sleep 30

# 4. Collect second profile
curl http://localhost:6060/debug/pprof/goroutine > profile2.pprof

# 5. Compare to see growth
go tool pprof -base=profile1.pprof profile2.pprof

# 6. View in browser
go tool pprof -http=:8081 profile2.pprof
```

---

## Resources & Learning Materials

### Core Concepts

1. [Conceptual Explanation](./resources/01-conceptual-explanation.md)
   - Extended discussion of goroutine leaks
   - More code examples and patterns
   - Common mistakes and misconceptions
   - Read time: 15 minutes

2. [Goroutine Internals](./resources/02-goroutine-internals.md)
   - How the Go scheduler manages goroutines
   - Goroutine states and transitions
   - Memory layout and stack growth
   - Read time: 20 minutes

3. [Channel Mechanics](./resources/03-channel-mechanics.md)
   - How channels cause blocking
   - Buffered vs unbuffered channels
   - Send and receive semantics
   - When goroutines become unblockable
   - Read time: 20 minutes

### Patterns & Best Practices

4. [Context Pattern](./resources/04-context-pattern.md)
   - Using context.Context for cancellation
   - Context propagation patterns
   - Timeout and deadline patterns
   - Best practices for context usage
   - Read time: 25 minutes

5. [Detection Methods](./resources/05-detection-methods.md)
   - Runtime metrics and monitoring
   - Setting up alerting
   - Profiling strategies
   - Production debugging techniques
   - Read time: 20 minutes

### Visual Learning

6. [Visual Diagrams](./resources/06-visual-diagrams.md)
   - Goroutine lifecycle diagrams
   - Channel blocking visualizations
   - Timeline comparisons (leak vs fixed)
   - State transition diagrams
   - Read time: 15 minutes

### Advanced Topics

7. [Real-World Examples](./resources/07-real-world-examples.md)
   - Production case studies
   - HTTP handler patterns
   - Worker pool implementations
   - Common pitfalls in major Go projects
   - Debugging war stories
   - Read time: 30 minutes

---

## Key Takeaways

1. **Goroutine leaks are the #1 memory leak in Go** - They're easy to create and hard to detect until it's too late.

2. **Every goroutine needs an exit condition** - Design for termination from the start. Ask: "How will this goroutine end?"

3. **Use context.Context for cancellation** - It's the standard Go pattern for propagating cancellation signals through goroutine trees.

4. **Monitor runtime.NumGoroutine() in production** - Set up alerts for unexpected growth. This is your early warning system.

5. **Profile regularly during development** - Don't wait for production. Run `go tool pprof` during testing to catch leaks early.

6. **Blocked goroutines are usually leaks** - If your pprof shows goroutines blocked in channel operations, investigate immediately.

7. **Consider worker pools for fan-out patterns** - Instead of spawning goroutines per task, use a fixed pool with a work queue (see [Unbounded Resources](../5.Unbounded-Resources/)).

---

## Related Leak Types

- [Unbounded Resources](../5.Unbounded-Resources/) - Related issue of creating too many goroutines at once
- [Resource Leaks](../3.Resource-Leaks/) - Goroutines that leak because they hold resources
- [Detection Decision Tree](../visual-guides/detection-decision-tree.md) - Help identifying your specific leak type

---

**Next Steps**: Try the [Long-Lived References](../2.Long-Lived-References/) examples to learn about memory-based leaks.

