# Memory Leak Flowchart

**Quick decision tree to identify which type of memory leak you're dealing with.**

---

## Main Flowchart

```
                    MEMORY ISSUE DETECTED
                            |
                            v
                Is goroutine count growing?
                     /            \
                   YES             NO
                   /                \
                  v                  v
        GOROUTINE LEAK        Is heap growing?
        Section 1              /          \
                             YES           NO
                             /              \
                            v                v
                  Are objects in cache?    Check FDs/connections
                     /        \               |
                   YES         NO             v
                   /            \        RESOURCE LEAK
                  v              v        Section 3
        LONG-LIVED REF    Is it under load?
        Section 2            /       \
                           YES        NO
                           /           \
                          v             v
                UNBOUNDED RESOURCE   DEFER ISSUE
                Section 5            Section 4
```

---

## Quick Identification Table

| Symptom | Check Command | Likely Type |
|---------|---------------|-------------|
| Goroutines climbing | `runtime.NumGoroutine()` | Goroutine Leak |
| HeapObjects growing | `pprof heap` | Long-Lived Reference |
| FD count increasing | `lsof -p <pid>` | Resource Leak |
| Memory spikes in loops | Stack trace analysis | Defer Issue |
| Rapid growth under load | Goroutine + heap | Unbounded Resource |

---

## Section Links

- [1. Goroutine Leaks](../1.Goroutine-Leaks-Most-Common/)
- [2. Long-Lived References](../2.Long-Lived-References/)
- [3. Resource Leaks](../3.Resource-Leaks/)
- [4. Defer Issues](../4.Defer-Issues/)
- [5. Unbounded Resources](../5.Unbounded-Resources/)

