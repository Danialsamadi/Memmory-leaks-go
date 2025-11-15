# Resource Lifecycle Patterns in Go

**Read Time**: ~15 minutes

**Prerequisites**: Basic Go syntax, understanding of defer

**Summary**: Learn the fundamental acquire-use-release pattern for managing OS resources in Go, why automatic resource management isn't provided, and how to implement proper resource lifecycle management.

---

## Introduction

Go's approach to resource management differs fundamentally from languages like Python, Java, or C#. While Go has garbage collection for memory, **OS resources (files, connections, handles) require explicit manual management**. This design choice trades convenience for control and performance.

## The Fundamental Pattern: Acquire-Use-Release

Every resource in Go follows a three-phase lifecycle:

```
┌──────────┐      ┌──────────┐      ┌──────────┐
│ ACQUIRE  │ ───> │   USE    │ ───> │ RELEASE  │
└──────────┘      └──────────┘      └──────────┘
    ↓                  ↓                  ↓
 os.Open()        Read/Write         file.Close()
 http.Get()       Process Data       resp.Body.Close()
 db.Query()       Scan Rows          rows.Close()
```

### The Golden Rule

**Between acquire and release, no code path should exit without cleanup.**

### Correct Implementation

```go
func processFile(path string) error {
    // Phase 1: ACQUIRE
    file, err := os.Open(path)
    if err != nil {
        return err  // No resource acquired, nothing to clean
    }
    
    // Phase 2: ENSURE RELEASE
    defer file.Close()  // Guaranteed cleanup
    
    // Phase 3: USE
    data := make([]byte, 1024)
    n, err := file.Read(data)
    if err != nil {
        return err  // defer ensures Close() is called
    }
    
    return processData(data[:n])
}
```

### Incorrect Implementation (Leak!)

```go
func processFileBadly(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    // Missing defer file.Close()!
    
    data := make([]byte, 1024)
    n, err := file.Read(data)
    if err != nil {
        return err  // ❌ File never closed on error path!
    }
    
    return processData(data[:n])  // ❌ File not closed on success either!
}
```

## Why Go Doesn't Have Automatic Resource Management

Many developers coming from other languages ask: "Why doesn't Go have Python's `with` statement or C#'s `using` keyword?"

### Design Philosophy

Go's creators made an intentional tradeoff:[^1][^2]

**Explicit > Implicit**
- Developers have precise control over resource lifetime
- No hidden behavior or "magic"
- Clear code path visibility

**Performance > Convenience**
- No runtime overhead for tracking resource relationships
- No finalizers blocking GC cycles
- Predictable execution (defer runs at function return, not GC time)

**Simplicity > Features**
- One clear pattern: defer
- No multiple ways to accomplish the same thing
- Easier to audit code for correctness

### Comparison with Other Languages

| Language | Mechanism | When Cleanup Runs | Guaranteed? |
|----------|-----------|-------------------|-------------|
| **Go** | `defer` | Function return | ✅ Yes (except `os.Exit()`) |
| **Python** | `with` | Block exit | ✅ Yes |
| **C#** | `using` | Block exit | ✅ Yes |
| **Java** | try-with-resources | Try block exit | ✅ Yes |
| **C++** | RAII/destructors | Scope exit | ✅ Yes |
| **JavaScript** | Manual | Manual | ❌ No (depends on discipline) |

**Go's Position**: Between fully automatic (C++ RAII) and fully manual (JavaScript). You must remember `defer`, but once written, cleanup is guaranteed.

## Common Resource Types and Their Patterns

### 1. File Handles

```go
file, err := os.Open(path)
if err != nil {
    return err
}
defer file.Close()
```

**OS Limits**: 
- Linux default: 1,024 per process (`ulimit -n`)
- macOS default: 256 per process
- Can be increased but not unlimited

### 2. HTTP Response Bodies

```go
resp, err := http.Get(url)
if err != nil {
    return err
}
defer resp.Body.Close()
```

**Why it matters**: HTTP client maintains a connection pool. Unclosed bodies prevent connection reuse, leading to connection exhaustion.

### 3. Database Rows

```go
rows, err := db.Query("SELECT * FROM users")
if err != nil {
    return err
}
defer rows.Close()

for rows.Next() {
    // scan rows
}
```

**Why it matters**: Database connections are pooled. Unclosed `Rows` objects hold connections, preventing reuse.

### 4. Network Listeners

```go
listener, err := net.Listen("tcp", ":8080")
if err != nil {
    return err
}
defer listener.Close()
```

**Why it matters**: Ports are a scarce resource. Unclosed listeners prevent port reuse until OS timeout.

### 5. Timers and Tickers

```go
ticker := time.NewTicker(1 * time.Second)
defer ticker.Stop()
```

**Why it matters**: Timers hold goroutines and memory. Unstopped tickers leak both.

## The `defer` Mechanism Deep Dive

### How `defer` Works

```go
func example() {
    defer fmt.Println("1")
    defer fmt.Println("2")
    defer fmt.Println("3")
    fmt.Println("4")
}
// Output: 4, 3, 2, 1 (LIFO: Last In, First Out)
```

### Defer Evaluation Rules

**Rule 1: Arguments Evaluated Immediately**

```go
func incorrectDefer() {
    i := 0
    defer fmt.Println(i)  // Evaluates i=0 NOW
    i++
    // Prints: 0 (not 1!)
}

func correctDefer() {
    i := 0
    defer func() {
        fmt.Println(i)  // Captures i by reference
    }()
    i++
    // Prints: 1 ✅
}
```

**Rule 2: Defer Executes Even on Panic**

```go
func recoverFromPanic() {
    file, _ := os.Open("data.txt")
    defer file.Close()  // ✅ Called even if panic occurs
    
    // Simulate panic
    panic("something went wrong")
    
    // defer still executes!
}
```

**Rule 3: Defer Does NOT Execute on `os.Exit()`**

```go
func badExit() {
    file, _ := os.Open("data.txt")
    defer file.Close()  // ❌ Never called!
    
    os.Exit(1)  // Immediate termination
}
```

## Common Pitfalls and Solutions

### Pitfall 1: Defer Before Error Check

```go
// ❌ WRONG: defer before checking error
file, err := os.Open(path)
defer file.Close()  // PANIC if err != nil (file is nil)
if err != nil {
    return err
}

// ✅ CORRECT: defer after checking error
file, err := os.Open(path)
if err != nil {
    return err
}
defer file.Close()  // Safe: file is valid
```

### Pitfall 2: Defer in Loop

```go
// ❌ WRONG: defer in loop accumulates
func processFiles(paths []string) error {
    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            return err
        }
        defer file.Close()  // All defers execute at function end!
        // If processing 1000 files, all 1000 stay open until return
        process(file)
    }
    return nil
}

// ✅ CORRECT: Extract to separate function
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
    defer file.Close()  // Closes at end of THIS iteration
    return process(file)
}
```

### Pitfall 3: Ignoring Close Errors

```go
// ❌ WRONG: Ignoring Close() error
defer file.Close()

// ✅ BETTER: Log Close() error
defer func() {
    if err := file.Close(); err != nil {
        log.Printf("failed to close file: %v", err)
    }
}()

// ✅ BEST: Return Close() error (for write operations)
func writeData(path string, data []byte) (err error) {
    file, err := os.Create(path)
    if err != nil {
        return err
    }
    defer func() {
        if closeErr := file.Close(); closeErr != nil && err == nil {
            err = closeErr  // Return close error if no other error
        }
    }()
    
    _, err = file.Write(data)
    return err
}
```

### Pitfall 4: Early Return Without Defer

```go
// ❌ WRONG: Multiple return points without cleanup
func complexLogic(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    
    if someCondition() {
        file.Close()
        return errors.New("condition failed")
    }
    
    if anotherCondition() {
        file.Close()  // Easy to forget!
        return errors.New("another condition failed")
    }
    
    file.Close()
    return nil
}

// ✅ CORRECT: Single defer, all paths handled
func complexLogic(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()  // Handles all return paths!
    
    if someCondition() {
        return errors.New("condition failed")
    }
    
    if anotherCondition() {
        return errors.New("another condition failed")
    }
    
    return nil
}
```

## Multi-Resource Cleanup Patterns

### Pattern 1: Nested Defers

```go
func copyFile(src, dst string) error {
    source, err := os.Open(src)
    if err != nil {
        return err
    }
    defer source.Close()
    
    destination, err := os.Create(dst)
    if err != nil {
        return err  // source.Close() still called
    }
    defer destination.Close()
    
    _, err = io.Copy(destination, source)
    return err  // Both files closed in LIFO order
}
```

### Pattern 2: Cleanup on Error

```go
func setupResources() (err error) {
    // Acquire resource 1
    r1, err := acquireResource1()
    if err != nil {
        return err
    }
    defer func() {
        if err != nil {
            r1.Release()  // Only release if function returns error
        }
    }()
    
    // Acquire resource 2
    r2, err := acquireResource2()
    if err != nil {
        return err  // r1 released by defer
    }
    defer func() {
        if err != nil {
            r2.Release()
        }
    }()
    
    // If we get here, both resources acquired successfully
    return nil  // err == nil, so defers do nothing
}
```

### Pattern 3: Resource Aggregator

```go
type Resources struct {
    files []*os.File
    conns []net.Conn
}

func (r *Resources) AddFile(f *os.File) {
    r.files = append(r.files, f)
}

func (r *Resources) AddConn(c net.Conn) {
    r.conns = append(r.conns, c)
}

func (r *Resources) Close() error {
    var errs []error
    
    for _, f := range r.files {
        if err := f.Close(); err != nil {
            errs = append(errs, err)
        }
    }
    
    for _, c := range r.conns {
        if err := c.Close(); err != nil {
            errs = append(errs, err)
        }
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("close errors: %v", errs)
    }
    return nil
}

// Usage:
func processMultiple() error {
    resources := &Resources{}
    defer resources.Close()
    
    f1, _ := os.Open("file1.txt")
    resources.AddFile(f1)
    
    f2, _ := os.Open("file2.txt")
    resources.AddFile(f2)
    
    // All resources closed automatically
    return nil
}
```

## Testing Resource Management

### Using `goleak` to Detect Leaks

```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)  // Fails if goroutines leak
}

func TestFileProcessing(t *testing.T) {
    defer goleak.VerifyNone(t)  // Fails if this test leaks
    
    // Test code here
}
```

### Forcing Low Resource Limits

```bash
# Test with low file descriptor limit
ulimit -n 128 && go test ./...

# Run in Docker with limits
docker run --ulimit nofile=128:128 myapp
```

## Key Takeaways

1. **Every resource acquisition must be paired with `defer` cleanup** immediately after error checking

2. **Go intentionally doesn't provide automatic resource management** for performance and clarity

3. **Defer is evaluated at function return**, not block scope like other languages

4. **Never use defer inside loops** - extract to separate function

5. **Always check errors from Close()** - especially for write operations

6. **Test resource management** with low limits and leak detection tools

7. **Multi-resource cleanup requires careful ordering** - use nested defers or resource aggregators

---

## References

[^1]: https://go.dev/blog/defer-panic-and-recover - Official Go blog on defer mechanics
[^2]: https://golang.org/doc/effective_go#defer - Effective Go on defer patterns

## Further Reading

- [File Descriptor Internals](02-file-descriptor-internals.md) - OS-level resource limits
- [Defer Mechanics Deep Dive](04-defer-mechanics.md) - Advanced defer patterns
- [Error Handling and Cleanup](05-error-handling-cleanup.md) - Cleanup on error paths

