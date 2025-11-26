# Defer Stack Internals

**Reading Time**: 20 minutes

---

## Introduction

Understanding how Go implements `defer` at the runtime level helps explain why defer-in-loop causes problems and how to optimize defer usage. This document explores the internal mechanics of Go's defer implementation.

---

## The _defer Structure

Each defer statement creates a `_defer` record in the runtime. Here's the simplified structure:

```go
// From src/runtime/runtime2.go (simplified)
type _defer struct {
    siz       int32    // Size of arguments
    started   bool     // Whether defer has begun executing
    heap      bool     // Whether allocated on heap
    openDefer bool     // Whether this is an open-coded defer
    sp        uintptr  // Stack pointer at time of defer
    pc        uintptr  // Program counter at time of defer
    fn        *funcval // Function to call
    _panic    *_panic  // Panic that triggered this defer (if any)
    link      *_defer  // Link to next defer in chain
    // Arguments follow this structure in memory
}
```

**Size**: The base `_defer` structure is approximately **48-56 bytes** on 64-bit systems, plus any captured arguments.

---

## The Defer Chain

Go maintains a singly-linked list of defer records for each goroutine:

```go
// From src/runtime/runtime2.go
type g struct {
    // ... other fields
    _defer *_defer  // Head of defer chain for this goroutine
    // ... other fields
}
```

### How Defers are Added

When you write `defer f()`, the compiler generates code that:

1. Allocates a `_defer` record
2. Fills in the function pointer and arguments
3. Links it to the head of the defer chain
4. Sets `sp` and `pc` to identify the calling function

```
Before: g._defer → defer1 → defer2 → nil

defer f() executes

After:  g._defer → defer3 → defer1 → defer2 → nil
        (defer3 is the new one)
```

### How Defers are Executed

When a function returns, `runtime.deferreturn` walks the defer chain:

```go
// Simplified from src/runtime/panic.go
func deferreturn() {
    gp := getg()
    for {
        d := gp._defer
        if d == nil {
            return
        }
        // Check if this defer belongs to current function
        if d.sp != getcallersp() {
            return
        }
        // Remove from chain
        gp._defer = d.link
        // Execute the deferred function
        reflectcall(nil, d.fn, deferArgs(d), uint32(d.siz), uint32(d.siz))
        // Free the defer record
        freedefer(d)
    }
}
```

---

## Three Implementations of Defer

Go has evolved to have three different defer implementations, each optimized for different cases:

### 1. Heap-Allocated Defer (Original)

The original implementation allocates defer records on the heap:

```go
// When defer is in a loop or complex control flow
for i := 0; i < n; i++ {
    defer f(i)  // Uses heap allocation
}
```

**Cost per defer**:
- ~48-56 bytes heap allocation
- Runtime overhead for allocation/deallocation
- Garbage collection pressure

### 2. Stack-Allocated Defer (Go 1.13+)

When the compiler can prove a defer is simple and bounded:

```go
// Simple defer, not in loop
func process() {
    f, _ := os.Open(name)
    defer f.Close()  // Can be stack-allocated
    // ...
}
```

**Optimization**: The `_defer` record is allocated on the stack frame, avoiding heap allocation.

**Benefit**: ~30% faster than heap-allocated defer

### 3. Open-Coded Defer (Go 1.14+)

For simple defers without loops, the compiler can inline the defer:

```go
// Compiler transforms this:
func process() {
    f, _ := os.Open(name)
    defer f.Close()
    // ... do work ...
    return result
}

// Into something like this:
func process() {
    f, _ := os.Open(name)
    // ... do work ...
    f.Close()  // Inlined at all return points
    return result
}
```

**Requirement**: 
- No defer in loops
- At most 8 defers in the function
- Simple defer (no recover)

**Benefit**: ~8x faster than heap-allocated defer

---

## Why Defer-in-Loop Defeats Optimizations

When defer appears inside a loop, the compiler cannot use stack allocation or open-coding:

```go
func bad() {
    for i := 0; i < 1000; i++ {
        defer f(i)  // Must use heap allocation
    }
}
```

**Why heap is required**:
1. The number of defers is unknown at compile time (could be 0, 1000, or more)
2. Stack frame size must be fixed at compile time
3. Each defer needs separate storage for its captured `i` value

**Consequence**: Each iteration causes a heap allocation, leading to:
- Memory pressure (48+ bytes × N iterations)
- GC overhead
- Slower execution

---

## Memory Layout Visualization

### Single Defer (Optimized)

```
┌─────────────────────────────────────────────┐
│             Stack Frame                     │
├─────────────────────────────────────────────┤
│  Local variables                            │
│  _defer record (stack allocated)            │
│    - fn: pointer to Close                   │
│    - args: file pointer                     │
│    - link: nil (or previous defer)          │
│  Return address                             │
└─────────────────────────────────────────────┘
```

### Defer in Loop (Unoptimized)

```
┌─────────────────────────────────────────────┐
│             Stack Frame                     │
├─────────────────────────────────────────────┤
│  Local variables                            │
│  g._defer pointer → ─────────────┐          │
│  Return address                   │          │
└───────────────────────────────────┼──────────┘
                                    │
┌───────────────────────────────────▼──────────┐
│             Heap                             │
├──────────────────────────────────────────────┤
│  _defer #1000                                │
│    - fn: pointer to Close                    │
│    - args: file1000 pointer                  │
│    - link: ─────────────────────────────┐    │
│                                          │    │
│  _defer #999                             │    │
│    - fn: pointer to Close       ◄────────┘   │
│    - args: file999 pointer                   │
│    - link: ─────────────────────────────┐    │
│                                          │    │
│  ...                                     │    │
│                                          │    │
│  _defer #1                               │    │
│    - fn: pointer to Close       ◄────────┘   │
│    - args: file1 pointer                     │
│    - link: nil                               │
└──────────────────────────────────────────────┘
```

---

## Performance Measurements

### Benchmark: Defer Allocation Cost

```go
func BenchmarkDeferHeap(b *testing.B) {
    for i := 0; i < b.N; i++ {
        func() {
            for j := 0; j < 100; j++ {
                defer func() {}()  // Heap allocated
            }
        }()
    }
}

func BenchmarkDeferStack(b *testing.B) {
    for i := 0; i < b.N; i++ {
        func() {
            defer func() {}()  // Stack allocated
        }()
    }
}

func BenchmarkDeferOpenCoded(b *testing.B) {
    for i := 0; i < b.N; i++ {
        func() {
            f := openFile()
            defer f.Close()  // Open-coded
            process(f)
        }()
    }
}
```

**Typical Results** (Go 1.21, M1 Mac):

| Pattern | Time per Operation | Allocations |
|---------|-------------------|-------------|
| 100 heap defers | 1,200 ns | 100 |
| 1 stack defer | 12 ns | 0 |
| 1 open-coded defer | 1.5 ns | 0 |

**Key Insight**: Open-coded defer is ~800x faster than heap-allocated defer!

---

## How to Check Which Defer Type is Used

Use the `-gcflags="-m"` to see compiler decisions:

```bash
go build -gcflags="-m" example.go 2>&1 | grep defer
```

**Sample Output**:

```
./example.go:10:7: defer f() heap-allocated
./example.go:20:7: defer g() open-coded
./example.go:30:7: defer h() stack-allocated
```

**Rules**:
- Defer in loop → heap-allocated
- Simple defer, ≤8 in function → open-coded
- Complex defer, no loop → stack-allocated

---

## The Open-Coded Defer Mechanism

When a function uses open-coded defers, the compiler generates:

1. A **defer bits** variable tracking which defers to execute
2. Cleanup code at each return point

```go
// Source:
func example() error {
    f, err := os.Open("a.txt")
    if err != nil {
        return err
    }
    defer f.Close()
    
    g, err := os.Open("b.txt")
    if err != nil {
        return err
    }
    defer g.Close()
    
    return process(f, g)
}

// Conceptually compiles to:
func example() error {
    var df uint8 = 0  // defer bits: which defers are active
    
    f, err := os.Open("a.txt")
    if err != nil {
        // No cleanup needed, df = 0
        return err
    }
    df |= 1  // Mark first defer as active
    
    g, err := os.Open("b.txt")
    if err != nil {
        if df & 1 != 0 { f.Close() }  // Cleanup first defer
        return err
    }
    df |= 2  // Mark second defer as active
    
    result := process(f, g)
    // Execute defers in LIFO order
    if df & 2 != 0 { g.Close() }
    if df & 1 != 0 { f.Close() }
    return result
}
```

---

## Impact on Garbage Collection

### Heap-Allocated Defers and GC

Each heap-allocated defer:
1. Creates an object that must be tracked by GC
2. Is freed after execution (more work for GC)
3. May reference other heap objects (closure captures)

With 1000 defers in a loop:
- 1000 heap objects created
- 1000 heap objects freed when function returns
- Potential GC pause if many goroutines do this

### GC-Friendly Patterns

```go
// ❌ GC unfriendly: 1000 allocations
for i := 0; i < 1000; i++ {
    defer f(i)
}

// ✅ GC friendly: ~0 allocations (uses open-coded defer)
for i := 0; i < 1000; i++ {
    func() {
        defer f(i)  // Each is open-coded in its own function
    }()
}
```

---

## Defer and Panic/Recover

Defers play a crucial role in panic recovery:

```go
func safeCall() (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v", r)
        }
    }()
    
    riskyOperation()
    return nil
}
```

**Important**: When a panic occurs:
1. The runtime walks the defer chain
2. Executes each defer in LIFO order
3. If a defer calls `recover()`, the panic is caught
4. Remaining defers still execute

**In loops**: If you have 1000 defers and a panic occurs after 500:
- All 1000 defers still execute
- This can be useful (cleanup all resources) or problematic (slow recovery)

---

## Recommendations

### For Performance-Critical Code

1. **Avoid defer in loops** — use explicit cleanup or extract to function
2. **Keep defers simple** — enable open-coding optimization
3. **Limit defers per function** — max 8 for open-coding
4. **Profile defer overhead** — use `go test -bench` and `-gcflags="-m"`

### For Maintainable Code

1. **Prefer extracted functions** — clearer semantics than anonymous functions
2. **Use defer for complex cleanup** — when explicit Close() is error-prone
3. **Accept defer overhead** — in non-critical paths, clarity beats micro-optimization

---

## Summary

| Defer Type | Allocation | Performance | Use Case |
|------------|------------|-------------|----------|
| Open-coded | None | Fastest (~1.5 ns) | Simple defers, ≤8 per function |
| Stack | Stack frame | Fast (~12 ns) | Complex defers, no loops |
| Heap | Heap | Slow (~1200 ns for 100) | Defers in loops |

**Key Takeaway**: Defer-in-loop forces heap allocation, losing all performance optimizations and creating GC pressure.

---

## Further Reading

- [Loop Mechanics](03-loop-mechanics.md) — How loop variables interact with closures
- [Performance Impact](05-performance-impact.md) — Detailed benchmarks
- [Go Source: runtime/panic.go](https://github.com/golang/go/blob/master/src/runtime/panic.go) — Implementation details

