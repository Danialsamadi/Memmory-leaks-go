# Visual Diagrams: Goroutine Leak Visualizations

**Read Time**: 15 minutes

**Related Topics**:
- [Conceptual Explanation](./01-conceptual-explanation.md)
- [Channel Mechanics](./03-channel-mechanics.md)
- [Back to README](../README.md)

---

## Goroutine Lifecycle: Leak vs Healthy

### Healthy Goroutine Lifecycle

```
Time →
────────────────────────────────────────────────────────────────
Main:     [Start] ──────────────────────────────────── [Running]
              │
              │ spawn
              ↓
Worker:    [Create]─[Run]─[Block]─[Resume]─[Complete]
              0ms    10ms   20ms    30ms      40ms
                                              ↓
                                           [Cleaned]

Goroutine Count:
  1 ──→ 2 ──────────────────────→ 1
```

### Leaked Goroutine

```
Time →
────────────────────────────────────────────────────────────────
Main:     [Start] ──────────────────────────────────── [Running]
              │
              │ spawn
              ↓
Worker:    [Create]─[Run]─[Block]─────────────────────→ [Stuck]
              0ms    10ms   20ms    30ms ... forever
                           ↓
                      [LEAKED - Never exits]

Goroutine Count:
  1 ──→ 2 ──────────────────────→ 2 (stays at 2)
```

---

## Channel Blocking Diagram

### Unbuffered Channel - No Receiver

```
Sender Goroutine:              Channel:              Receiver:
                                                     (none)
    ↓
[Compute result]
    ↓
  value = 42
    ↓
ch <- value  ──────────────→  [unbuffered]
    ↓                            queue: []
[BLOCKED]                        sendq: [G1] ←─── Goroutine stuck here
    ↓                            recvq: []        (no receiver)
[WAITING]
    ↓
  (forever)


After 10 seconds:
    Goroutine still blocked
    State: _Gwaiting
    Reason: "chan send"
    Duration: 10s
```

### With Receiver - Healthy

```
Sender:                  Channel:                 Receiver:
                                                      ↓
  ↓                                              [Ready to
[Compute]                                         receive]
  ↓                                                  ↓
ch <- value  ──→  [unbuffered]  ──────────→    value := <-ch
  ↓              ╔═══════════════════════╗          ↓
[Rendezvous]    ║  Both goroutines meet ║      [Received]
  ↓              ║  Data transferred     ║          ↓
[Continue]       ╚═══════════════════════╝      [Continue]


Both goroutines continue executing - No leak!
```

---

## Goroutine Accumulation Over Time

### Leaky Application

```
Goroutine Count Over Time:

500 |                                              ╱
    |                                            ╱
400 |                                          ╱
    |                                        ╱
300 |                                      ╱
    |                                    ╱
200 |                                  ╱
    |                                ╱
100 |                              ╱
    |                            ╱
  0 |________________________╱_______________
    0s   10s   20s   30s   40s   50s   60s

Pattern: Linear growth
Rate: ~50 goroutines/second
Cause: Each spawned goroutine blocks and never exits
```

### Fixed Application

```
Goroutine Count Over Time:

500 |
    |
400 |
    |
300 |
    |
200 |
    |
100 |
    |
  3 |════════════════════════════════════════
  0 |___________________________________________
    0s   10s   20s   30s   40s   50s   60s

Pattern: Flat line
Baseline: 3 goroutines (main + pprof server + coordinator)
Cause: Goroutines complete and exit properly
```

---

## Context Cancellation Flow

### Without Context - Leak

```
Parent Function:              Worker Goroutine:
     ↓                             ↓
[Start worker] ──spawn──→     [Start loop]
     ↓                             ↓
[Return]                      [Iteration 1]
     ↓                             ↓
(exited)                       [Iteration 2]
                                   ↓
                              [Iteration 3]
                                   ↓
                                  ...
                               (forever)

No communication path to tell worker to stop!
```

### With Context - No Leak

```
Parent:                 Context:              Worker:
   ↓                       ↓                     ↓
[Create ctx] ──────→  [Active]            [Start loop]
   ↓                       ↓                     ↓
[Start worker] ──pass─→  [Active]  ──check─→ [Working]
   ↓                       ↓                     ↓
[cancel()]  ─signal──→ [Done()]             [Checks ctx]
   ↓                       ↓                     ↓
[Return]              [Cancelled]            [Exits]
                           ↓                     ↓
                      (ch closed)           (cleaned up)

Clear cancellation path exists!
```

---

## Select Statement Patterns

### Blocking Select - Leak

```
select {
    case val := <-ch1:  ─────→ [ch1: no data]
        process(val)
                                    ↓
    case val := <-ch2:  ─────→ [ch2: no data]
        process(val)
}                                   ↓
    ↓                          [BLOCKED]
[NEVER EXITS]                       ↓
                               (waiting on
                              both channels)

No case is ready → Goroutine blocks forever
```

### Non-Blocking Select - No Leak

```
select {
    case val := <-ch1:  ─────→ [ch1: no data]
        process(val)
                                    ↓
    case val := <-ch2:  ─────→ [ch2: no data]  
        process(val)
                                    ↓
    case <-ctx.Done():  ─────→ [ctx: Done!] ←─ Context cancelled
        return                      ↓
}                               [Exit case]
    ↓                               ↓
[EXITS]  ←───────────────────── [Returns]

Context provides guaranteed exit path
```

---

## Goroutine State Transitions

### Normal Execution

```
_Gidle ──→ _Grunnable ──→ _Grunning ──→ _Gwaiting ──→ _Grunnable ──→ _Gdead
(new)      (ready)        (executing)   (I/O wait)   (ready again)   (done)
                                             ↓
                                        [Temporary]
                                        (unblocks)
```

### Leaked Goroutine

```
_Gidle ──→ _Grunnable ──→ _Grunning ──→ _Gwaiting ──────────────→ [STUCK]
(new)      (ready)        (executing)   (chan send)
                                             ↓
                                        [Permanent]
                                      (never unblocks)
                                             ↓
                                        Duration: ∞
                                        Memory: Leaked
```

---

## Memory Layout: Leaked vs Healthy

### Healthy System

```
Memory Layout:

Heap:           Stack:                   Goroutines:
┌────────┐      ┌──────────┐            ┌─────────┐
│ Objects│      │ G1 (8KB) │            │ Count: 5│
│ ~10 MB │      │ G2 (4KB) │            │ Active  │
│        │      │ G3 (2KB) │            │ Working │
└────────┘      │ G4 (2KB) │            └─────────┘
                │ G5 (2KB) │
                └──────────┘
                Total: 18KB
```

### Leaked System (After 1 Hour)

```
Memory Layout:

Heap:           Stack:                      Goroutines:
┌────────┐      ┌────────────────────┐     ┌──────────────┐
│ Objects│      │ G1-G1000 (2KB each)│     │ Count: 1005  │
│ ~15 MB │      │ = 2 MB             │     │ 1000 leaked  │
│Growing │      │                    │     │ 5 working    │
└────────┘      │ G1001-G3000 (2KB)  │     └──────────────┘
                │ = 4 MB             │
                │                    │     Stack growth!
                │ Total: ~6 MB       │     ↓
                └────────────────────┘     System unstable
```

---

## Producer-Consumer Pattern

### Leak Pattern

```
Producer:                  Queue:                Consumer:
    ↓                        ↓                       ↓
[Generate] ──send──→    [Channel]              [BLOCKED]
    ↓                        ↓                       ↓
[Generate] ──send──→    [Channel]              (waiting for
    ↓                        ↓                   channel that
[Generate] ──send──→    [Channel]               never sends)
    ↓                        ↓
  (queue                  [Growing]
   grows                   ↓
   forever)            [Memory leak]


Issue: Consumer never starts or channel misconfigured
```

### Healthy Pattern

```
Producer:                  Queue:                Consumer:
    ↓                        ↓                       ↓
[Generate] ──send──→    [Channel]  ──recv──→   [Process]
    ↓                        ↓                       ↓
[Generate] ──send──→    [Channel]  ──recv──→   [Process]
    ↓                        ↓                       ↓
[Done]                   [Empty]                 [Done]
    ↓                        ↓                       ↓
close(ch)            [Closed] ──signal──→      [Exit loop]


All goroutines complete properly
```

---

## HTTP Handler Goroutine Leak

### Leaky Handler

```
Request arrives:              Handler spawns:           Goroutine:
      ↓                             ↓                       ↓
[Client connects]            go makeAPICall()         [Call API]
      ↓                             ↓                       ↓
[Handler returns]──────────→ [Response sent]          [Waiting...]
      ↓                             ↓                       ↓
[Connection closed]           [Handler exits]          [Still waiting]
                                                            ↓
                                                       [No timeout]
                                                            ↓
                                                        [LEAKED]

1000 requests = 1000 leaked goroutines!
```

### Fixed Handler

```
Request arrives:              Handler spawns:           Goroutine:
      ↓                             ↓                       ↓
[Client connects]            go makeAPICall(ctx)      [Call API]
      ↓                             ↓                       ↓
[Handler returns]            [Response sent]          [Waiting...]
      ↓                             ↓                       ↓
[Connection closed]          [ctx.Done()]  ──────→    [Receives cancel]
      ↓                             ↓                       ↓
                                                        [Returns]
                                                            ↓
                                                        [Cleaned up]

Context ensures cleanup when connection closes
```

---

## Timeline: Leak Detection

```
Time:     0s          30s         60s         90s         Detection:
          │           │           │           │
Goroutines:
Count:    5 ────→    505 ───→   1005 ──→   1505        [Growing!]
          │           │           │           │
          │           │           │           │
Memory:   │           │           │           │
Stack:    40KB ──→   4MB ───→   8MB ───→   12MB       [Growing!]
          │           │           │           │
          │           │           │           │
Symptoms: │           │           │           │
          Normal   Slower     Degraded    Unstable     [Impact!]
          │           │           │           │
          │           │           │           │
Action:   │           │           │           │
          None     Monitor    Alert!      Fix!         [Response]


Detection Points:
├─ 30s: Automated monitoring notices growth
├─ 60s: Alert triggered (threshold exceeded)
└─ 90s: Manual intervention required
```

---

## Summary Diagram: Leak Prevention

```
┌──────────────────────────────────────────────────────┐
│           GOROUTINE LEAK PREVENTION                  │
├──────────────────────────────────────────────────────┤
│                                                      │
│  Design Phase:                                       │
│  ├─ Every goroutine has exit condition              │
│  ├─ Use context.Context for cancellation            │
│  └─ Document goroutine lifecycle                    │
│                                                      │
│  Implementation:                                     │
│  ├─ Pass ctx as first parameter                     │
│  ├─ Check <-ctx.Done() in select                    │
│  └─ Always defer cancel()                           │
│                                                      │
│  Testing:                                            │
│  ├─ Use goleak in tests                             │
│  ├─ Check goroutine count before/after              │
│  └─ Stress test with many iterations                │
│                                                      │
│  Production:                                         │
│  ├─ Monitor runtime.NumGoroutine()                  │
│  ├─ Collect pprof profiles                          │
│  ├─ Set up alerts for growth                        │
│  └─ Dashboard with goroutine metrics                │
│                                                      │
└──────────────────────────────────────────────────────┘
```

---

## Further Reading

- [Conceptual Explanation](./01-conceptual-explanation.md)
- [Context Pattern](./04-context-pattern.md)
- [Detection Methods](./05-detection-methods.md)
- [Real-World Examples](./07-real-world-examples.md)

---

**Return to**: [Goroutine Leaks README](../README.md)

