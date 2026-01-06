# Goroutine Timeline

**Visual lifecycle diagrams showing goroutine states and leak patterns.**

---

## Healthy Goroutine Lifecycle

```
Time -->

Main:     [=====START=====]----[RUNNING]----[=====END=====]
                |                                   |
Worker:         +--[SPAWN]--[WORK]--[DONE]--[EXIT]--+
                                                    
Goroutines:  1 -----> 2 -----> 2 -----> 2 -----> 1
                      ^                         ^
                   created                   cleaned up
```

---

## Leaking Goroutine Pattern

```
Time -->

Main:     [=====START=====]----[RUNNING]----[CONTINUES]---->
                |         |         |
Worker1:        +--[SPAWN]--[BLOCK]--[STUCK]--[STUCK]--[STUCK]-->
Worker2:                  +--[SPAWN]--[BLOCK]--[STUCK]--[STUCK]-->
Worker3:                            +--[SPAWN]--[BLOCK]--[STUCK]-->

Goroutines:  1 --> 2 --> 3 --> 4 --> 5 --> 6 --> ...
                   ^     ^     ^
                 NEVER EXIT - LEAK!
```

---

## Common Blocking Points

```
Goroutine blocked on:

Channel Read (no sender):
    [RUNNING]---> ch <-data ---> [BLOCKED FOREVER]

Channel Write (no receiver):
    [RUNNING]---> ch <- data ---> [BLOCKED FOREVER]

Lock (never released):
    [RUNNING]---> mu.Lock() ---> [BLOCKED FOREVER]

Select (no case ready):
    [RUNNING]---> select{} ---> [BLOCKED FOREVER]
```

---

## Fixed Pattern with Context

```
Time -->

Main:     [START]----[RUNNING]----[CANCEL]----[WAIT]----[END]
              |                       |           |
Worker:       +--[SPAWN]--[WORK]------+-[EXIT]----+
                           |          |
                     ctx.Done() fires |
                                   cleanup

Goroutines:  1 -----> 2 -----> 2 -----> 1
                      ^                 ^
                   created          properly exited
```

---

## Key Indicators

| State | Goroutine Count | Memory | Action |
|-------|-----------------|--------|--------|
| Healthy | Stable | Stable | None |
| Leaking | Growing | Growing | Investigate |
| Fixed | Returns to baseline | Stable | Verified |

