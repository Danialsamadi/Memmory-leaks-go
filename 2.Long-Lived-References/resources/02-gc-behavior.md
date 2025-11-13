# GC Behavior

**Read Time**: 20 minutes

**Related**: [Memory Model](./01-memory-model-explanation.md) | [Back to README](../README.md)

---

## Go Garbage Collector

Go uses a **concurrent mark-and-sweep** collector:
- Runs concurrently with application
- Pauses are typically <1ms
- Automatically triggered based on heap growth

### Mark Phase
1. Stop-the-world (STW) briefly
2. Mark all reachable objects
3. Resume application

### Sweep Phase
4. Reclaim unmarked objects
5. Return memory to allocator

### Why Leaked Objects Aren't Collected

If an object is **reachable** (referenced from a root), it's marked as live and NOT collected.

```go
var cache = make(map[string]*Object)  // Root

cache["key"] = obj  // obj is now reachable via cache
// GC will never collect obj while it's in cache
```

---

## GC Tuning

```go
import "runtime/debug"

// Set GC target percentage
debug.SetGCPercent(100)  // Default: trigger GC when heap doubles

// Force GC
runtime.GC()
```

---

**Return to**: [Long-Lived References README](../README.md)

