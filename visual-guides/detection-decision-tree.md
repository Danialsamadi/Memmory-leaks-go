# Memory Leak Detection Decision Tree

**Use this guide when you suspect a memory leak in production.**

---

## Quick Start: What Are You Seeing?

```
START HERE
    |
    v
Is memory growing over time?
    |
    +-- NO --> Check CPU/latency issues instead
    |
    +-- YES
         |
         v
    Is it gradual (hours/days) or rapid (minutes)?
         |
         +-- GRADUAL --> Go to [Slow Leak Detection]
         |
         +-- RAPID --> Go to [Fast Leak Detection]
```

---

## Slow Leak Detection (Hours/Days)

```
Memory grows slowly over hours or days
    |
    v
Check goroutine count: runtime.NumGoroutine()
    |
    +-- GROWING --> Goroutine Leak
    |               See: 1.Goroutine-Leaks-Most-Common/
    |
    +-- STABLE
         |
         v
    Check heap profile: go tool pprof heap
         |
         +-- Large objects in cache/map --> Long-Lived Reference Leak
         |                                  See: 2.Long-Lived-References/
         |
         +-- Many small allocations --> Check allocation sites
         |
         +-- Nothing obvious --> Check for resource leaks
                                 See: 3.Resource-Leaks/
```

### Commands for Slow Leaks

```bash
# 1. Check goroutine count
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | head -5

# 2. Take heap snapshot
curl http://localhost:6060/debug/pprof/heap > heap1.pprof

# 3. Wait 10-30 minutes, take another
curl http://localhost:6060/debug/pprof/heap > heap2.pprof

# 4. Compare (shows what grew)
go tool pprof -base=heap1.pprof heap2.pprof
```

---

## Fast Leak Detection (Minutes)

```
Memory grows rapidly under load
    |
    v
Check goroutine count during spike
    |
    +-- EXPLODING (1000s) --> Unbounded Goroutine Creation
    |                         See: 5.Unbounded-Resources/
    |
    +-- STABLE
         |
         v
    Check for large channel buffers
         |
         +-- YES --> Channel Buffer Leak
         |           See: 5.Unbounded-Resources/examples/channel-buffer-leak/
         |
         +-- NO
              |
              v
         Check file descriptors: lsof -p <pid> | wc -l
              |
              +-- GROWING --> Resource Leak (files/connections)
              |               See: 3.Resource-Leaks/
              |
              +-- STABLE --> Check defer issues
                             See: 4.Defer-Issues/
```

### Commands for Fast Leaks

```bash
# 1. Watch goroutine count in real-time
watch -n 1 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"

# 2. Check file descriptors
lsof -p $(pgrep your-app) | wc -l

# 3. Check network connections
netstat -an | grep $(pgrep your-app) | wc -l

# 4. Quick heap check
curl http://localhost:6060/debug/pprof/heap?debug=1 | head -20
```

---

## Decision Tree by Symptom

### Symptom: OOM Kills

```
Application getting OOM killed
    |
    v
How quickly does it happen?
    |
    +-- Minutes after start --> Check initialization code
    |                           Large allocations at startup?
    |
    +-- Hours after start --> Classic memory leak
    |                         Follow [Slow Leak Detection]
    |
    +-- Only under load --> Unbounded resources
                            Follow [Fast Leak Detection]
```

### Symptom: High Goroutine Count

```
Goroutine count > 1000
    |
    v
Are goroutines in same function?
    |
    +-- YES --> Goroutine leak in that function
    |           Check: channel operations, context cancellation
    |
    +-- NO --> Multiple leak sources
               Profile each function separately
```

**Command to check:**
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | \
  grep -E "^[0-9]+ @" | sort -rn | head -10
```

### Symptom: Growing Heap

```
HeapAlloc growing continuously
    |
    v
What type of objects?
    |
    +-- Strings --> String concatenation in loops?
    |               Substring keeping large strings alive?
    |
    +-- Slices --> Slice reslicing trap?
    |              See: 2.Long-Lived-References/examples/reslicing-leak/
    |
    +-- Maps --> Unbounded cache?
    |            See: 2.Long-Lived-References/examples/cache-leak/
    |
    +-- Channels --> Large channel buffers?
                     See: 5.Unbounded-Resources/examples/channel-buffer-leak/
```

---

## Profiling Commands Cheat Sheet

### Basic Health Check

```bash
# Quick memory stats
curl http://localhost:6060/debug/pprof/heap?debug=1 | grep -E "^# (Alloc|Sys|HeapAlloc)"

# Goroutine count
curl -s http://localhost:6060/debug/pprof/goroutine | head -1

# Top memory consumers
go tool pprof -top http://localhost:6060/debug/pprof/heap
```

### Detailed Analysis

```bash
# Interactive heap analysis
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap

# Goroutine stack traces
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Allocation profiling (where memory is allocated)
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/allocs
```

### Comparison (Before/After)

```bash
# Take baseline
curl http://localhost:6060/debug/pprof/heap > before.pprof

# Wait for leak to grow
sleep 300

# Take current
curl http://localhost:6060/debug/pprof/heap > after.pprof

# Compare (shows only growth)
go tool pprof -base=before.pprof -http=:8081 after.pprof
```

---

## Quick Reference: Leak Type Indicators

| Indicator | Likely Leak Type | Section |
|-----------|------------------|---------|
| Goroutines growing | Goroutine leak | 1.Goroutine-Leaks-Most-Common/ |
| HeapObjects growing, stable types | Long-lived references | 2.Long-Lived-References/ |
| File descriptors growing | Resource leak | 3.Resource-Leaks/ |
| Memory grows only in loops | Defer issues | 4.Defer-Issues/ |
| Rapid growth under load | Unbounded resources | 5.Unbounded-Resources/ |

---

## Emergency Response

If your production system is about to crash:

### 1. Immediate Actions (< 1 minute)

```bash
# Capture state before restart
curl http://localhost:6060/debug/pprof/heap > emergency-heap.pprof
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > emergency-goroutines.txt
```

### 2. Quick Diagnosis (1-5 minutes)

```bash
# What's consuming memory?
go tool pprof -top emergency-heap.pprof | head -20

# What goroutines are stuck?
grep -E "^goroutine [0-9]+" emergency-goroutines.txt | wc -l
```

### 3. Temporary Mitigation

- Restart the service (buys time)
- Scale horizontally (distribute load)
- Enable rate limiting (reduce pressure)
- Reduce traffic to affected endpoints

### 4. Root Cause Analysis (after stabilization)

Use the profiles captured above to identify the leak source, then apply the appropriate fix from the relevant section.

---

## Next Steps

Based on your diagnosis:

- **Goroutine Leak**: [1.Goroutine-Leaks-Most-Common/](../1.Goroutine-Leaks-Most-Common/)
- **Long-Lived References**: [2.Long-Lived-References/](../2.Long-Lived-References/)
- **Resource Leaks**: [3.Resource-Leaks/](../3.Resource-Leaks/)
- **Defer Issues**: [4.Defer-Issues/](../4.Defer-Issues/)
- **Unbounded Resources**: [5.Unbounded-Resources/](../5.Unbounded-Resources/)
- **Need pprof help**: [tools-setup/pprof-complete-guide.md](../tools-setup/pprof-complete-guide.md)

