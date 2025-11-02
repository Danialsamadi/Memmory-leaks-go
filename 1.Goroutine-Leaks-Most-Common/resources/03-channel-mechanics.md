# Channel Mechanics: How Channels Cause Goroutine Leaks

**Read Time**: 20 minutes

**Prerequisites**: Basic understanding of Go channels

**Related Topics**:
- [Goroutine Internals](./02-goroutine-internals.md)
- [Context Pattern](./04-context-pattern.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Channel Fundamentals](#channel-fundamentals)
2. [Unbuffered Channels](#unbuffered-channels)
3. [Buffered Channels](#buffered-channels)
4. [Blocking Conditions](#blocking-conditions)
5. [Select Statement Mechanics](#select-statement-mechanics)
6. [Channel Closure](#channel-closure)
7. [Common Leak Patterns](#common-leak-patterns)
8. [Summary](#summary)

---

## Channel Fundamentals

### What Is a Channel?

A channel is a **typed conduit** for sending and receiving values between goroutines:

```go
ch := make(chan int)     // Unbuffered channel
ch := make(chan int, 5)  // Buffered channel with capacity 5
```

Internally, a channel is a struct containing:
- A circular queue (buffer)
- A mutex (for synchronization)
- Send queue (blocked senders)
- Receive queue (blocked receivers)

### Channel Structure (Simplified)

```
type hchan struct {
    qcount   uint           // number of items in buffer
    dataqsiz uint           // size of circular buffer
    buf      unsafe.Pointer // circular queue
    
    sendx    uint           // send index in buffer
    recvx    uint           // receive index in buffer
    
    recvq    waitq          // blocked receivers
    sendq    waitq          // blocked senders
    
    lock     mutex          // protects all fields
}
```

### The Wait Queues

This is where goroutine leaks happen:

```
Channel:
    Buffer: [___] (empty or full)
    
    Send Queue:    [g1] → [g2] → [g3]  ← Goroutines waiting to send
    Receive Queue: [g4] → [g5]         ← Goroutines waiting to receive
```

**If a goroutine enters a wait queue and never leaves**, it's leaked.

---

## Unbuffered Channels

### Definition

An unbuffered channel has **capacity 0**:

```go
ch := make(chan int)  // Buffer size = 0
```

### Synchronization Semantics

Unbuffered channels provide **synchronous communication**:

```
Sender:                  Receiver:
  ↓                        ↓
ch <- value           value := <-ch
  ↓                        ↓
BLOCKS until            BLOCKS until
receiver ready          sender ready
  ↓                        ↓
    ╔══════════════════════╗
    ║  Both rendezvous     ║
    ║  Data transferred    ║
    ║  Both continue       ║
    ╚══════════════════════╝
```

Both sides must be ready **simultaneously**.

### Timeline Example

```
Time    Goroutine 1 (Sender)         Goroutine 2 (Receiver)
────────────────────────────────────────────────────────────
t0      ch <- 42
t1      [BLOCKED - waiting]          (doing other work)
t2      [BLOCKED - waiting]          (doing other work)
t3      [BLOCKED - waiting]          val := <-ch
t4      [DATA TRANSFERRED - continues]  [receives 42]
```

**If Goroutine 2 never executes `<-ch`**, Goroutine 1 blocks forever = leak.

### Send on Unbuffered Channel (Detailed)

```go
ch := make(chan int)

go func() {
    ch <- 42  // What happens here?
}()
```

**Runtime steps**:
1. Goroutine attempts send
2. Runtime acquires channel lock
3. Checks receive queue:
   - **If receiver waiting**: Transfer data directly, wake receiver, continue
   - **If no receiver**: Add goroutine to send queue, block, release lock
4. Goroutine enters `_Gwaiting` state with reason "chan send"
5. Goroutine waits indefinitely

**Goroutine can only become unblocked when**:
- Another goroutine receives from the channel, OR
- The channel is closed (causes panic on sender)

### Receive on Unbuffered Channel

```go
val := <-ch  // What happens here?
```

**Runtime steps**:
1. Goroutine attempts receive
2. Runtime acquires channel lock
3. Checks send queue:
   - **If sender waiting**: Transfer data, wake sender, continue
   - **If no sender and channel not closed**: Add goroutine to receive queue, block
4. Goroutine enters `_Gwaiting` state with reason "chan receive"

**Goroutine can only become unblocked when**:
- Another goroutine sends on the channel, OR
- The channel is closed (receive returns zero value)

---

## Buffered Channels

### Definition

A buffered channel has **capacity > 0**:

```go
ch := make(chan int, 3)  // Can hold 3 values
```

### Buffer Mechanics

```
Buffered Channel (capacity 3):

Empty:     [___][___][___]    ← Can receive 3 sends without blocking
           ^
           sendx, recvx = 0

1 item:    [42][___][___]     ← 1 send succeeded, 2 more can succeed
           ^   ^
           recvx sendx

Full:      [42][99][17]       ← Next send will block
           ^               ^
           recvx           sendx
```

### Send on Buffered Channel

```go
ch := make(chan int, 2)

ch <- 1  // Succeeds immediately (buffer has space)
ch <- 2  // Succeeds immediately (buffer has space)
ch <- 3  // BLOCKS (buffer is full)
```

**Timeline**:
```
Time    Buffer State         Operation          Result
───────────────────────────────────────────────────────────
t0      [___][___]          ch <- 1            Success
t1      [1][___]            ch <- 2            Success
t2      [1][2]              ch <- 3            BLOCKS
t3      [1][2]              (waiting...)       Still blocked
t4      [1][2]              val := <-ch        Unblocks sender
t5      [2][3]              All operations     Complete
```

### Receive on Buffered Channel

```go
ch := make(chan int, 2)

val1 := <-ch  // BLOCKS (buffer is empty)

// In another goroutine:
ch <- 42      // Unblocks receiver
```

**Blocking conditions**:
- **Send blocks** when: Buffer is full AND no receiver waiting
- **Receive blocks** when: Buffer is empty AND no sender waiting AND channel not closed

### Buffer as Anti-Leak Mechanism

Buffered channels can prevent some leaks:

```go
// Leaky:
func bad() {
    ch := make(chan int)  // Unbuffered
    go func() {
        ch <- computeResult()  // BLOCKS if no receiver
    }()
    // Function returns, goroutine leaks
}

// Fixed:
func better() {
    ch := make(chan int, 1)  // Buffered
    go func() {
        ch <- computeResult()  // Succeeds immediately
    }()
    // Goroutine completes and exits
}
```

**But**: This doesn't work if you send multiple times:
```go
func stillBad() {
    ch := make(chan int, 1)  // Buffer size 1
    go func() {
        ch <- 1  // OK
        ch <- 2  // BLOCKS (buffer full)
    }()
}
```

**Better solution**: Use context or close the channel when done.

---

## Blocking Conditions

### Complete Blocking Matrix

| Channel Type | Operation | Buffer State | Sender Waiting? | Receiver Waiting? | Result |
|--------------|-----------|--------------|-----------------|-------------------|--------|
| Unbuffered | Send | N/A | N/A | No | **BLOCKS** |
| Unbuffered | Send | N/A | N/A | Yes | Success |
| Unbuffered | Receive | N/A | No | N/A | **BLOCKS** |
| Unbuffered | Receive | N/A | Yes | N/A | Success |
| Buffered | Send | Not full | N/A | N/A | Success |
| Buffered | Send | Full | N/A | No | **BLOCKS** |
| Buffered | Send | Full | N/A | Yes | Success |
| Buffered | Receive | Not empty | N/A | N/A | Success |
| Buffered | Receive | Empty | No | N/A | **BLOCKS** |
| Buffered | Receive | Empty | Yes | N/A | Success |

**Bold = Potential leak point**

### Permanent vs Temporary Blocking

**Temporary Blocking** (healthy):
```go
// Receiver will eventually read
ch := make(chan int)
go func() {
    time.Sleep(100 * time.Millisecond)
    val := <-ch  // Unblocks sender after 100ms
}()
ch <- 42  // Blocks temporarily
```

**Permanent Blocking** (leak):
```go
// No receiver exists
ch := make(chan int)
go func() {
    ch <- 42  // Blocks forever
}()
// Goroutine leaks
```

The runtime can't tell the difference!

---

## Select Statement Mechanics

### Basic Select

```go
select {
case val := <-ch1:
    // Executed if ch1 has data
case ch2 <- value:
    // Executed if ch2 has space
default:
    // Executed if no case is ready
}
```

### Select Execution Algorithm

```
1. Evaluate all channel expressions (left to right)
2. Lock all channels in the select
3. Check each case:
   - If any case is ready, execute it
   - If multiple cases ready, choose one randomly
   - If no case ready and there's a default, execute default
   - If no case ready and no default, block
4. Unlock channels
5. If blocked, add goroutine to wait queue of all channels
```

### Select Without Default

```go
select {
case <-ch1:
case <-ch2:
}
```

**If both channels never receive data**, goroutine blocks forever on the select = leak.

### Select With Context (Anti-Leak)

```go
select {
case val := <-ch:
    process(val)
case <-ctx.Done():
    return  // Unblock when context is cancelled
}
```

**This prevents leaks** because context cancellation provides a guaranteed exit path.

### Common Select Leak

```go
func leak() {
    ch1 := make(chan int)
    ch2 := make(chan int)
    
    go func() {
        select {
        case val := <-ch1:
            fmt.Println(val)
        case val := <-ch2:
            fmt.Println(val)
        // Missing: case <-ctx.Done(): return
        }
    }()
    
    // Function returns, neither ch1 nor ch2 ever receives data
    // Goroutine blocks forever on select
}
```

---

## Channel Closure

### Closing a Channel

```go
ch := make(chan int, 2)
ch <- 1
ch <- 2
close(ch)  // Mark channel as closed
```

### Effects of Closure

**On Receivers**:
```go
val, ok := <-ch
// If channel closed and empty:
//   val = zero value (0 for int)
//   ok = false
```

**On Senders**:
```go
ch <- 42  // PANIC if channel is closed
```

### Closure and Goroutine Leaks

**Closing can unblock receivers**:
```go
ch := make(chan int)
go func() {
    for val := range ch {  // Exits when channel closes
        process(val)
    }
}()
// ... send some data ...
close(ch)  // Unblocks the range loop
```

**But doesn't help senders**:
```go
ch := make(chan int)
go func() {
    ch <- 42  // Blocked
}()
close(ch)  // Doesn't unblock sender; they're still blocked
```

### Close Pattern for Coordination

```go
done := make(chan struct{})

go func() {
    // Do work
    close(done)  // Signal completion
}()

<-done  // Wait for completion
```

**Benefits**:
- Multiple goroutines can wait on `<-done`
- Closing is a broadcast to all receivers
- Simple synchronization primitive

---

## Common Leak Patterns

### Pattern 1: Send Without Receiver

```go
func leakPattern1() {
    ch := make(chan int)
    go func() {
        result := compute()
        ch <- result  // LEAK: No receiver
    }()
}
```

**Fix**: Buffered channel or ensure receiver exists:
```go
func fixed1() {
    ch := make(chan int, 1)  // Buffer size >= sends
    go func() {
        result := compute()
        ch <- result  // OK: Buffer has space
    }()
}
```

### Pattern 2: Receive Without Sender

```go
func leakPattern2() {
    ch := make(chan int)
    go func() {
        val := <-ch  // LEAK: No sender, channel never closed
        process(val)
    }()
}
```

**Fix**: Ensure sender exists or close channel:
```go
func fixed2() {
    ch := make(chan int)
    go func() {
        val := <-ch
        process(val)
    }()
    ch <- 42  // Provide the data
}
```

### Pattern 3: Bidirectional Without Coordination

```go
func leakPattern3() {
    ch1 := make(chan int)
    ch2 := make(chan int)
    
    go func() {
        ch1 <- 1
        val := <-ch2  // Waits for ch2
        process(val)
    }()
    
    go func() {
        val := <-ch1  // Waits for ch1
        ch2 <- 2
    }()
    // Both goroutines block waiting for each other? NO!
    // Actually this works because ch1 <- 1 completes first.
}
```

But this DOES deadlock:
```go
func actualDeadlock() {
    ch1 := make(chan int)
    ch2 := make(chan int)
    
    go func() {
        val := <-ch1  // Waits for ch1
        ch2 <- 2
    }()
    
    go func() {
        val := <-ch2  // Waits for ch2
        ch1 <- 1
    }()
    // Both wait for each other = deadlock
}
```

### Pattern 4: Select Without Escape Hatch

```go
func leakPattern4() {
    ch := make(chan int)
    go func() {
        for {
            select {
            case val := <-ch:
                process(val)
            // LEAK: No way to exit loop
            }
        }
    }()
}
```

**Fix**: Add context case:
```go
func fixed4(ctx context.Context) {
    ch := make(chan int)
    go func() {
        for {
            select {
            case val := <-ch:
                process(val)
            case <-ctx.Done():
                return  // Exit path
            }
        }
    }()
}
```

### Pattern 5: Closed Channel Detection

```go
func leakPattern5() {
    ch := make(chan int)
    go func() {
        for {
            val := <-ch  // Continues receiving zeros after close
            if val == 0 {
                continue  // WRONG: Can't distinguish zero from closed
            }
            process(val)
        }
    }()
    close(ch)  // Goroutine enters infinite loop of zero receives
}
```

**Fix**: Check `ok` value:
```go
func fixed5() {
    ch := make(chan int)
    go func() {
        for {
            val, ok := <-ch
            if !ok {
                return  // Channel closed, exit
            }
            process(val)
        }
    }()
    close(ch)
}
```

Or better, use `range`:
```go
func bestFixed5() {
    ch := make(chan int)
    go func() {
        for val := range ch {  // Exits automatically on close
            process(val)
        }
    }()
    close(ch)
}
```

---

## Summary

### Key Insights

1. **Channels use wait queues** - Blocked goroutines sit in these queues
2. **Unbuffered = synchronous** - Both sides must be ready simultaneously
3. **Buffered = asynchronous** - Decouples sender and receiver timing
4. **Blocking is normal** - But permanent blocking is a leak
5. **Select needs escape** - Always include cancellation case
6. **Close unblocks receivers** - But not senders (they panic)

### Anti-Leak Checklist

When using channels:

- [ ] Is there a guaranteed receiver for every send?
- [ ] Is there a guaranteed sender or close for every receive?
- [ ] If buffered, is buffer size sufficient?
- [ ] Do select statements include `<-ctx.Done()`?
- [ ] Are channels closed when no longer needed?
- [ ] Do receivers check for closure (use `range` or `ok`)?
- [ ] Is there a maximum blocking duration (timeout)?

### Buffer Size Guidelines

```go
// No buffer: Strong synchronization, easy to leak
ch := make(chan T)

// Buffer = 1: Most common for request/response
ch := make(chan T, 1)

// Buffer = N: For N producers or batch processing
ch := make(chan T, numWorkers)

// Large buffer: Decouple producer/consumer rates
ch := make(chan T, 1000)
```

Choose based on:
- Synchronization needs
- Producer/consumer count
- Acceptable memory usage
- Likelihood of leaks if receiver fails

---

## Further Reading

**Related Resources**:
- [Context Pattern](./04-context-pattern.md) - Using context to prevent channel leaks
- [Detection Methods](./05-detection-methods.md) - Finding channel-related leaks
- [Visual Diagrams](./06-visual-diagrams.md) - Channel blocking visualizations

**Go Documentation**:
- [Effective Go - Channels](https://go.dev/doc/effective_go#channels)
- [Go Memory Model](https://go.dev/ref/mem)

**Source Code**:
- `runtime/chan.go` - Channel implementation

---

**Return to**: [Goroutine Leaks README](../README.md)

