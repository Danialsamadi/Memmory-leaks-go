# Memory Model Explanation

**Read Time**: 15 minutes

**Related Topics**:
- [GC Behavior](./02-gc-behavior.md)
- [Slice Internals](./03-slice-internals.md)
- [Back to README](../README.md)

---

## Go Memory Allocation

### Stack vs Heap

**Stack**:
- Fast allocation/deallocation
- Automatic cleanup (function returns)
- Limited size (~1-8 MB per goroutine)
- Local variables, function parameters

**Heap**:
- Slower allocation
- Requires garbage collection
- Unlimited (system memory)
- Objects that escape function scope

### Escape Analysis

Go compiler determines whether variables live on stack or heap:

```go
func stackAlloc() {
    x := 42  // Stays on stack
    fmt.Println(x)
}

func heapAlloc() *int {
    x := 42
    return &x  // Escapes to heap (returned)
}
```

View escape analysis:
```bash
go build -gcflags='-m' yourfile.go
```

---

## How References Work

### Reachability

An object is **reachable** if there's a path of references from a root:

```
Roots (always reachable):
  ├─ Global variables
  ├─ Goroutine stacks
  └─ Registers

Object reachable if:
  Root → Reference → Object
```

Objects not reachable = garbage = can be collected.

### Long-Lived References Problem

```go
var globalCache = make(map[string]*Data)

func process(key string) {
    data := fetchData()
    globalCache[key] = data  // Creates long-lived reference
    // data cannot be GC'd while in globalCache
}
```

The `globalCache` is a root (global variable), so everything it references stays in memory.

---

## Memory Leak Definition

**In Go**: When objects remain reachable (and thus uncollectable) longer than intended.

Not a traditional "leak" (memory isn't lost), but practically equivalent because memory grows unbounded.

---

## Further Reading

- [GC Behavior](./02-gc-behavior.md)
- [Production Examples](./07-production-examples.md)

---

**Return to**: [Long-Lived References README](../README.md)

