# Defer Mechanics Deep Dive

**Read Time**: ~22 minutes

**Prerequisites**: Basic Go syntax, understanding of stack and function calls

**Summary**: Master Go's defer statement—how it works under the hood, common pitfalls, advanced patterns, and why it's critical for resource management.

---

## Introduction

The `defer` statement is Go's primary mechanism for resource cleanup. While it appears simple, its behavior has subtle edge cases that can lead to bugs and resource leaks if misunderstood. This guide explores defer's implementation, execution model, and best practices.

## How Defer Works Under the Hood

### The Defer Stack

Each function maintains a **defer stack** (LIFO: Last In, First Out) of deferred function calls:

```go
func example() {
    defer fmt.Println("First")   // Pushed to stack: [First]
    defer fmt.Println("Second")  // Pushed to stack: [Second, First]
    defer fmt.Println("Third")   // Pushed to stack: [Third, Second, First]
    fmt.Println("Body")
}
// Output:
// Body
// Third   ← Popped from stack
// Second  ← Popped from stack
// First   ← Popped from stack
```

**Execution Order**: LIFO (reverse of declaration)

### defer at the Assembly Level

When you write:

```go
func processFile() error {
    file, err := os.Open("data.txt")
    if err != nil {
        return err
    }
    defer file.Close()
    
    // ... use file
    return nil
}
```

The compiler generates (simplified):

```go
func processFile() (err error) {
    var deferStack []func()  // Conceptual defer stack
    
    file, err := os.Open("data.txt")
    if err != nil {
        goto returnLabel
    }
    
    // Register deferred function
    deferStack = append(deferStack, func() { file.Close() })
    
    // ... use file
    err = nil
    
returnLabel:
    // Execute all deferred functions in reverse order
    for i := len(deferStack) - 1; i >= 0; i-- {
        deferStack[i]()
    }
    return err
}
```

**Key Insight**: Defers execute at **function return**, not block exit.

## The Three Rules of Defer

### Rule 1: Arguments Are Evaluated Immediately

Deferred function arguments are evaluated when the `defer` statement runs, NOT when the function executes.

```go
func rule1() {
    i := 0
    defer fmt.Println(i)  // Evaluates i=0 NOW
    i++
    fmt.Println(i)
}
// Output:
// 1  ← from fmt.Println(i)
// 0  ← from defer (captured i=0)
```

**Common Mistake: Loop Variables**

```go
// ❌ WRONG: All defers print the same value
func wrongLoop() {
    for i := 0; i < 3; i++ {
        defer fmt.Println(i)  // Captures i by value at each iteration
    }
}
// Output: 2, 1, 0 (reverse order)
// Wait, this actually works correctly!

// ❌ REALLY WRONG: Closure capturing
func reallyWrongLoop() {
    for i := 0; i < 3; i++ {
        defer func() {
            fmt.Println(i)  // Captures i by REFERENCE
        }()
    }
}
// Output: 3, 3, 3 (all see final value of i)

// ✅ CORRECT: Pass as parameter or capture explicitly
func correctLoop() {
    for i := 0; i < 3; i++ {
        defer func(val int) {
            fmt.Println(val)
        }(i)  // Capture i by value
    }
}
// Output: 2, 1, 0 ✅
```

### Rule 2: Defer Executes on Function Return

Defers run when the function **returns**, regardless of how it returns:

```go
func rule2() {
    defer fmt.Println("Defer 1")
    defer fmt.Println("Defer 2")
    
    if someCondition {
        return  // Defers still execute!
    }
    
    if anotherCondition {
        return  // Defers still execute!
    }
    
    // Normal return - defers execute
}
```

**Exception: `os.Exit()` and `log.Fatal()`**

```go
func noDefer() {
    defer fmt.Println("Will this run?")
    os.Exit(1)  // ❌ Program terminates immediately, defer skipped!
}

func alsoNoDefer() {
    defer fmt.Println("Will this run?")
    log.Fatal("error")  // ❌ Calls os.Exit(1) internally, defer skipped!
}
```

**When to use defer**:
- ✅ Regular returns
- ✅ Panic (defer runs before unwinding)
- ❌ `os.Exit()` / `log.Fatal()` (immediate termination)

### Rule 3: Defer Runs After Named Return Values Are Set

```go
func rule3() (result int) {
    defer func() {
        result++  // ✅ Can modify named return value
    }()
    
    return 41  // Sets result = 41, then defer runs
}
// Returns: 42 (41 + 1 from defer)

func anotherExample() (result int) {
    defer func() {
        if result < 0 {
            result = 0  // ✅ Sanitize return value
        }
    }()
    
    return -10  // Sets result = -10, defer modifies to 0
}
// Returns: 0
```

**Use Cases**:
- Error wrapping
- Metric recording
- Return value sanitization
- Panic recovery

## Defer and Resource Management

### Pattern 1: Standard Resource Cleanup

```go
func standardPattern(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()  // ✅ Runs on all return paths
    
    // Multiple return points - all safe
    data := make([]byte, 1024)
    n, err := file.Read(data)
    if err != nil {
        return err  // defer executes
    }
    
    if n == 0 {
        return io.EOF  // defer executes
    }
    
    return process(data[:n])  // defer executes
}
```

### Pattern 2: Checking Close Errors

```go
// ❌ WRONG: Ignoring Close() error
defer file.Close()

// ✅ GOOD: Logging Close() error
defer func() {
    if err := file.Close(); err != nil {
        log.Printf("failed to close file: %v", err)
    }
}()

// ✅ BETTER: Returning Close() error
func withCloseError(path string) (err error) {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer func() {
        closeErr := file.Close()
        if err == nil {
            err = closeErr  // Return close error if no other error
        }
    }()
    
    return process(file)
}
```

### Pattern 3: Multi-Resource Cleanup

```go
func multiResource() error {
    // Acquire resource 1
    r1, err := acquireResource1()
    if err != nil {
        return err
    }
    defer r1.Close()  // Cleanup 1
    
    // Acquire resource 2
    r2, err := acquireResource2()
    if err != nil {
        return err  // r1 cleanup happens
    }
    defer r2.Close()  // Cleanup 2
    
    // Acquire resource 3
    r3, err := acquireResource3()
    if err != nil {
        return err  // r2 and r1 cleanup happen (LIFO)
    }
    defer r3.Close()  // Cleanup 3
    
    return useResources(r1, r2, r3)
    // Cleanup order: r3, r2, r1 (LIFO)
}
```

## The Defer-in-Loop Anti-Pattern

### Why It's a Problem

```go
// ❌ DANGER: defer accumulates in loop
func processFiles(paths []string) error {
    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            return err
        }
        defer file.Close()  // ❌ All defers wait until function returns!
        
        // Process file
        process(file)
    }
    // If paths has 1000 files, all 1000 files stay open until here!
    return nil
}
```

**Memory and FD Impact**:
- 1,000 files × 2 KB per file descriptor = 2 MB wasted
- 1,000 open FDs might hit OS limit (e.g., 1024)

### The Solutions

**Solution 1: Extract to Function**

```go
// ✅ CORRECT: Each iteration closes file
func processFiles(paths []string) error {
    for _, path := range paths {
        if err := processFile(path); err != nil {
            return err
        }
    }
    return nil
}

func processFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()  // ✅ Closes at end of THIS function
    
    return process(file)
}
```

**Solution 2: Explicit Close in Loop**

```go
// ✅ ACCEPTABLE: Explicit close with error handling
func processFiles(paths []string) error {
    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            return err
        }
        
        err = process(file)
        
        // Explicit close with error check
        if closeErr := file.Close(); closeErr != nil {
            if err == nil {
                err = closeErr
            }
        }
        
        if err != nil {
            return err
        }
    }
    return nil
}
```

**Solution 3: Anonymous Function**

```go
// ✅ CORRECT: Anonymous function for each iteration
func processFiles(paths []string) error {
    for _, path := range paths {
        err := func() error {
            file, err := os.Open(path)
            if err != nil {
                return err
            }
            defer file.Close()  // ✅ Closes at end of anonymous function
            
            return process(file)
        }()
        
        if err != nil {
            return err
        }
    }
    return nil
}
```

## Defer Performance Considerations

### Performance Cost

Defer has a small performance overhead:

```go
// Benchmark results (Go 1.21):
BenchmarkWithDefer-8       50000000    25.3 ns/op
BenchmarkWithoutDefer-8    100000000   10.1 ns/op
```

**Overhead**: ~15 ns per defer (negligible for I/O operations)

### When Performance Matters

```go
// Hot path in tight loop - avoid defer
func hotPath() {
    for i := 0; i < 1000000; i++ {
        mu.Lock()
        criticalSection()
        mu.Unlock()  // Faster than defer mu.Unlock()
    }
}

// Regular I/O - use defer for safety
func regularPath() error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()  // ✅ Defer overhead irrelevant compared to I/O
    
    return process(file)
}
```

**Rule of Thumb**: Use defer unless profiling proves it's a bottleneck (rare).

## Defer and Panic Recovery

### Recovering from Panics

```go
func safeDivide(a, b int) (result int, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v", r)
        }
    }()
    
    return a / b, nil  // Panics if b == 0
}

// Usage:
result, err := safeDivide(10, 0)
if err != nil {
    log.Println(err)  // "panic: runtime error: integer divide by zero"
}
```

### Resource Cleanup During Panic

```go
func guaranteedCleanup() {
    file, err := os.Open("data.txt")
    if err != nil {
        panic(err)
    }
    defer file.Close()  // ✅ Executes even if panic occurs below
    
    // Code that might panic
    riskyOperation(file)
    
    // file.Close() is guaranteed to run
}
```

### Re-Panicking

```go
func cleanupThenPanic() {
    resource := acquire()
    defer func() {
        resource.Close()  // Cleanup first
        
        if r := recover(); r != nil {
            log.Printf("Cleaning up after panic: %v", r)
            panic(r)  // Re-panic to propagate
        }
    }()
    
    doSomethingRisky()
}
```

## Advanced Defer Patterns

### Pattern 1: Transaction Rollback

```go
func transaction(db *sql.DB) (err error) {
    tx, err := db.Begin()
    if err != nil {
        return err
    }
    
    // Rollback if error, commit if success
    defer func() {
        if err != nil {
            tx.Rollback()
        } else {
            err = tx.Commit()
        }
    }()
    
    // Do transactional work
    if err := doWork1(tx); err != nil {
        return err  // Will trigger rollback
    }
    
    if err := doWork2(tx); err != nil {
        return err  // Will trigger rollback
    }
    
    return nil  // Will trigger commit
}
```

### Pattern 2: Timer/Cleanup with Cancellation

```go
func withTimeout(ctx context.Context) error {
    // Start timer
    timer := time.NewTimer(5 * time.Second)
    defer timer.Stop()  // ✅ Always stop timer
    
    select {
    case <-timer.C:
        return errors.New("timeout")
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Pattern 3: Metrics and Tracing

```go
func tracedOperation(name string) (err error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        status := "success"
        if err != nil {
            status = "error"
        }
        metrics.RecordOperation(name, duration, status)
    }()
    
    return performOperation()
}
```

### Pattern 4: Lock Release

```go
type Counter struct {
    mu    sync.Mutex
    value int
}

func (c *Counter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()  // ✅ Always unlocks, even if panic
    
    c.value++
    
    // Complex logic with multiple returns
    if c.value > 100 {
        c.value = 0
        return
    }
    
    // All paths unlock the mutex
}
```

## Common Defer Mistakes

### Mistake 1: Defer Before Error Check

```go
// ❌ PANIC if err != nil
file, err := os.Open(path)
defer file.Close()  // file is nil!
if err != nil {
    return err
}
```

### Mistake 2: Defer Pointer Method on Value

```go
type Resource struct{}

func (r *Resource) Close() error { return nil }

// ❌ SUBTLE BUG
func wrong() {
    var r Resource  // Note: not a pointer
    defer r.Close()  // Defers on a copy of r!
    
    // Modifying r doesn't affect the deferred copy
}

// ✅ CORRECT
func correct() {
    r := &Resource{}  // Pointer
    defer r.Close()  // Defers on the pointer
}
```

### Mistake 3: Defer in HTTP Handler (subtle)

```go
// ❌ WRONG: defer runs after response sent
func handler(w http.ResponseWriter, r *http.Request) {
    file, _ := os.Open("data.txt")
    defer file.Close()
    
    // Long-running operation
    time.Sleep(1 * time.Minute)
    
    // File stays open for 1 minute!
}

// ✅ BETTER: Close as soon as done
func handler(w http.ResponseWriter, r *http.Request) {
    file, _ := os.Open("data.txt")
    defer file.Close()
    
    data, _ := io.ReadAll(file)
    // Could close here explicitly if waiting is needed
    
    time.Sleep(1 * time.Minute)
    w.Write(data)
}
```

## Testing Defer Behavior

```go
func TestDeferOrder(t *testing.T) {
    var order []string
    
    func() {
        defer func() { order = append(order, "first") }()
        defer func() { order = append(order, "second") }()
        defer func() { order = append(order, "third") }()
    }()
    
    expected := []string{"third", "second", "first"}
    if !reflect.DeepEqual(order, expected) {
        t.Errorf("Expected %v, got %v", expected, order)
    }
}

func TestDeferWithPanic(t *testing.T) {
    cleaned := false
    
    func() {
        defer func() {
            cleaned = true
            recover()  // Prevent test failure
        }()
        
        panic("test panic")
    }()
    
    if !cleaned {
        t.Error("Defer should run even with panic")
    }
}
```

## Key Takeaways

1. **Defer runs at function return**, not block exit (LIFO order)

2. **Arguments evaluated immediately**, not when defer executes

3. **Runs even on panic**, but NOT on `os.Exit()`

4. **Never defer in loops** - extract to separate function

5. **Check Close() errors** - especially for writes

6. **Named returns allow defer to modify return values**

7. **Performance cost negligible** for I/O operations

8. **Always defer after error checks** to avoid panics

---

## References

- https://go.dev/blog/defer-panic-and-recover - Official Go blog
- https://go.dev/ref/spec#Defer_statements - Language specification
- https://github.com/golang/go/issues/43844 - Defer implementation details

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - When to use defer
- [Error Handling and Cleanup](05-error-handling-cleanup.md) - Defer with errors
- [Production Case Studies](07-production-case-studies.md) - Real defer bugs

