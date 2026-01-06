# Fix Strategies Matrix

**Comprehensive fix patterns organized by leak type.**

---

## Quick Reference Matrix

| Leak Type | Primary Fix | Secondary Fix | Prevention |
|-----------|-------------|---------------|------------|
| **Goroutine** | Context cancellation | Channel close | Always use context |
| **Long-Lived Ref** | LRU cache with limit | Weak references | Set max sizes |
| **Resource** | `defer Close()` | Explicit cleanup | Linters |
| **Defer Loop** | Extract to function | Manual close in loop | Code review |
| **Unbounded** | Worker pool | Semaphore | Rate limiting |

---

## Fix Patterns by Type

### 1. Goroutine Leaks

```go
// PROBLEM: Goroutine blocks forever
go func() {
    result := <-ch  // Never receives
}()

// FIX: Use context for cancellation
go func(ctx context.Context) {
    select {
    case result := <-ch:
        process(result)
    case <-ctx.Done():
        return
    }
}(ctx)
```

### 2. Long-Lived References

```go
// PROBLEM: Unbounded cache
cache[key] = largeValue

// FIX: LRU with size limit
cache := lru.New(1000)
cache.Add(key, value)
```

### 3. Resource Leaks

```go
// PROBLEM: File not closed
f, _ := os.Open(path)
// missing close

// FIX: Defer close immediately
f, err := os.Open(path)
if err != nil { return err }
defer f.Close()
```

### 4. Defer Issues

```go
// PROBLEM: Defer in loop
for _, f := range files {
    file, _ := os.Open(f)
    defer file.Close()  // Accumulates!
}

// FIX: Extract to function
for _, f := range files {
    processFile(f)  // Defer inside function
}
```

### 5. Unbounded Resources

```go
// PROBLEM: Unlimited goroutines
for task := range tasks {
    go process(task)
}

// FIX: Worker pool
pool := NewWorkerPool(100)
for task := range tasks {
    pool.Submit(task)
}
```

---

## Prevention Checklist

- [ ] All goroutines have exit conditions
- [ ] All caches have size limits
- [ ] All resources use `defer Close()`
- [ ] No `defer` inside loops
- [ ] All concurrent work uses pools/limits

