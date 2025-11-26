# Loop Mechanics and Closure Capture

**Reading Time**: 18 minutes

---

## Introduction

Understanding how Go loops work with closures is essential to avoid the closure capture bug — the second most common defer issue. This document explains loop variable semantics, closure behavior, and the changes introduced in Go 1.22.

---

## Loop Variable Semantics

### Pre-Go 1.22 Behavior

Before Go 1.22, the loop variable in a `for` loop was **reused across iterations**:

```go
// Go 1.21 and earlier
for i := 0; i < 3; i++ {
    // 'i' is the SAME variable in each iteration
    // Its memory address doesn't change
    fmt.Printf("i address: %p\n", &i)
}
// Output:
// i address: 0xc0000b4008
// i address: 0xc0000b4008  (same!)
// i address: 0xc0000b4008  (same!)
```

This created the infamous "closure capture" problem:

```go
// BUGGY: Pre-Go 1.22
funcs := []func(){}
for i := 0; i < 3; i++ {
    funcs = append(funcs, func() {
        fmt.Println(i)  // Captures 'i' by reference
    })
}
for _, f := range funcs {
    f()  // All print 3!
}
```

### Go 1.22+ Behavior

Go 1.22 introduced **per-iteration loop variables**:

```go
// Go 1.22 and later
for i := 0; i < 3; i++ {
    // 'i' is a NEW variable in each iteration
    fmt.Printf("i address: %p\n", &i)
}
// Output:
// i address: 0xc0000b4008
// i address: 0xc0000b4010  (different!)
// i address: 0xc0000b4018  (different!)
```

```go
// FIXED: Go 1.22+
funcs := []func(){}
for i := 0; i < 3; i++ {
    funcs = append(funcs, func() {
        fmt.Println(i)  // Each closure captures a different 'i'
    })
}
for _, f := range funcs {
    f()  // Prints 0, 1, 2 correctly
}
```

---

## How Closures Capture Variables

### By Reference, Not By Value

In Go (all versions), closures capture variables **by reference**:

```go
func main() {
    x := 10
    f := func() {
        fmt.Println(x)  // Captures reference to 'x'
    }
    x = 20
    f()  // Prints 20, not 10!
}
```

The closure `f` doesn't copy `x`; it stores a reference to `x`'s memory location.

### The Pre-1.22 Loop Problem

```go
// Pre-Go 1.22
for i := 0; i < 3; i++ {
    defer func() {
        fmt.Println(i)  // All closures share same 'i'
    }()
}
// When defers execute (LIFO):
// All see i=3 (value after loop exits)
```

**Timeline**:
```
i=0: defer closure1 (captures &i, i=0 now)
i=1: defer closure2 (captures &i, i=1 now, but same &i)
i=2: defer closure3 (captures &i, i=2 now, but same &i)
Loop exits: i=3
closure3 executes: reads *(&i) = 3
closure2 executes: reads *(&i) = 3
closure1 executes: reads *(&i) = 3
```

---

## The Range Loop Subtlety

### For-Range with Value

```go
items := []string{"a", "b", "c"}
for _, item := range items {
    defer func() {
        fmt.Println(item)
    }()
}
// Pre-Go 1.22: prints "c" three times
// Go 1.22+: prints "c", "b", "a" (LIFO order, correct values)
```

### For-Range with Index

```go
items := []string{"a", "b", "c"}
for i := range items {
    defer func() {
        fmt.Println(items[i])
    }()
}
// Pre-Go 1.22: prints items[3] → panic (index out of range)!
// Go 1.22+: prints "c", "b", "a" correctly
```

---

## Classic Fix Patterns (Pre-Go 1.22)

Even with Go 1.22+, understanding these patterns is valuable for maintaining legacy code.

### Fix 1: Shadow the Variable

```go
for i := 0; i < 3; i++ {
    i := i  // Create new 'i' in this iteration's scope
    defer func() {
        fmt.Println(i)  // Captures the shadowed 'i'
    }()
}
// Output: 2, 1, 0 (correct, LIFO order)
```

**How it works**:
- `i := i` creates a new variable in the loop body's scope
- This new variable is initialized with the current iteration's value
- The closure captures this new variable, not the loop variable

### Fix 2: Pass as Argument

```go
for i := 0; i < 3; i++ {
    defer func(n int) {
        fmt.Println(n)  // Uses function parameter
    }(i)  // 'i' evaluated and passed at defer time
}
// Output: 2, 1, 0 (correct, LIFO order)
```

**How it works**:
- `(i)` is evaluated when `defer` is executed
- The current value is passed as argument to the closure
- The closure uses its parameter, not an outer variable

### Fix 3: Extract to Function

```go
for i := 0; i < 3; i++ {
    deferPrint(i)
}

func deferPrint(n int) {
    defer func() {
        fmt.Println(n)  // Captures function parameter
    }()
}
// Output: 0, 1, 2 (each defer executes in deferPrint)
```

**How it works**:
- Function parameters are new variables
- Each call to `deferPrint` has its own `n`
- The defer executes when `deferPrint` returns

---

## When Go 1.22+ Still Has Issues

Go 1.22's fix helps with **closure capture**, but doesn't fix **defer accumulation**:

```go
// Go 1.22+ - closure capture is fixed
// BUT defer still accumulates!
func process() {
    for i := 0; i < 1000; i++ {
        file := openFile(i)
        defer file.Close()  // Still 1000 pending defers!
    }
    // All 1000 files still open here
}
```

**Go 1.22 fixes**: Closure captures correct values
**Go 1.22 does NOT fix**: Resource accumulation

---

## Detailed Memory Model

### Pre-Go 1.22 Loop Variable

```
┌─────────────────────────────────────┐
│           Stack Frame              │
├─────────────────────────────────────┤
│  i: int at address 0x100           │
│     Iteration 0: value = 0         │
│     Iteration 1: value = 1         │
│     Iteration 2: value = 2         │
│     After loop:  value = 3         │
├─────────────────────────────────────┤
│  Closures all capture 0x100        │
│  When executed, read value 3       │
└─────────────────────────────────────┘
```

### Go 1.22+ Loop Variable

```
┌─────────────────────────────────────┐
│           Stack Frame              │
├─────────────────────────────────────┤
│  Iteration 0:                      │
│    i: int at address 0x100 = 0     │
│    Closure captures 0x100 → 0      │
├─────────────────────────────────────┤
│  Iteration 1:                      │
│    i: int at address 0x108 = 1     │ (new address!)
│    Closure captures 0x108 → 1      │
├─────────────────────────────────────┤
│  Iteration 2:                      │
│    i: int at address 0x110 = 2     │ (new address!)
│    Closure captures 0x110 → 2      │
└─────────────────────────────────────┘
```

---

## Range Loop Internals

### How For-Range Works

A range loop like:

```go
for i, v := range slice {
    // body
}
```

Is roughly equivalent to:

```go
for i := 0; i < len(slice); i++ {
    v := slice[i]
    // body
}
```

### Pre-Go 1.22 Range Issue

```go
// Pre-Go 1.22
slice := []string{"a", "b", "c"}
for _, v := range slice {
    // 'v' is reused! Same address each iteration
    defer func() { fmt.Println(v) }()
}
// All print "c"
```

### Go 1.22+ Range Fix

```go
// Go 1.22+
slice := []string{"a", "b", "c"}
for _, v := range slice {
    // 'v' is new each iteration! Different addresses
    defer func() { fmt.Println(v) }()
}
// Prints "c", "b", "a" (LIFO, correct values)
```

---

## Common Patterns with Closures and Defer

### Pattern: Worker with Cleanup

```go
// ❌ WRONG: Closure captures reused variable (pre-1.22)
// Also wrong: Resource accumulation (all versions)
func processAll(jobs []Job) {
    for _, job := range jobs {
        resource := acquire(job)
        defer func() {
            release(resource)  // Wrong resource in pre-1.22
        }()
        doWork(resource)
    }
}

// ✅ CORRECT: Extract to function
func processAll(jobs []Job) {
    for _, job := range jobs {
        processOne(job)
    }
}

func processOne(job Job) {
    resource := acquire(job)
    defer release(resource)  // Correct capture, no accumulation
    doWork(resource)
}
```

### Pattern: Parallel Workers

```go
// ❌ WRONG: All goroutines see same 'job' (pre-1.22)
func runParallel(jobs []Job) {
    var wg sync.WaitGroup
    for _, job := range jobs {
        wg.Add(1)
        go func() {
            defer wg.Done()
            process(job)  // BUG: 'job' captured by reference
        }()
    }
    wg.Wait()
}

// ✅ CORRECT: Pass as argument
func runParallel(jobs []Job) {
    var wg sync.WaitGroup
    for _, job := range jobs {
        wg.Add(1)
        go func(j Job) {  // Parameter creates new variable
            defer wg.Done()
            process(j)  // Uses function parameter
        }(job)  // Pass current 'job' value
    }
    wg.Wait()
}
```

---

## Checking Your Go Version

To see if you have the Go 1.22+ fix:

```bash
go version
# go version go1.22.0 darwin/arm64
```

Or check in code:

```go
import "runtime"

func main() {
    fmt.Println(runtime.Version())  // e.g., "go1.22.0"
}
```

---

## Migration Guide

### From Pre-1.22 to 1.22+

**Code that can be simplified**:

```go
// Old (pre-1.22)
for _, v := range items {
    v := v  // Shadowing
    defer func() { use(v) }()
}

// New (Go 1.22+) - shadowing no longer needed for closure capture
for _, v := range items {
    defer func() { use(v) }()  // Safe for capture
}
// BUT: Still need to fix accumulation!
```

**Code that still needs fixes**:

```go
// Still wrong in Go 1.22+
for _, item := range items {
    f := open(item)
    defer f.Close()  // Still accumulates!
}
// Fix: Extract to function
for _, item := range items {
    processItem(item)
}
```

---

## Summary

| Aspect | Pre-Go 1.22 | Go 1.22+ |
|--------|-------------|----------|
| Loop variable | Reused | New per iteration |
| Closure capture | Buggy (same var) | Fixed (own var) |
| Defer accumulation | Problematic | Still problematic |
| Shadow needed | Yes | No (for capture) |
| Function extract needed | Yes (both issues) | Yes (for accumulation) |

**Key Takeaway**: Go 1.22 fixed closure capture, but **defer accumulation is a separate issue** that still requires extracting to functions.

---

## Further Reading

- [Go 1.22 Release Notes](https://go.dev/doc/go1.22) — Official documentation
- [Refactoring Patterns](04-refactoring-patterns.md) — How to fix defer issues
- [Defer Stack Internals](02-defer-stack-internals.md) — Why accumulation happens

