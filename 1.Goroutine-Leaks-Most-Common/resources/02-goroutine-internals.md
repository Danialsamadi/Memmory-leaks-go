# Goroutine Internals: How Go Manages Goroutines

**Read Time**: 20 minutes

**Prerequisites**: Understanding of basic Go concurrency

**Related Topics**:
- [Conceptual Explanation](./01-conceptual-explanation.md)
- [Channel Mechanics](./03-channel-mechanics.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [The Go Scheduler](#the-go-scheduler)
2. [Goroutine Structure](#goroutine-structure)
3. [Goroutine States](#goroutine-states)
4. [Stack Management](#stack-management)
5. [Creation and Termination](#creation-and-termination)
6. [Why Goroutines Can Leak](#why-goroutines-can-leak)
7. [Summary](#summary)

---

## The Go Scheduler

### M:N Scheduling Model

Go uses an **M:N scheduler**, mapping M goroutines onto N OS threads:

```
Goroutines (G):  g1  g2  g3  g4  g5  g6  g7  g8  ... thousands
                  ↓   ↓   ↓   ↓   ↓   ↓   ↓   ↓
Processors (P):  [P0]    [P1]    [P2]    [P3]      (GOMAXPROCS)
                  ↓       ↓       ↓       ↓
OS Threads (M):  M0      M1      M2      M3         (thread pool)
                  ↓       ↓       ↓       ↓
CPU Cores:      CPU0    CPU1    CPU2    CPU3
```

### The GMP Model

Three key structures:

**G (Goroutine)**:
- Represents a goroutine
- Contains stack, program counter, state
- Lightweight (2 KB initial stack)

**M (Machine)**:
- OS thread
- Executes goroutines
- Heavy (1 MB stack on Linux)

**P (Processor)**:
- Logical processor (execution context)
- Count = GOMAXPROCS (typically = CPU cores)
- Has local run queue of goroutines

### Scheduling Flow

```
1. Goroutine created → Added to P's local run queue
2. M attached to P → Picks goroutine from run queue
3. M runs goroutine → Executes until blocking or preemption
4. Goroutine blocks → M detaches P, creates/reuses another M
5. Goroutine ready → Added back to run queue
6. Repeat
```

**Key Insight**: Blocked goroutines don't block OS threads. The thread can run other goroutines while one is blocked.

### Work Stealing

When P's local queue is empty:

```
P0 (empty)  →  Check global run queue
              ↓
              Check other P's queues (steal half)
              ↓
              Park M (sleep) if nothing found
```

This is why goroutine count matters: the scheduler must track and evaluate all goroutines, even blocked ones.

---

## Goroutine Structure

### The `g` Struct (Simplified)

In the Go runtime (`runtime/runtime2.go`), each goroutine is represented by a `g` struct:

```go
type g struct {
    stack       stack      // Stack bounds [lo, hi]
    stackguard0 uintptr    // Stack guard (for growth detection)
    
    m           *m         // Current M executing this G
    sched       gobuf      // Scheduling context (PC, SP, etc.)
    
    atomicstatus uint32    // Goroutine state
    goid        int64      // Goroutine ID
    
    waitsince   int64      // Time when goroutine started waiting
    waitreason  string     // Reason for waiting ("chan send", etc.)
    
    // ... many more fields
}
```

### Memory Layout

Each goroutine consumes:

```
Goroutine Descriptor (g struct): ~384 bytes
    ├── Stack pointer
    ├── Program counter
    ├── State information
    └── Scheduling metadata

Stack Memory: 2 KB initial (can grow to 1 GB)
    ├── Local variables
    ├── Function call frames
    └── Return addresses

Total minimum: ~2.4 KB per goroutine
```

### Goroutine ID

Every goroutine has a unique ID (`goid`):

```go
// You can't access this directly in Go, but runtime uses it internally
// Visible in stack traces and pprof output:
// goroutine 123 [chan send]:
//            ^^^
//         This is the goid
```

This ID is used for:
- Debugging and profiling
- Stack trace identification
- Internal runtime tracking

**Why It Matters for Leaks**: When you see `goroutine 54321` in production, that's a very high ID indicating many goroutines have been created.

---

## Goroutine States

### State Machine

Goroutines transition through these states:

```
                 ┌──────────────┐
                 │   _Gidle     │  (newly allocated, not initialized)
                 └──────┬───────┘
                        ↓
                 ┌──────────────┐
       ┌────────→│  _Grunnable  │←──────┐  (ready to run)
       │         └──────┬───────┘       │
       │                ↓                │
       │         ┌──────────────┐       │
       │         │   _Grunning  │       │  (executing)
       │         └──────┬───────┘       │
       │                ↓                │
       │    ┌───────────┼───────────┐   │
       │    ↓           ↓           ↓   │
  ┌─────────┐   ┌──────────┐   ┌───────────┐
  │_Gwaiting│   │  _Gsyscall│   │   _Gdead  │  (terminated)
  └─────────┘   └──────────┘   └───────────┘
       ↑                              
  (blocked on channel, select, etc.)
```

### State Definitions

| State | Meaning | Typical Duration | Can Leak? |
|-------|---------|------------------|-----------|
| `_Gidle` | Just allocated | Microseconds | No |
| `_Grunnable` | Ready to run | Milliseconds | No |
| `_Grunning` | Executing | Milliseconds to seconds | Only if infinite loop |
| `_Gwaiting` | Blocked | Variable | **YES** - This is where leaks happen |
| `_Gsyscall` | In system call | Milliseconds | Rarely |
| `_Gdead` | Terminated | N/A - being cleaned up | No |

### The `_Gwaiting` State

This is where leaked goroutines get stuck. Sub-states include:

```go
// Wait reasons (from runtime/runtime2.go)
const (
    waitReasonChanSend          = "chan send"
    waitReasonChanReceive       = "chan receive"
    waitReasonSelect            = "select"
    waitReasonSleep             = "sleep"
    waitReasonSemacquire        = "semacquire"
    waitReasonIOWait            = "IO wait"
    // ... many more
)
```

**In pprof output**:
```
goroutine 123 [chan send, 15 minutes]:
              ^^^^^^^^^^  ^^^^^^^^^^
              wait reason   duration
```

**15 minutes in "chan send"** = Almost certainly a leak!

### Checking Goroutine State

You can see states in the debug output:

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

Output:
```
goroutine 1 [running]:
main.main()
    /path/to/main.go:10 +0x100

goroutine 17 [chan send, 30 minutes]:
main.leakFunc.func1()
    /path/to/leak.go:42 +0x50
created by main.leakFunc
    /path/to/leak.go:40 +0x30
```

---

## Stack Management

### Initial Stack Size

Go 1.14+ uses **2 KB initial stacks** (down from 8 KB in earlier versions):

```
New goroutine → Allocate 2 KB stack
              ↓
       [Stack: 2 KB]
       ├── Local vars
       └── Call frames
```

Why so small?
- Enables millions of goroutines
- Most goroutines don't need large stacks
- Stack can grow dynamically

### Stack Growth

When stack space runs out, the runtime **copies to a larger stack**:

```
Original (2 KB):     ┌────────────┐
                     │  Stack     │
                     │  (full)    │
                     └────────────┘
                            ↓
                     [Stack overflow detected]
                            ↓
Larger (4 KB):       ┌────────────────────┐
                     │  Old stack (copied)│
                     │  ────────────────  │
                     │  New space         │
                     └────────────────────┘
```

Stack growth pattern:
```
2 KB → 4 KB → 8 KB → 16 KB → ... up to 1 GB
```

### Why This Matters for Leaks

**Memory consumption calculation**:
```
10,000 leaked goroutines × 2 KB each = 20 MB minimum
```

But if they're deep in call stacks:
```
10,000 leaked goroutines × 8 KB average = 80 MB
```

Plus, stack growth is permanent for that goroutine (doesn't shrink until Go 1.14+).

### Stack Traces

Each goroutine's stack contains:

```
Stack Frame N:     ┌─────────────────┐
                   │ Return address  │
                   │ Local variables │
                   │ Function args   │
                   ├─────────────────┤
Stack Frame N-1:   │ Return address  │
                   │ Local variables │
                   │ Function args   │
                   ├─────────────────┤
                   │      ...        │
                   ├─────────────────┤
Stack Frame 0:     │ main() frame    │
                   └─────────────────┘
```

This is what pprof shows as the "stack trace" - the call chain that led to the current state.

---

## Creation and Termination

### Creating a Goroutine

When you write:
```go
go myFunc(arg)
```

The runtime does:

```
1. Allocate `g` struct (~384 bytes)
2. Allocate initial stack (2 KB)
3. Set up scheduling context:
   - PC = address of myFunc
   - SP = top of stack
   - Arguments copied to stack
4. Set state = _Grunnable
5. Add to P's local run queue
6. If needed, wake up or create M

Total time: ~1-2 microseconds
```

**This is why goroutines are cheap**: Simple allocation, no system calls, just memory and bookkeeping.

### Terminating a Goroutine

When a goroutine returns:

```
1. Set state = _Gdead
2. Clear pointers (allow GC to collect referenced objects)
3. Put `g` struct on free list (recycled for new goroutines)
4. Stack memory freed or recycled

Total time: <1 microsecond
```

**Critical**: Steps 1-4 only happen when the goroutine's function **returns**. Blocked goroutines never reach this point.

### The Free List

Go maintains a pool of `g` structs:

```
Dead goroutines → [Free list] → Reused for new goroutines
```

Benefits:
- Faster creation (no allocation)
- Better cache locality
- Reduced GC pressure

But leaked goroutines never reach the free list, so the pool can't be reused.

---

## Why Goroutines Can Leak

### The Runtime's Perspective

The runtime **cannot distinguish** between:
- A goroutine waiting for an event that will arrive
- A goroutine waiting for an event that will never arrive (leaked)

Both look identical:
```
State: _Gwaiting
Reason: "chan receive"
Duration: 10 minutes
```

Is this goroutine:
- Waiting for a legitimate slow operation?
- Leaked because the channel will never be written to?

**The runtime doesn't know**, so it keeps the goroutine alive forever.

### No Automatic Cleanup

Unlike some languages with automatic thread cleanup, Go:
- **Does NOT** time out blocked goroutines
- **Does NOT** forcefully terminate long-running goroutines
- **Does NOT** track goroutine "ownership" or relationships

Why?
- Performance: Tracking would add overhead
- Correctness: Forced termination could leave shared state corrupt
- Simplicity: Clear, explicit lifecycle management

**The tradeoff**: Developer responsibility for cleanup.

### The Scheduler's Overhead

Even blocked goroutines consume scheduler resources:

```go
// Simplified scheduler loop
for {
    g := findRunnableGoroutine()  // Checks ALL goroutines
    if g != nil {
        run(g)
    }
    
    // Even blocked goroutines are checked
    // to see if they've become unblocked
}
```

More goroutines = more checking = slower scheduling.

### Memory Pressure

Leaked goroutines prevent GC from collecting:

```
Goroutine stack → References local variable → Points to heap object
                                            → GC can't collect it
```

Example:
```go
func leak() {
    data := make([]byte, 1<<20)  // 1 MB allocation
    ch := make(chan bool)
    
    go func() {
        // This goroutine's stack holds a pointer to 'data'
        processData(data)
        <-ch  // Blocks forever
        // 'data' can't be GC'd as long as this goroutine exists
    }()
}
```

Each leaked goroutine can hold references to megabytes of heap data.

---

## Summary

### Key Takeaways

1. **M:N Scheduling**: Thousands of goroutines multiplex onto a few OS threads
2. **Lightweight**: 2 KB stack + ~384 byte descriptor = ~2.4 KB per goroutine
3. **State-Based**: Goroutines are state machines; `_Gwaiting` is where leaks occur
4. **No Auto-Cleanup**: Blocked goroutines live forever unless unblocked
5. **Scheduler Overhead**: Even blocked goroutines consume scheduler resources

### Goroutine Lifecycle Rules

1. **Creation**: Fast and cheap (1-2 μs)
2. **Scheduling**: Handled by efficient Go scheduler
3. **Blocking**: Doesn't block OS threads (other goroutines can run)
4. **Termination**: Only happens when function returns
5. **Cleanup**: Stack and descriptor freed after termination

### Implications for Leak Prevention

**Design Principles**:
- Every goroutine needs an exit condition
- Use `context.Context` to propagate cancellation
- Monitor goroutine count in production
- Test goroutine count in unit tests

**Detection**:
- Watch for `_Gwaiting` goroutines with long durations
- Look for identical stack traces repeated many times
- Monitor `runtime.NumGoroutine()` for growth

**Prevention**:
- Bounded goroutine creation (worker pools)
- Timeouts on blocking operations
- Proper channel lifecycle (close when done)
- Context cancellation in all goroutines

---

## Further Reading

**Runtime Source Code** (for the curious):
- `runtime/runtime2.go` - Goroutine and scheduler structures
- `runtime/proc.go` - Scheduler implementation
- `runtime/stack.go` - Stack management

**Related Resources**:
- [Channel Mechanics](./03-channel-mechanics.md) - How channels cause blocking
- [Context Pattern](./04-context-pattern.md) - Using context to prevent leaks
- [Detection Methods](./05-detection-methods.md) - Finding leaked goroutines

**External Resources**:
- "Go Scheduler" by Kavya Joshi (talk)
- "The Go Scheduler" by William Kennedy (article series)
- Go source code: `src/runtime/`

---

**Return to**: [Goroutine Leaks README](../README.md)

