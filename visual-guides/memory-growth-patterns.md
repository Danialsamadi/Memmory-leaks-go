# Memory Growth Patterns

**Visual signatures of different memory leak types.**

---

## Pattern Recognition Chart

```
Memory
  ^
  |
  |     Unbounded (spike)        Goroutine (linear)
  |           /\                      /
  |          /  \                    /
  |         /    \__               /
  |        /                      /
  |       /            Long-Lived (step)
  |      /                 ___/
  |     /              ___/
  |    /           ___/
  |   /        ___/
  |  /     ___/     Healthy (flat)
  | /  ___/    ________________________
  |/__/
  +-----------------------------------------> Time
```

---

## Pattern Descriptions

### 1. Healthy (Flat Line)
```
Memory: ____________________________
        stable with minor GC waves
```
- Memory stays within bounds
- GC keeps allocations in check
- No intervention needed

### 2. Goroutine Leak (Linear Growth)
```
Memory:                    /
                         /
                       /
                     /
        ___________/
```
- Steady, predictable growth
- Each goroutine adds ~2-8KB
- Grows until OOM

### 3. Long-Lived Reference (Step Pattern)
```
Memory:            _____
              ____|
         ____|
    ____|
___|
```
- Grows in chunks
- Steps when cache fills
- Plateaus between events

### 4. Unbounded Resource (Spike)
```
Memory:    /\
          /  \
         /    \___
        /
_______/
```
- Rapid spike under load
- May recover partially
- Dangerous during traffic bursts

### 5. Defer Issue (Sawtooth)
```
Memory:  /|  /|  /|
        / | / | / |
       /  |/  |/  |
______/
```
- Grows during loop execution
- Drops when function exits
- Repeated pattern

---

## Quick Diagnosis

| Pattern | Growth Rate | Trigger | Leak Type |
|---------|-------------|---------|-----------|
| Flat | None | N/A | Healthy |
| Linear | Constant | Time | Goroutine |
| Step | Periodic | Events | Long-Lived Ref |
| Spike | Rapid | Load | Unbounded |
| Sawtooth | Cyclic | Loops | Defer |

---

## Monitoring Commands

```bash
# Watch memory over time
watch -n 1 'curl -s localhost:6060/debug/pprof/heap?debug=1 | grep HeapAlloc'

# Plot goroutine growth
while true; do
  echo "$(date +%s) $(curl -s localhost:6060/debug/pprof/goroutine | head -1)"
  sleep 5
done
```

