# Slice Internals

**Read Time**: 15 minutes

**Related**: [Memory Model](./01-memory-model-explanation.md) | [Back to README](../README.md)

---

## Slice Structure

A slice is a descriptor containing:
```go
type slice struct {
    ptr *array  // Pointer to underlying array
    len int     // Current length
    cap int     // Capacity
}
```

## The Reslicing Trap

```go
bigArray := make([]byte, 10*1024*1024)  // 10 MB
smallSlice := bigArray[:1024]           // 1 KB view

// smallSlice.ptr still points to the 10 MB array!
// GC cannot free the 10 MB while smallSlice exists
```

## Solution: Copy

```go
bigArray := make([]byte, 10*1024*1024)
smallSlice := make([]byte, 1024)
copy(smallSlice, bigArray[:1024])  // Independent copy

// Now bigArray can be GC'd
```

## When to Copy vs Reference

**Use reference** (reslicing):
- Short-lived slices
- You need the full array anyway
- Performance critical (avoid copying)

**Use copy**:
- Long-lived small slices from large arrays
- Want to release the large array
- Independent data needed

---

**Return to**: [Long-Lived References README](../README.md)

