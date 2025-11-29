# Refactoring Patterns for Defer Issues

**Reading Time**: 22 minutes

---

## Introduction

Once you've identified a defer-in-loop issue, you need to refactor the code. This document presents several patterns with trade-offs to help you choose the right approach for your situation.

---

## Pattern 1: Function Extraction (Recommended)

Extract the loop body to a separate function where defer executes per-iteration.

### Before

```go
func processLogs(logPaths []string) error {
    for _, path := range logPaths {
        file, err := os.Open(path)
        if err != nil {
            return fmt.Errorf("open %s: %w", path, err)
        }
        defer file.Close()  // ❌ Accumulates
        
        if err := analyzeLogs(file); err != nil {
            return fmt.Errorf("analyze %s: %w", path, err)
        }
    }
    return nil
}
```

### After

```go
func processLogs(logPaths []string) error {
    for _, path := range logPaths {
        if err := processOneLog(path); err != nil {
            return err
        }
    }
    return nil
}

func processOneLog(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("open %s: %w", path, err)
    }
    defer file.Close()  // ✅ Executes at end of this function
    
    if err := analyzeLogs(file); err != nil {
        return fmt.Errorf("analyze %s: %w", path, err)
    }
    return nil
}
```

### Pros
- Clear separation of concerns
- Easy to test individual function
- Easy to add logging, metrics, etc.
- Most readable pattern

### Cons
- Adds a new function to the codebase
- May require passing additional context

### When to Use
- **Always prefer this pattern** unless:
  - The loop body is trivially simple (1-3 lines)
  - You need to share state across iterations in complex ways

---

## Pattern 2: Anonymous Function (Inline)

Wrap the loop body in an immediately-invoked anonymous function.

### Before

```go
func fetchAll(urls []string) ([][]byte, error) {
    var results [][]byte
    for _, url := range urls {
        resp, err := http.Get(url)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()  // ❌ Accumulates
        
        data, err := io.ReadAll(resp.Body)
        if err != nil {
            return nil, err
        }
        results = append(results, data)
    }
    return results, nil
}
```

### After

```go
func fetchAll(urls []string) ([][]byte, error) {
    var results [][]byte
    for _, url := range urls {
        data, err := func() ([]byte, error) {
            resp, err := http.Get(url)
            if err != nil {
                return nil, err
            }
            defer resp.Body.Close()  // ✅ Executes at end of anonymous function
            
            return io.ReadAll(resp.Body)
        }()
        
        if err != nil {
            return nil, err
        }
        results = append(results, data)
    }
    return results, nil
}
```

### Pros
- No new named function
- Logic stays inline
- Good for simple loop bodies

### Cons
- Harder to read with complex logic
- Can't easily test the inner logic
- Nested indentation

### When to Use
- Simple loop bodies (< 10 lines)
- When you want to keep logic visible in context
- Quick fixes without major refactoring

---

## Pattern 3: Explicit Close (No Defer)

For simple cases, explicitly close resources without using defer.

### Before

```go
func readConfigs(paths []string) ([]Config, error) {
    var configs []Config
    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            return nil, err
        }
        defer file.Close()  // ❌ Accumulates
        
        var cfg Config
        if err := json.NewDecoder(file).Decode(&cfg); err != nil {
            return nil, err
        }
        configs = append(configs, cfg)
    }
    return configs, nil
}
```

### After

```go
func readConfigs(paths []string) ([]Config, error) {
    var configs []Config
    for _, path := range paths {
        file, err := os.Open(path)
        if err != nil {
            return nil, err
        }
        
        var cfg Config
        decodeErr := json.NewDecoder(file).Decode(&cfg)
        closeErr := file.Close()  // ✅ Explicit close
        
        if decodeErr != nil {
            return nil, decodeErr
        }
        if closeErr != nil {
            return nil, fmt.Errorf("close %s: %w", path, closeErr)
        }
        configs = append(configs, cfg)
    }
    return configs, nil
}
```

### Pros
- No defer overhead
- Clear control flow
- Can handle Close() errors explicitly

### Cons
- Easy to forget close on error paths
- More verbose
- Doesn't handle panics (unlike defer)

### When to Use
- Simple happy-path code with no early returns
- When Close() errors matter
- Performance-critical inner loops

---

## Pattern 4: Helper Function with Callback

Create a helper that handles resource lifecycle.

### Implementation

```go
// Helper function that manages resource lifecycle
func withFile(path string, fn func(*os.File) error) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()
    return fn(file)
}

// Usage
func processLogs(paths []string) error {
    for _, path := range paths {
        err := withFile(path, func(f *os.File) error {
            return analyzeLogs(f)
        })
        if err != nil {
            return err
        }
    }
    return nil
}
```

### Pros
- Reusable pattern across codebase
- Enforces correct resource handling
- Clear intention

### Cons
- Callback style can be awkward
- Error handling can be complex
- Extra indentation

### When to Use
- Repeated resource patterns in your codebase
- When you want to enforce a standard pattern
- Library/framework code

---

## Pattern 5: Resource Pool/Manager

For high-frequency operations, use a resource pool.

### Implementation

```go
type FileProcessor struct {
    pool sync.Pool
}

func NewFileProcessor() *FileProcessor {
    return &FileProcessor{
        pool: sync.Pool{
            New: func() interface{} {
                return &bytes.Buffer{}
            },
        },
    }
}

func (fp *FileProcessor) Process(path string) error {
    buf := fp.pool.Get().(*bytes.Buffer)
    defer func() {
        buf.Reset()
        fp.pool.Put(buf)
    }()
    
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()
    
    // Use pooled buffer
    _, err = buf.ReadFrom(file)
    return err
}

// Usage
func processAll(paths []string) error {
    processor := NewFileProcessor()
    for _, path := range paths {
        if err := processor.Process(path); err != nil {
            return err
        }
    }
    return nil
}
```

### Pros
- Reduces allocations
- Amortizes setup cost
- Good for high-throughput scenarios

### Cons
- More complex setup
- Need to manage pool correctly
- May not be worth it for simple cases

### When to Use
- High-frequency operations (thousands per second)
- When profiling shows allocation overhead
- Shared resources across goroutines

---

## Comparison Matrix

| Pattern | Readability | Performance | Safety | Testability | Best For |
|---------|-------------|-------------|--------|-------------|----------|
| Function Extraction | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | Default choice |
| Anonymous Function | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ | Simple loops |
| Explicit Close | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ | No early returns |
| Callback Helper | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | Repeated patterns |
| Resource Pool | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ | High throughput |

---

## Refactoring HTTP Response Bodies

HTTP response bodies are a special case because they MUST be closed and read.

### Before

```go
func checkEndpoints(urls []string) ([]int, error) {
    var statuses []int
    for _, url := range urls {
        resp, err := http.Get(url)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()  // ❌ Accumulates
        
        statuses = append(statuses, resp.StatusCode)
    }
    return statuses, nil
}
```

### After (Function Extraction)

```go
func checkEndpoints(urls []string) ([]int, error) {
    var statuses []int
    for _, url := range urls {
        status, err := checkOne(url)
        if err != nil {
            return nil, err
        }
        statuses = append(statuses, status)
    }
    return statuses, nil
}

func checkOne(url string) (int, error) {
    resp, err := http.Get(url)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    // Drain body to allow connection reuse
    io.Copy(io.Discard, resp.Body)
    
    return resp.StatusCode, nil
}
```

### Special Note: Draining the Body

Even if you don't need the response body, you should drain it:

```go
func checkStatus(url string) (int, error) {
    resp, err := http.Get(url)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    // Drain body to allow HTTP connection reuse
    io.Copy(io.Discard, resp.Body)
    
    return resp.StatusCode, nil
}
```

Without draining, HTTP/1.1 connections won't be reused.

---

## Refactoring Database Rows

Database `Rows` must be closed to return the connection to the pool.

### Before

```go
func getAllUsers(db *sql.DB, departments []string) ([]User, error) {
    var users []User
    for _, dept := range departments {
        rows, err := db.Query("SELECT * FROM users WHERE dept = ?", dept)
        if err != nil {
            return nil, err
        }
        defer rows.Close()  // ❌ Accumulates
        
        for rows.Next() {
            var u User
            rows.Scan(&u.ID, &u.Name, &u.Dept)
            users = append(users, u)
        }
    }
    return users, nil
}
```

### After (Function Extraction)

```go
func getAllUsers(db *sql.DB, departments []string) ([]User, error) {
    var users []User
    for _, dept := range departments {
        deptUsers, err := getUsersInDept(db, dept)
        if err != nil {
            return nil, err
        }
        users = append(users, deptUsers...)
    }
    return users, nil
}

func getUsersInDept(db *sql.DB, dept string) ([]User, error) {
    rows, err := db.Query("SELECT * FROM users WHERE dept = ?", dept)
    if err != nil {
        return nil, err
    }
    defer rows.Close()  // ✅ Executes when this function returns
    
    var users []User
    for rows.Next() {
        var u User
        if err := rows.Scan(&u.ID, &u.Name, &u.Dept); err != nil {
            return nil, err
        }
        users = append(users, u)
    }
    
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return users, nil
}
```

---

## Refactoring Goroutine Cleanup

When spawning goroutines in a loop, ensure proper cleanup.

### Before

```go
func processParallel(items []Item) error {
    errCh := make(chan error, len(items))
    
    for _, item := range items {
        item := item  // Shadow for goroutine (pre-Go 1.22)
        go func() {
            conn := acquire(item)
            defer conn.Release()  // This defer is fine (per goroutine)
            
            if err := process(conn, item); err != nil {
                errCh <- err
            }
        }()
    }
    
    // Wait for completion...
}
```

This pattern is actually **fine** for defer because each goroutine has its own function scope. The issue here is not defer accumulation but potentially unbounded goroutine creation.

### Better: With Worker Pool

```go
func processParallel(items []Item, workers int) error {
    itemCh := make(chan Item)
    errCh := make(chan error, 1)
    
    var wg sync.WaitGroup
    for i := 0; i < workers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for item := range itemCh {
                if err := processOne(item); err != nil {
                    select {
                    case errCh <- err:
                    default:
                    }
                }
            }
        }()
    }
    
    for _, item := range items {
        itemCh <- item
    }
    close(itemCh)
    
    wg.Wait()
    close(errCh)
    
    return <-errCh
}

func processOne(item Item) error {
    conn := acquire(item)
    defer conn.Release()
    return process(conn, item)
}
```

---

## IDE Refactoring Support

### VS Code

1. Select the loop body
2. Right-click → "Refactor" → "Extract to function"
3. Name the new function

### GoLand

1. Select the loop body
2. Refactor → Extract → Method (Ctrl+Alt+M)
3. Configure parameters and return values

### Command Line (gofmt + manual)

```bash
# Ensure code is formatted
gofmt -w file.go

# Then manually extract the function
```

---

## Automated Detection

Add to your CI pipeline:

```yaml
# .github/workflows/lint.yml
- name: Run golangci-lint
  uses: golangci/golangci-lint-action@v3
  with:
    args: --enable=gocritic
```

```yaml
# .golangci.yml
linters:
  enable:
    - gocritic

linters-settings:
  gocritic:
    enabled-checks:
      - deferInLoop
```

---

## Summary

1. **Default to Function Extraction** — it's the cleanest, most testable pattern

2. **Use Anonymous Functions** for trivial loop bodies (< 5 lines)

3. **Use Explicit Close** when you don't have early returns and Close errors matter

4. **Create Helpers** when the same pattern repeats across your codebase

5. **Use Pools** for high-throughput scenarios identified by profiling

---

## Further Reading

- [Conceptual Explanation](01-conceptual-explanation.md) — Why defer behaves this way
- [Performance Impact](05-performance-impact.md) — Benchmarks of different patterns
- [Detection Methods](06-detection-methods.md) — How to find defer issues automatically



