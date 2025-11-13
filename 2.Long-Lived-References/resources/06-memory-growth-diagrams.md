# Memory Growth Diagrams

**Read Time**: 15 minutes

**Related**: [Visual Guides](../../visual-guides/) | [Back to README](../README.md)

---

## Unbounded Cache Growth

```
Memory Usage Over Time:

1000MB|                                              ╱
      |                                            ╱
 800MB|                                          ╱
      |                                        ╱
 600MB|                                      ╱
      |                                    ╱
 400MB|                                  ╱
      |                                ╱
 200MB|                              ╱
      |                            ╱
   0MB|________________________╱_______________
      0min  5min  10min 15min 20min 25min 30min

Pattern: Linear growth
Rate: ~33 MB/min (5000 objects/sec × 5KB)
Cause: No eviction, all objects retained
```

## LRU Cache (Stable)

```
Memory Usage Over Time:

 20MB|════════════════════════════════════════
     |
 15MB|
     |
 10MB|
     |
  5MB|
     |
  0MB|________________________________________
     0min  5min  10min 15min 20min 25min 30min

Pattern: Flat line after stabilization
Stable at: 12 MB (1000 objects × 5KB + overhead)
Cause: LRU eviction maintains constant size
```

## Slice Reslicing Memory Trap

```
Expected vs Actual Memory:

Expected (with proper copying):
100 files × 1KB headers = 0.1 MB

Actual (with reslicing):
100 files × 10MB arrays = 1000 MB

Waste: 999.9 MB (99.99% wasted)

Diagram:
File 1:  [10 MB array] ─┬─ header[0:1024] keeps entire array
File 2:  [10 MB array] ─┤
File 3:  [10 MB array] ─┤
...                      │
File 100:[10 MB array] ─┘  All arrays retained in memory
```

---

**Return to**: [Long-Lived References README](../README.md)

