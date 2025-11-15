# Error Handling and Resource Cleanup

**Read Time**: ~17 minutes

**Prerequisites**: Understanding of Go error handling and defer

**Summary**: Learn patterns for ensuring resource cleanup on error paths, handling multiple errors, and combining error handling with defer for robust code.

---

## Introduction

Proper error handling and resource cleanup must work together seamlessly. A common source of resource leaks is failure to clean up resources when errors occur. This guide explores patterns that ensure resources are always released, regardless of error conditions.

## The Problem: Error Paths and Resource Leaks

### Common Leak Pattern

```go
// ❌ LEAKS: Resource not cleaned on error
func processData(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    
    data, err := readData(file)
    if err != nil {
        return err  // ❌ File never closed!
    }
    
    err = validateData(data)
    if err != nil {
        return err  // ❌ File never closed!
    }
    
    file.Close()  // Only reached on success path
    return nil
}
```

**Problem**: Early returns bypass cleanup, leaking the file descriptor.

### The defer Solution

```go
// ✅ CORRECT: defer ensures cleanup on all paths
func processData(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()  // ✅ Runs on all return paths
    
    data, err := readData(file)
    if err != nil {
        return err  // File closed by defer
    }
    
    err = validateData(data)
    if err != nil {
        return err  // File closed by defer
    }
    
    return nil  // File closed by defer
}
```

## Handling Close Errors

### The Dilemma

Many Go resources return errors from their `Close()` methods:

```go
type Closer interface {
    Close() error
}
```

Questions:
1. Should we check the error from `Close()`?
2. If both operation and close fail, which error should we return?
3. How do we handle multiple cleanup errors?

### Pattern 1: Ignore Close Error (Read-Only Operations)

```go
// ✅ ACCEPTABLE: Ignoring Close() for read-only files
func readFile(path string) ([]byte, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()  // Ignore error - we're only reading
    
    return io.ReadAll(file)
}
```

**Rationale**: For read-only operations, `Close()` errors are usually benign (fd limit temporarily higher).

### Pattern 2: Log Close Error

```go
// ✅ GOOD: Logging Close() errors
func processFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer func() {
        if err := file.Close(); err != nil {
            log.Printf("failed to close %s: %v", path, err)
        }
    }()
    
    return process(file)
}
```

**Rationale**: Logging provides visibility without complicating error handling.

### Pattern 3: Return Close Error (Write Operations)

```go
// ✅ BEST: Returning Close() error for write operations
func writeFile(path string, data []byte) (err error) {
    file, err := os.Create(path)
    if err != nil {
        return err
    }
    defer func() {
        closeErr := file.Close()
        if err == nil {
            err = closeErr  // Return close error if no other error
        }
    }()
    
    _, err = file.Write(data)
    return err
}
```

**Rationale**: For writes, `Close()` may flush buffers and return I/O errors. These MUST be checked.

## Multi-Error Handling

### Problem: Multiple Potential Errors

```go
// What if both Write and Close fail?
func writeData(path string, data []byte) error {
    file, _ := os.Create(path)
    defer file.Close()
    
    _, writeErr := file.Write(data)
    // closeErr from defer - how to return both?
    return writeErr
}
```

### Solution 1: Aggregate Errors

```go
// ✅ Collect all errors
func writeData(path string, data []byte) error {
    file, err := os.Create(path)
    if err != nil {
        return err
    }
    
    var errs []error
    
    if _, err := file.Write(data); err != nil {
        errs = append(errs, err)
    }
    
    if err := file.Close(); err != nil {
        errs = append(errs, err)
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("multiple errors: %v", errs)
    }
    
    return nil
}
```

### Solution 2: Prioritize Write Error

```go
// ✅ Return write error, log close error
func writeData(path string, data []byte) (err error) {
    file, err := os.Create(path)
    if err != nil {
        return err
    }
    defer func() {
        if closeErr := file.Close(); closeErr != nil {
            if err == nil {
                err = closeErr  // Only return if no write error
            } else {
                log.Printf("also failed to close: %v", closeErr)
            }
        }
    }()
    
    _, err = file.Write(data)
    return err
}
```

### Solution 3: Go 1.20+ Multi-Error

```go
// ✅ Using errors.Join (Go 1.20+)
func writeData(path string, data []byte) (err error) {
    file, err := os.Create(path)
    if err != nil {
        return err
    }
    defer func() {
        if closeErr := file.Close(); closeErr != nil {
            err = errors.Join(err, closeErr)  // Combine errors
        }
    }()
    
    _, err = file.Write(data)
    return err
}
```

## Multi-Resource Cleanup Patterns

### Pattern 1: Sequential Cleanup with Nested Defers

```go
func complexOperation() error {
    // Acquire resource 1
    r1, err := acquireResource1()
    if err != nil {
        return err
    }
    defer r1.Close()  // Always cleans up r1
    
    // Acquire resource 2
    r2, err := acquireResource2()
    if err != nil {
        return err  // r1 cleaned by its defer
    }
    defer r2.Close()  // Always cleans up r2
    
    // Acquire resource 3
    r3, err := acquireResource3()
    if err != nil {
        return err  // r2 and r1 cleaned by their defers
    }
    defer r3.Close()  // Always cleans up r3
    
    // Use all resources
    return useResources(r1, r2, r3)
    
    // Cleanup order: r3, r2, r1 (LIFO)
}
```

**Advantage**: Clean, automatic cleanup in correct order.

### Pattern 2: Cleanup-on-Error-Only

```go
func setupResources() (r1, r2 *Resource, err error) {
    // Acquire resource 1
    r1, err = acquireResource1()
    if err != nil {
        return nil, nil, err
    }
    
    // Cleanup r1 only if subsequent operations fail
    defer func() {
        if err != nil {
            r1.Close()
        }
    }()
    
    // Acquire resource 2
    r2, err = acquireResource2()
    if err != nil {
        return nil, nil, err  // r1 cleaned by defer
    }
    
    // Success: don't clean up (caller's responsibility)
    return r1, r2, nil
}

// Caller must clean up returned resources
func caller() error {
    r1, r2, err := setupResources()
    if err != nil {
        return err
    }
    defer r1.Close()
    defer r2.Close()
    
    return use(r1, r2)
}
```

**Use Case**: Factory functions that return resources to caller.

### Pattern 3: Resource Manager

```go
type ResourceManager struct {
    resources []io.Closer
    mu        sync.Mutex
}

func (rm *ResourceManager) Add(r io.Closer) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    rm.resources = append(rm.resources, r)
}

func (rm *ResourceManager) CloseAll() error {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    
    var errs []error
    
    // Close in reverse order (LIFO)
    for i := len(rm.resources) - 1; i >= 0; i-- {
        if err := rm.resources[i].Close(); err != nil {
            errs = append(errs, err)
        }
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("cleanup errors: %v", errs)
    }
    return nil
}

// Usage:
func processMany() error {
    rm := &ResourceManager{}
    defer rm.CloseAll()
    
    f1, _ := os.Open("file1.txt")
    rm.Add(f1)
    
    f2, _ := os.Open("file2.txt")
    rm.Add(f2)
    
    conn, _ := net.Dial("tcp", "example.com:80")
    rm.Add(conn)
    
    // All resources cleaned up automatically
    return process(f1, f2, conn)
}
```

**Advantage**: Centralizes cleanup logic, handles multiple resources elegantly.

## Error Wrapping and Context

### Adding Context to Errors

```go
func processFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("opening %s: %w", path, err)
    }
    defer file.Close()
    
    data, err := io.ReadAll(file)
    if err != nil {
        return fmt.Errorf("reading %s: %w", path, err)
    }
    
    if err := validate(data); err != nil {
        return fmt.Errorf("validating %s: %w", path, err)
    }
    
    return nil
}

// Error chain example:
// validating /data/users.json: invalid format: unexpected EOF
```

### Preserving Error Type

```go
var ErrInvalidData = errors.New("invalid data")

func processFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("opening %s: %w", path, err)
    }
    defer file.Close()
    
    data, err := io.ReadAll(file)
    if err != nil {
        return fmt.Errorf("reading %s: %w", path, err)
    }
    
    if !valid(data) {
        // Wrap custom error
        return fmt.Errorf("validating %s: %w", path, ErrInvalidData)
    }
    
    return nil
}

// Caller can check:
if errors.Is(err, ErrInvalidData) {
    // Handle invalid data specifically
}
```

## Transaction-Style Error Handling

### Database Transactions

```go
func updateUsers(db *sql.DB, users []User) (err error) {
    tx, err := db.Begin()
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }
    
    // Rollback on error, commit on success
    defer func() {
        if err != nil {
            if rbErr := tx.Rollback(); rbErr != nil {
                err = fmt.Errorf("%w; rollback failed: %v", err, rbErr)
            }
        } else {
            err = tx.Commit()
        }
    }()
    
    // Perform updates
    for _, user := range users {
        if err := updateUser(tx, user); err != nil {
            return fmt.Errorf("updating user %d: %w", user.ID, err)
        }
    }
    
    return nil  // Triggers commit
}
```

### File Atomic Write

```go
func atomicWrite(path string, data []byte) (err error) {
    // Write to temporary file first
    tmpPath := path + ".tmp"
    
    file, err := os.Create(tmpPath)
    if err != nil {
        return fmt.Errorf("creating temp file: %w", err)
    }
    
    // Remove temp file on error
    defer func() {
        file.Close()
        if err != nil {
            os.Remove(tmpPath)  // Clean up temp file
        }
    }()
    
    // Write data
    if _, err = file.Write(data); err != nil {
        return fmt.Errorf("writing data: %w", err)
    }
    
    // Sync to disk
    if err = file.Sync(); err != nil {
        return fmt.Errorf("syncing: %w", err)
    }
    
    // Close before rename
    if err = file.Close(); err != nil {
        return fmt.Errorf("closing: %w", err)
    }
    
    // Atomic rename
    if err = os.Rename(tmpPath, path); err != nil {
        return fmt.Errorf("renaming: %w", err)
    }
    
    return nil  // Success, temp file becomes permanent
}
```

## Panic Recovery with Cleanup

### Pattern: Recover and Cleanup

```go
func safeDivide(a, b int) (result int, err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic recovered: %v", r)
        }
    }()
    
    return a / b, nil  // Panics on divide by zero
}
```

### Pattern: Resource Cleanup Before Re-Panic

```go
func processWithRecovery(path string) (err error) {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    
    defer func() {
        // Always close file
        file.Close()
        
        // Then handle panic
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v", r)
            // Could re-panic here if needed:
            // panic(r)
        }
    }()
    
    // Potentially panicking code
    return riskyOperation(file)
}
```

## Testing Error Paths

### Test All Error Paths

```go
func TestErrorPaths(t *testing.T) {
    tests := []struct {
        name    string
        path    string
        wantErr bool
    }{
        {"success", "/tmp/test.txt", false},
        {"nonexistent", "/nonexistent", true},
        {"permission denied", "/root/secret", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := processFile(tt.path)
            if (err != nil) != tt.wantErr {
                t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
            }
        })
    }
}
```

### Test Resource Cleanup

```go
func TestResourceCleanup(t *testing.T) {
    // Track open file descriptors
    before := countOpenFDs()
    
    // Operation that should clean up
    err := processFile("/tmp/test.txt")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    
    // Verify cleanup
    after := countOpenFDs()
    if after != before {
        t.Errorf("file descriptor leak: before=%d, after=%d", before, after)
    }
}

func countOpenFDs() int {
    // On Linux: count files in /proc/self/fd
    entries, _ := os.ReadDir("/proc/self/fd")
    return len(entries)
}
```

### Use goleak for Goroutine Leaks

```go
import "go.uber.org/goleak"

func TestNoLeaks(t *testing.T) {
    defer goleak.VerifyNone(t)  // Fails if goroutines leak
    
    // Test code that shouldn't leak goroutines
    processData()
}
```

## Best Practices Summary

### For Resource Cleanup

1. ✅ **Always use defer after error check**
2. ✅ **Check Close() errors for write operations**
3. ✅ **Log Close() errors for read operations**
4. ✅ **Use named returns to modify error in defer**
5. ✅ **Clean up in LIFO order (nested defers)**

### For Error Handling

1. ✅ **Wrap errors with context** (`fmt.Errorf("context: %w", err)`)
2. ✅ **Use custom error types** when callers need to distinguish
3. ✅ **Test all error paths**, not just happy path
4. ✅ **Aggregate multiple errors** when appropriate
5. ✅ **Don't ignore errors** - at minimum, log them

### For Multi-Resource Operations

1. ✅ **Use nested defers** for automatic LIFO cleanup
2. ✅ **Consider Resource Manager** for many resources
3. ✅ **Implement transaction pattern** for atomic operations
4. ✅ **Clean up on error** but let caller own resources on success

## Common Mistakes to Avoid

### Mistake 1: Silent Error Ignoring

```go
// ❌ WRONG: Ignoring all errors
defer file.Close()

// ✅ CORRECT: At least log it
defer func() {
    if err := file.Close(); err != nil {
        log.Printf("close failed: %v", err)
    }
}()
```

### Mistake 2: Leaking on Early Return

```go
// ❌ WRONG: Leak on validation failure
func process(path string) error {
    file, _ := os.Open(path)
    
    if !valid(path) {
        return errors.New("invalid")  // Leak!
    }
    
    defer file.Close()
    return processFile(file)
}

// ✅ CORRECT: defer immediately
func process(path string) error {
    file, _ := os.Open(path)
    defer file.Close()
    
    if !valid(path) {
        return errors.New("invalid")  // Closed by defer
    }
    
    return processFile(file)
}
```

### Mistake 3: Forgetting Cleanup on Panic

```go
// ❌ WRONG: No cleanup if panic
func risky(path string) {
    file, _ := os.Open(path)
    mightPanic(file)
    file.Close()  // Never reached if panic!
}

// ✅ CORRECT: defer ensures cleanup even on panic
func risky(path string) {
    file, _ := os.Open(path)
    defer file.Close()
    mightPanic(file)  // Cleaned up even if panics
}
```

## Key Takeaways

1. **Defer immediately after successful resource acquisition** to ensure cleanup on all paths

2. **Check Close() errors for write operations** - they can indicate data loss

3. **Use named returns** to allow defer to modify the error

4. **Wrap errors with context** using `%w` to preserve error chain

5. **Test error paths explicitly** - they're often where leaks hide

6. **Multiple resources clean up in LIFO order** with nested defers

7. **Panic recovery should still clean up resources** before re-panicking

---

## References

- https://go.dev/blog/error-handling-and-go - Official error handling guide
- https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully
- https://pkg.go.dev/errors - errors package documentation

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - Basic defer patterns
- [Defer Mechanics](04-defer-mechanics.md) - How defer works
- [Production Case Studies](07-production-case-studies.md) - Real error handling bugs

