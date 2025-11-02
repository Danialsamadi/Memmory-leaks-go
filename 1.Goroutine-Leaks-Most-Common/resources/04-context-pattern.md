# Context Pattern: Using Context to Prevent Goroutine Leaks

**Read Time**: 25 minutes

**Prerequisites**: Understanding of goroutines and channels

**Related Topics**:
- [Conceptual Explanation](./01-conceptual-explanation.md)
- [Channel Mechanics](./03-channel-mechanics.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Introduction to Context](#introduction-to-context)
2. [Context Types](#context-types)
3. [Context Propagation](#context-propagation)
4. [Cancellation Pattern](#cancellation-pattern)
5. [Timeout Pattern](#timeout-pattern)
6. [Deadline Pattern](#deadline-pattern)
7. [Context Best Practices](#context-best-practices)
8. [Common Pitfalls](#common-pitfalls)
9. [Summary](#summary)

---

## Introduction to Context

### What Is Context?

`context.Context` is Go's standard mechanism for:
- **Cancellation signaling**: Telling goroutines to stop
- **Deadline propagation**: Imposing time limits
- **Request-scoped values**: Carrying request metadata

Defined in the `context` package:

```go
type Context interface {
    Done() <-chan struct{}           // Closed when context is cancelled
    Err() error                      // Why context was cancelled
    Deadline() (deadline time.Time, ok bool)  // When context expires
    Value(key interface{}) interface{}        // Request-scoped values
}
```

### The Done Channel

The most important method for leak prevention:

```go
ctx.Done()  // Returns a channel that's closed on cancellation
```

Pattern:
```go
select {
case <-ctx.Done():
    // Context cancelled, stop work and return
    return ctx.Err()
case result := <-workCh:
    // Continue normal work
}
```

### Why Context Prevents Leaks

Without context:
```go
go func() {
    for {
        doWork()  // Runs forever, no way to stop
    }
}()
```

With context:
```go
go func(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return  // Exit when cancelled
        default:
            doWork()
        }
    }
}(ctx)
```

**Key**: `ctx.Done()` provides a **guaranteed exit path** for every goroutine.

---

## Context Types

### Background Context

The root context, never cancelled:

```go
ctx := context.Background()
```

**Use when**:
- Starting your application (main function)
- Top-level requests without cancellation
- Tests where cancellation isn't needed

**Example**:
```go
func main() {
    ctx := context.Background()
    server := NewServer(ctx)
    server.Run()
}
```

### TODO Context

Placeholder when context should be added later:

```go
ctx := context.TODO()
```

**Use when**:
- Prototyping
- Planning to add proper context later
- Not sure what context to use yet

**Note**: `TODO()` is identical to `Background()` but signals intent.

### WithCancel

Creates a cancellable context:

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()  // Always call cancel
```

**Calling `cancel()`**:
- Closes the `Done()` channel
- Sets `Err()` to `context.Canceled`
- Cancels all child contexts

**Example**:
```go
func worker(ctx context.Context) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()
    
    go func() {
        <-time.After(5 * time.Second)
        cancel()  // Cancel after 5 seconds
    }()
    
    select {
    case <-ctx.Done():
        fmt.Println("Cancelled:", ctx.Err())
    }
}
```

### WithTimeout

Automatically cancels after a duration:

```go
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
defer cancel()
```

**Equivalent to**:
```go
ctx, cancel := context.WithDeadline(parent, time.Now().Add(5*time.Second))
```

**Example**:
```go
func fetchWithTimeout(url string) ([]byte, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err  // May be context.DeadlineExceeded
    }
    defer resp.Body.Close()
    
    return io.ReadAll(resp.Body)
}
```

### WithDeadline

Cancels at a specific time:

```go
deadline := time.Now().Add(10 * time.Second)
ctx, cancel := context.WithDeadline(parent, deadline)
defer cancel()
```

**Example**:
```go
func processUntil(ctx context.Context, deadline time.Time) error {
    ctx, cancel := context.WithDeadline(ctx, deadline)
    defer cancel()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if err := processItem(); err != nil {
                return err
            }
        }
    }
}
```

### WithValue

Carries request-scoped values (use sparingly):

```go
ctx := context.WithValue(parent, key, value)
```

**Not for cancellation**, but for carrying:
- Request IDs
- User authentication
- Trace information

**Example**:
```go
type RequestIDKey struct{}

func withRequestID(ctx context.Context, requestID string) context.Context {
    return context.WithValue(ctx, RequestIDKey{}, requestID)
}

func getRequestID(ctx context.Context) string {
    if id, ok := ctx.Value(RequestIDKey{}).(string); ok {
        return id
    }
    return ""
}
```

---

## Context Propagation

### The Context Tree

Contexts form a parent-child tree:

```
context.Background()
    ├── WithTimeout(10s)
    │   ├── WithCancel()
    │   │   └── (your goroutine)
    │   └── WithValue("userID", 123)
    └── WithCancel()
        └── WithTimeout(5s)
```

**Cancellation propagates down**:
- Cancelling a parent cancels all children
- Cancelling a child doesn't affect parent or siblings

### Propagation Example

```go
func demonstratePropagation() {
    root := context.Background()
    
    parent, parentCancel := context.WithCancel(root)
    child1, child1Cancel := context.WithCancel(parent)
    child2, child2Cancel := context.WithCancel(parent)
    
    // Cancelling parent affects both children
    parentCancel()
    
    <-child1.Done()  // Closed
    <-child2.Done()  // Closed
    
    // But cancelling child doesn't affect parent
    grandchild, gcCancel := context.WithCancel(parent)
    gcCancel()
    
    select {
    case <-parent.Done():
        // Doesn't happen
    default:
        // Parent still active
    }
}
```

### HTTP Request Context

HTTP requests come with built-in context:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()  // Cancelled when client disconnects
    
    go backgroundWork(ctx)  // Automatically cancelled if client leaves
    
    // Process request
}
```

**Benefits**:
- No manual cancellation needed
- Goroutines stop if client disconnects
- Prevents work on abandoned requests

---

## Cancellation Pattern

### Basic Cancellation

```go
func worker(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            log.Println("Worker cancelled:", ctx.Err())
            return
        default:
            doWork()
        }
    }
}

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    
    go worker(ctx)
    
    time.Sleep(5 * time.Second)
    cancel()  // Stop the worker
    time.Sleep(100 * time.Millisecond)  // Give time to clean up
}
```

### Channel + Context

Combining channels with context:

```go
func processor(ctx context.Context, input <-chan int) {
    for {
        select {
        case <-ctx.Done():
            return  // Cancelled
        case val, ok := <-input:
            if !ok {
                return  // Channel closed
            }
            process(val)
        }
    }
}
```

### Multiple Goroutines

Cancelling multiple goroutines with one context:

```go
func startWorkers(ctx context.Context, n int) {
    var wg sync.WaitGroup
    
    for i := 0; i < n; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            
            for {
                select {
                case <-ctx.Done():
                    log.Printf("Worker %d stopped\n", id)
                    return
                default:
                    log.Printf("Worker %d working\n", id)
                    time.Sleep(time.Second)
                }
            }
        }(i)
    }
    
    wg.Wait()  // Wait for all workers to finish
}

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    
    go startWorkers(ctx, 5)
    
    time.Sleep(3 * time.Second)
    cancel()  // All 5 workers stop
}
```

---

## Timeout Pattern

### Operation Timeout

```go
func fetchData(url string) ([]byte, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        // Check if it's a timeout
        if ctx.Err() == context.DeadlineExceeded {
            return nil, fmt.Errorf("request timed out after 5s")
        }
        return nil, err
    }
    defer resp.Body.Close()
    
    return io.ReadAll(resp.Body)
}
```

### Database Query Timeout

```go
func queryWithTimeout(db *sql.DB, query string, timeout time.Duration) (*sql.Rows, error) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    return db.QueryContext(ctx, query)
}

// Usage
rows, err := queryWithTimeout(db, "SELECT * FROM users", 3*time.Second)
if err != nil {
    if err == context.DeadlineExceeded {
        log.Println("Query timed out")
    }
    return err
}
defer rows.Close()
```

### Timeout with Fallback

```go
func fetchWithFallback(primaryURL, fallbackURL string) ([]byte, error) {
    // Try primary with short timeout
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()
    
    data, err := fetch(ctx, primaryURL)
    if err == nil {
        return data, nil
    }
    
    // Fallback with longer timeout
    ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel2()
    
    return fetch(ctx2, fallbackURL)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}
```

---

## Deadline Pattern

### Absolute Deadline

```go
func processWithDeadline(deadline time.Time) error {
    ctx, cancel := context.WithDeadline(context.Background(), deadline)
    defer cancel()
    
    for {
        select {
        case <-ctx.Done():
            return fmt.Errorf("deadline exceeded: %w", ctx.Err())
        default:
            if err := processItem(); err != nil {
                return err
            }
        }
    }
}

// Usage: process until 5 PM
deadline := time.Date(2025, 11, 2, 17, 0, 0, 0, time.Local)
err := processWithDeadline(deadline)
```

### Remaining Time Pattern

```go
func processWithTimeCheck(ctx context.Context) error {
    deadline, ok := ctx.Deadline()
    if !ok {
        return fmt.Errorf("no deadline set")
    }
    
    for {
        remaining := time.Until(deadline)
        if remaining < time.Second {
            return fmt.Errorf("insufficient time remaining")
        }
        
        if err := processItem(); err != nil {
            return err
        }
    }
}
```

---

## Context Best Practices

### Rule 1: Always Pass Context as First Parameter

```go
// Good
func process(ctx context.Context, data Data) error

// Bad
func process(data Data, ctx context.Context) error
```

### Rule 2: Never Store Context in Structs

```go
// Bad
type Worker struct {
    ctx context.Context  // DON'T DO THIS
}

// Good
type Worker struct {
    // No context field
}

func (w *Worker) Work(ctx context.Context) {
    // Pass context as parameter
}
```

**Exception**: Contexts that live for the entire program lifetime.

### Rule 3: Always Defer cancel()

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()  // ALWAYS call cancel, even if context times out
```

**Why**: Releases resources (timers, goroutines).

### Rule 4: Don't Pass nil Context

```go
// Bad
process(nil, data)

// Good - use Background if no context available
process(context.Background(), data)

// Better - use TODO if you plan to add context later
process(context.TODO(), data)
```

### Rule 5: Check ctx.Done() in Loops

```go
// Good
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        work()
    }
}

// Also good
for {
    if ctx.Err() != nil {
        return ctx.Err()
    }
    work()
}
```

### Rule 6: Propagate Context Through Call Chains

```go
func handler(ctx context.Context) {
    processRequest(ctx)  // Pass it down
}

func processRequest(ctx context.Context) {
    fetchData(ctx)  // Keep passing it
}

func fetchData(ctx context.Context) {
    // Use it
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
}
```

---

## Common Pitfalls

### Pitfall 1: Not Checking ctx.Done() in Select

```go
// Bad - goroutine leaks if ch never receives
select {
case val := <-ch:
    process(val)
}

// Good - can be cancelled
select {
case val := <-ch:
    process(val)
case <-ctx.Done():
    return ctx.Err()
}
```

### Pitfall 2: Blocking Operations Without Context

```go
// Bad - blocks forever if conn hangs
data, err := conn.Read(buffer)

// Good - respects context cancellation
type result struct {
    data []byte
    err  error
}
resultCh := make(chan result, 1)

go func() {
    data, err := conn.Read(buffer)
    resultCh <- result{data, err}
}()

select {
case res := <-resultCh:
    // Use res.data, res.err
case <-ctx.Done():
    return ctx.Err()
}
```

### Pitfall 3: Creating Context Inside Goroutine

```go
// Bad - parent context not passed
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    work(ctx)  // Can't be cancelled from outside
}()

// Good - receives context from parent
go func(ctx context.Context) {
    work(ctx)  // Respects parent cancellation
}(ctx)
```

### Pitfall 4: Ignoring Context Errors

```go
// Bad - doesn't distinguish timeout from other errors
err := operation(ctx)
if err != nil {
    return err
}

// Good - handles context errors specifically
err := operation(ctx)
if err != nil {
    if ctx.Err() == context.DeadlineExceeded {
        return fmt.Errorf("operation timed out: %w", err)
    }
    if ctx.Err() == context.Canceled {
        return fmt.Errorf("operation cancelled: %w", err)
    }
    return err
}
```

### Pitfall 5: Forgetting to Call cancel()

```go
// Bad - leaks timer goroutine
func bad() {
    ctx, cancel := context.WithTimeout(parent, 5*time.Second)
    // Forgot: defer cancel()
    
    work(ctx)
}

// Good - timer cleaned up
func good() {
    ctx, cancel := context.WithTimeout(parent, 5*time.Second)
    defer cancel()  // Stops timer even if work() finishes early
    
    work(ctx)
}
```

---

## Summary

### Context Prevents Leaks By

1. **Providing cancellation signals** - `ctx.Done()` channel closes
2. **Propagating timeouts** - Automatic cancellation after duration
3. **Coordinating multiple goroutines** - All children cancelled together
4. **Integrating with standard library** - HTTP, DB, etc. respect context

### Mental Model

Think of context as a **cancellation token** that:
- Flows down through your call stack
- Can be cancelled at any level
- Cancels all descendants when cancelled
- Provides consistent error handling

### Quick Reference

```go
// Creating contexts
ctx := context.Background()                      // Root context
ctx := context.TODO()                            // Placeholder
ctx, cancel := context.WithCancel(parent)        // Manual cancellation
ctx, cancel := context.WithTimeout(parent, dur)  // Timeout
ctx, cancel := context.WithDeadline(parent, t)   // Deadline

// Using contexts
select {
case <-ctx.Done():                // Check for cancellation
    return ctx.Err()              // Return cancellation error
case result := <-ch:
    // Normal work
}

// Always
defer cancel()  // Clean up resources
```

### Anti-Leak Pattern

Every goroutine should follow this pattern:

```go
func worker(ctx context.Context, input <-chan T) {
    for {
        select {
        case <-ctx.Done():
            // Cleanup and exit
            return
        case item, ok := <-input:
            if !ok {
                // Channel closed, exit
                return
            }
            // Process item
            process(item)
        }
    }
}
```

**Two exit paths**: Context cancellation OR channel closure.

---

## Further Reading

**Related Resources**:
- [Detection Methods](./05-detection-methods.md) - Detecting goroutines that ignore context
- [Real-World Examples](./07-real-world-examples.md) - Production context patterns
- [Visual Diagrams](./06-visual-diagrams.md) - Context cancellation visualizations

**Official Documentation**:
- [context package](https://pkg.go.dev/context)
- [Go Blog: Context](https://go.dev/blog/context)

---

**Return to**: [Goroutine Leaks README](../README.md)

