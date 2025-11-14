# Slice Internals: Understanding Go Slices and Memory

**Read Time**: 20 minutes

**Prerequisites**: Understanding of [Memory Model](./01-memory-model-explanation.md)

**Related Topics**: 
- [Memory Model Explanation](./01-memory-model-explanation.md)
- [GC Behavior](./02-gc-behavior.md)
- [Back to README](../README.md)

---

## Table of Contents

1. [Slice Structure and Layout](#slice-structure-and-layout)
2. [How Reslicing Works](#how-reslicing-works)
3. [The Reslicing Memory Trap](#the-reslicing-memory-trap)
4. [When Copying Is Required](#when-copying-is-required)
5. [Pointer Slices: The Worst Case](#pointer-slices-the-worst-case)
6. [Best Practices](#best-practices)
7. [Summary](#summary)

---

## Slice Structure and Layout

### The Three-Field Structure

A slice in Go is **not** an array. It's a small data structure (24 bytes on 64-bit systems) that *describes* an array:

```go
// Internal representation (not actual Go code)
type slice struct {
    ptr unsafe.Pointer  // Pointer to underlying array (8 bytes)
    len int             // Current length (8 bytes)
    cap int             // Capacity (8 bytes)
}
// Total: 24 bytes on 64-bit systems
```

### Visual Representation

```
Slice variable (24 bytes on stack/heap):
┌─────────────────────────────────────┐
│  ptr: 0x1040a000                    │ ────┐
│  len: 5                             │     │
│  cap: 10                            │     │
└─────────────────────────────────────┘     │
                                            │
                                            ▼
Underlying array (on heap):
┌───┬───┬───┬───┬───┬───┬───┬───┬───┬───┐
│ 1 │ 2 │ 3 │ 4 │ 5 │   │   │   │   │   │
└───┴───┴───┴───┴───┴───┴───┴───┴───┴───┘
 0   1   2   3   4   5   6   7   8   9
 └─────────────┘     └─────────────────┘
   len = 5           unused capacity = 5
```

### Creating Slices: What Actually Happens

```go
// Scenario 1: make()
s1 := make([]int, 5, 10)

// What happens:
// 1. Allocate array of 10 ints on heap (80 bytes)
// 2. Create slice descriptor:
//    ptr = address of array
//    len = 5
//    cap = 10
// 3. Place descriptor on stack (or heap if it escapes)


// Scenario 2: Slice literal
s2 := []int{1, 2, 3}

// What happens:
// 1. Allocate array of 3 ints on heap (24 bytes)
// 2. Initialize with values {1, 2, 3}
// 3. Create slice descriptor:
//    ptr = address of array
//    len = 3
//    cap = 3


// Scenario 3: Slicing an array
arr := [10]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
s3 := arr[2:7]

// What happens:
// 1. NO new array allocation
// 2. Create slice descriptor:
//    ptr = address of arr[2]
//    len = 5 (7 - 2)
//    cap = 8 (10 - 2)
```

---

## How Reslicing Works

### Reslicing Creates New Descriptors, Not New Arrays

```go
original := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
sub1 := original[2:7]
sub2 := original[5:9]
```

**Memory Layout**:
```
original slice descriptor:
┌─────────────────────────┐
│ ptr: 0x1000             │ ─────┐
│ len: 10                 │      │
│ cap: 10                 │      │
└─────────────────────────┘      │
                                 │
sub1 slice descriptor:           │
┌─────────────────────────┐      │
│ ptr: 0x1008 (0x1000+8)  │ ─────┤
│ len: 5                  │      │
│ cap: 8                  │      │
└─────────────────────────┘      │
                                 │
sub2 slice descriptor:           │
┌─────────────────────────┐      │
│ ptr: 0x1014 (0x1000+20) │ ─────┤
│ len: 4                  │      │
│ cap: 5                  │      │
└─────────────────────────┘      │
                                 ▼
Underlying array (SINGLE SHARED ARRAY):
┌───┬───┬───┬───┬───┬───┬───┬───┬───┬───┐
│ 0 │ 1 │ 2 │ 3 │ 4 │ 5 │ 6 │ 7 │ 8 │ 9 │
└───┴───┴───┴───┴───┴───┴───┴───┴───┴───┘
 ▲       ▲           ▲
 │       │           │
 │       │           └── sub2 starts here (index 5)
 │       └────────────── sub1 starts here (index 2)
 └────────────────────── original starts here (index 0)
```

**Key Point**: All three slices share the **same underlying array**. Modifying one affects others:

```go
original[5] = 999

fmt.Println(original)  // [0 1 2 3 4 999 6 7 8 9]
fmt.Println(sub1)      // [2 3 4 999 6] ← affected!
fmt.Println(sub2)      // [999 6 7 8]   ← affected!
```

### Reslicing Does NOT Allocate Memory

```go
func benchmark() {
    data := make([]byte, 1_000_000)  // 1 MB allocation
    
    for i := 0; i < 1_000_000; i++ {
        sub := data[i:i+100]  // NO allocation
        process(sub)          // Just copies 24 bytes
    }
    // Total allocations: 1 (just the initial array)
}
```

This is why reslicing is **fast** but can cause **memory leaks**.

---

## The Reslicing Memory Trap

### The Problem Scenario

```go
func readFileHeader(filename string) []byte {
    data, _ := os.ReadFile(filename)  // 100 MB file
    header := data[:1024]              // First 1 KB
    return header                      // Return just the header
}

// Caller
func process() {
    headers := make([][]byte, 0)
    
    for _, file := range files {
        header := readFileHeader(file)
        headers = append(headers, header)
    }
    
    // We have 100 headers (100 KB of useful data)
    // But memory usage: 100 × 100 MB = 10 GB!
}
```

### Why It Leaks

```
After reading first file:
┌────────────────────────────────┐
│  header slice (returned)       │
│  ptr: points to start of array │
│  len: 1024                     │
│  cap: 100 MB / elementSize     │
└────────────────────────────────┘
              │
              ▼
┌──────────────────────────────────────────────────────────┐
│  Underlying 100 MB array                                 │
│  ┌─────┬─────┬─────┬──────────────────────────────────┐ │
│  │ HD  │ HD  │ HD  │   Rest of file (99.999 MB)      │ │
│  │ R1  │ R2  │ R3  │   NOT USED                       │ │
│  └─────┴─────┴─────┴──────────────────────────────────┘ │
│   ▲                                                      │
│   │                                                      │
│   └── header slice points here                          │
└──────────────────────────────────────────────────────────┘

GC Reasoning:
1. headers (slice) is reachable
2. headers[0] (header slice) is reachable
3. header.ptr points to array
4. Array is reachable
5. Conclusion: Keep 100 MB array (even though only 1 KB used)
```

### Real-World Impact

```go
// Web server processing uploads
func handleUpload(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)  // 10 MB upload
    
    // Extract just the metadata (100 bytes)
    metadata := body[:100]
    
    // Store in session (long-lived)
    sessions[sessionID] = Session{
        Metadata: metadata,  // Holds reference to 10 MB!
    }
    
    // After 1000 uploads: 10 GB memory usage
    // But only 100 KB is "useful" data
}
```

### The Fix: Copy Only What You Need

```go
func readFileHeaderFixed(filename string) []byte {
    data, _ := os.ReadFile(filename)  // 100 MB file
    
    // Option 1: Manual copy
    header := make([]byte, 1024)
    copy(header, data[:1024])
    
    // Option 2: bytes.Clone (Go 1.20+)
    header := bytes.Clone(data[:1024])
    
    // Now 'data' array has no references
    // GC can reclaim the 100 MB
    
    return header  // Only 1 KB of actual memory
}
```

**Memory comparison**:
```
Without copy:
  100 files × 100 MB = 10 GB

With copy:
  100 files × 1 KB = 100 KB
  
Savings: 99.999% reduction!
```

---

## When Copying Is Required

### Decision Tree

```
Question 1: Is the slice long-lived?
  ├─ No  → Reslicing OK (short-lived, will be GC'd soon)
  └─ Yes → Continue to Question 2

Question 2: Is the slice much smaller than the backing array?
  ├─ No  → Reslicing OK (using most of array anyway)
  └─ Yes → Continue to Question 3

Question 3: Will the backing array be used elsewhere?
  ├─ Yes → Reslicing OK (array is needed)
  └─ No  → COPY REQUIRED (to free backing array)
```

### Examples of Each Case

**Case 1: Reslicing OK (Short-lived)**
```go
func processLine(data []byte) {
    // Short-lived within function
    line := data[:findNewline(data)]
    parseLine(line)
    // line goes out of scope
    // No memory leak
}
```

**Case 2: Reslicing OK (Similar size)**
```go
func getRows(table []Row) []Row {
    // Removing just a few rows
    return table[2:len(table)-2]
    // Using 99% of array, leak is minimal
}
```

**Case 3: Reslicing OK (Array used elsewhere)**
```go
func splitData(data []byte) ([]byte, []byte) {
    mid := len(data) / 2
    // Both slices are used
    return data[:mid], data[mid:]
    // Array is fully utilized
}
```

**Case 4: COPY REQUIRED**
```go
func extractTitle(document []byte) []byte {
    // Long-lived: stored in cache
    // Small: 100 bytes from 10 MB document
    // Unique: document not used elsewhere
    
    titleBytes := document[0:100]
    
    // Must copy!
    title := make([]byte, 100)
    copy(title, titleBytes)
    return title
}
```

---

## Pointer Slices: The Worst Case

### The Problem Magnified

When slices contain **pointers**, the issue is even worse:

```go
type Record struct {
    ID   int
    Data [10000]byte  // 10 KB per record
}

func leak() {
    records := make([]*Record, 1_000_000)
    
    for i := 0; i < 1_000_000; i++ {
        records[i] = &Record{ID: i}
    }
    
    // Keep only the last 10 records
    records = records[len(records)-10:]
    
    // What's in memory:
    // - records slice (24 bytes)
    // - Backing array of 1M pointers (8 MB)
    // - All 1M Record objects (10 GB)
    //
    // Even though we "truncated" to 10 records!
}
```

### Why ALL Objects Are Retained

```
After truncation: records = records[999990:]

records slice (what we see):
┌─────────────────────────┐
│ ptr: (to index 999990)  │
│ len: 10                 │
│ cap: 10                 │
└─────────────────────────┘
         │
         ▼
Backing pointer array (8 MB):
┌─────┬─────┬─────┬─────┬────────┬─────┬─────┬─────┐
│ *R0 │ *R1 │ *R2 │ ... │ *R999989│*R999990│...│*R999999│
└──┬──┴──┬──┴──┬──┴─────┴───┬────┴────┬───┴───┴────┬───┘
   │     │     │             │         │            │
   ▼     ▼     ▼             ▼         ▼            ▼
  R0    R1    R2   ...    R999989  R999990  ...  R999999
  10KB  10KB  10KB         10KB      10KB         10KB

GC sees ALL pointers in the backing array!
Even though our slice descriptor only points to the last 10.
```

### The Correct Fix for Pointer Slices

```go
func correctTruncation(records []*Record, keepCount int) []*Record {
    // Step 1: Nil out pointers we're not keeping
    for i := 0; i < len(records)-keepCount; i++ {
        records[i] = nil  // Break reference
    }
    
    // Step 2: Truncate
    records = records[len(records)-keepCount:]
    
    // Now GC can collect the nil'd objects
    // Only the last keepCount objects are referenced
    
    // Even better: Create new slice
    result := make([]*Record, keepCount)
    copy(result, records[len(records)-keepCount:])
    return result
    // Original backing array can be GC'd entirely
}
```

### Performance Comparison

```go
// Benchmark: Truncating 1M records to 10

// Method 1: Direct truncation (LEAKY)
func truncateLeak(records []*Record) []*Record {
    return records[len(records)-10:]
}
// Memory: 10 GB retained
// Time: ~1 ns (just pointer arithmetic)

// Method 2: Nil then truncate
func truncateNil(records []*Record) []*Record {
    for i := 0; i < len(records)-10; i++ {
        records[i] = nil
    }
    return records[len(records)-10:]
}
// Memory: 100 KB retained
// Time: ~1 ms (must iterate and nil out)

// Method 3: Copy to new slice
func truncateCopy(records []*Record) []*Record {
    result := make([]*Record, 10)
    copy(result, records[len(records)-10:])
    return result
}
// Memory: 100 KB retained
// Time: ~100 ns (allocate + copy 10 pointers)

// Winner: Method 3 (best memory + good performance)
```

---

## Best Practices

### Practice 1: Use bytes.Clone for Byte Slices (Go 1.20+)

```go
// Old way
func oldExtract(data []byte, start, end int) []byte {
    result := make([]byte, end-start)
    copy(result, data[start:end])
    return result
}

// New way (Go 1.20+)
import "bytes"

func newExtract(data []byte, start, end int) []byte {
    return bytes.Clone(data[start:end])
}

// Even better: Clear intent
func extractIndependent(data []byte, start, end int) []byte {
    // Clone makes independence explicit
    return bytes.Clone(data[start:end])
}
```

### Practice 2: Document Sharing vs Copying

```go
// Good: Clear documentation
// extract returns a slice that shares the underlying array.
// Modifying the result will affect the original.
func extract(data []byte, start, end int) []byte {
    return data[start:end]
}

// Good: Clear documentation
// extractCopy returns an independent copy.
// Safe to modify without affecting the original.
func extractCopy(data []byte, start, end int) []byte {
    return bytes.Clone(data[start:end])
}
```

### Practice 3: Nil Out Pointer Slices Before Truncating

```go
// WRONG
func removeFirst(items []*Item, n int) []*Item {
    return items[n:]  // Leaks first n items
}

// CORRECT
func removeFirst(items []*Item, n int) []*Item {
    // Clear references
    for i := 0; i < n; i++ {
        items[i] = nil
    }
    return items[n:]
}

// EVEN BETTER (for large truncations)
func removeFirst(items []*Item, n int) []*Item {
    result := make([]*Item, len(items)-n)
    copy(result, items[n:])
    return result
}
```

### Practice 4: Use Full Slice Expression for Safety

```go
// Vulnerable to append overwrites
func getSub(data []int) []int {
    return data[0:5]  // cap is full length of data
}

// Protected from append overwrites
func getSubSafe(data []int) []int {
    return data[0:5:5]  // cap limited to 5
}

// Explanation:
// data[low:high:max]
//   low: starting index
//   high: length = high - low
//   max: capacity = max - low
```

Example of the difference:
```go
original := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

// Vulnerable
sub1 := original[0:5]
sub1 = append(sub1, 999)  // Overwrites original[5]!

// Protected
sub2 := original[0:5:5]
sub2 = append(sub2, 999)  // Allocates new array
```

---

## Summary

### Key Points

1. **Slices are descriptors** (24 bytes) that point to arrays
2. **Reslicing is cheap** (just creates new descriptor)
3. **Reslicing shares arrays** (can cause memory leaks)
4. **Copy when needed** (long-lived small slices from large arrays)
5. **Nil out pointers** (before truncating pointer slices)
6. **Use bytes.Clone** (for byte slices in Go 1.20+)

### Decision Matrix

| Scenario | Action | Reason |
|----------|--------|--------|
| Short-lived slice | Reslice | Performance, no leak risk |
| Long-lived small from large | Copy | Prevent memory leak |
| Long-lived similar size | Reslice | No significant leak |
| Pointer slice truncation | Nil + copy | Prevent object retention |
| Byte slice extraction | bytes.Clone | Clear intent, safe |
| Temporary processing | Reslice | Performance critical |

### Common Mistakes

```go
// ❌ Mistake 1: Returning reslice from large array
func readFile(name string) []byte {
    data, _ := os.ReadFile(name)  // 100 MB
    return data[:1000]             // Keeps 100 MB alive
}

// ✅ Fix
func readFile(name string) []byte {
    data, _ := os.ReadFile(name)
    return bytes.Clone(data[:1000])  // Only 1 KB retained
}


// ❌ Mistake 2: Storing resliced data in cache
var cache = make(map[string][]byte)
func cache(key string, data []byte) {
    cache[key] = data[0:100]  // Keeps full data array
}

// ✅ Fix
var cache = make(map[string][]byte)
func cacheData(key string, data []byte) {
    cache[key] = bytes.Clone(data[0:100])  // Independent copy
}


// ❌ Mistake 3: Truncating pointer slice
func keep Last(items []*Item) []*Item {
    return items[len(items)-10:]  // Leaks all items
}

// ✅ Fix
func keepLast(items []*Item) []*Item {
    result := make([]*Item, 10)
    copy(result, items[len(items)-10:])
    return result  // Only 10 items referenced
}
```

---

## Next Steps

- **Study cache patterns**: Read [Cache Patterns](./04-cache-patterns.md)
- **Learn eviction strategies**: Read [Eviction Strategies](./05-eviction-strategies.md)
- **See visual examples**: Read [Memory Growth Diagrams](./06-memory-growth-diagrams.md)
- **Real production cases**: Read [Production Examples](./07-production-examples.md)

---

**Return to**: [Long-Lived References README](../README.md)
