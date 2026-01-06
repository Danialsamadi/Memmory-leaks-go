# pprof Complete Guide

**The essential tool for Go profiling and leak detection**

---

## Table of Contents

1. [What is pprof](#what-is-pprof)
2. [Quick Setup](#quick-setup)
3. [Profile Types](#profile-types)
4. [Collection Methods](#collection-methods)
5. [Analysis Commands](#analysis-commands)
6. [Web UI Guide](#web-ui-guide)
7. [Production Usage](#production-usage)
8. [Common Workflows](#common-workflows)

---

## What is pprof

pprof is Go's built-in profiling tool that provides:
- **CPU profiling**: Where is time spent?
- **Memory profiling**: Where is memory allocated?
- **Goroutine profiling**: What goroutines exist and why?
- **Block profiling**: Where are goroutines blocking?
- **Mutex profiling**: Where is lock contention?

---

## Quick Setup

### Option 1: HTTP Server (Recommended)

```go
package main

import (
    "log"
    "net/http"
    _ "net/http/pprof" // Import for side effects
)

func main() {
    // Start pprof server on separate port
    go func() {
        log.Println("pprof server: http://localhost:6060/debug/pprof/")
        log.Fatal(http.ListenAndServe("localhost:6060", nil))
    }()
    
    // Your application code here
    select {}
}
```

### Option 2: Existing HTTP Server

```go
import (
    "net/http"
    "net/http/pprof"
)

func main() {
    mux := http.NewServeMux()
    
    // Your routes
    mux.HandleFunc("/api/", apiHandler)
    
    // pprof routes
    mux.HandleFunc("/debug/pprof/", pprof.Index)
    mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
    mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
    mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
    mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
    
    http.ListenAndServe(":8080", mux)
}
```

### Option 3: Programmatic (No HTTP)

```go
import (
    "os"
    "runtime/pprof"
)

func main() {
    // CPU profile
    f, _ := os.Create("cpu.pprof")
    pprof.StartCPUProfile(f)
    defer pprof.StopCPUProfile()
    
    // Your code here
    doWork()
    
    // Heap profile
    f2, _ := os.Create("heap.pprof")
    pprof.WriteHeapProfile(f2)
    f2.Close()
}
```

---

## Profile Types

### 1. Goroutine Profile

Shows all current goroutines and their stack traces.

```bash
# Collect
curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof

# View count only
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1

# View with stack traces
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

**Use for**: Detecting goroutine leaks, finding blocked goroutines

### 2. Heap Profile

Shows current memory allocations.

```bash
# Collect
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# View summary
curl http://localhost:6060/debug/pprof/heap?debug=1
```

**Use for**: Finding memory leaks, large allocations

### 3. Allocs Profile

Shows all allocations since program start (cumulative).

```bash
curl http://localhost:6060/debug/pprof/allocs > allocs.pprof
```

**Use for**: Finding allocation hotspots, optimization

### 4. CPU Profile

Shows where CPU time is spent.

```bash
# Collect 30 seconds of CPU data
curl "http://localhost:6060/debug/pprof/profile?seconds=30" > cpu.pprof
```

**Use for**: Performance optimization, finding slow code

### 5. Block Profile

Shows where goroutines block on synchronization.

```bash
# Enable block profiling first
runtime.SetBlockProfileRate(1)

# Then collect
curl http://localhost:6060/debug/pprof/block > block.pprof
```

**Use for**: Finding lock contention, channel blocking

### 6. Mutex Profile

Shows mutex contention.

```bash
# Enable mutex profiling first
runtime.SetMutexProfileFraction(1)

# Then collect
curl http://localhost:6060/debug/pprof/mutex > mutex.pprof
```

**Use for**: Finding lock contention

---

## Collection Methods

### Method 1: curl (Simple)

```bash
# Goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof

# Heap profile
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# CPU profile (30 seconds)
curl "http://localhost:6060/debug/pprof/profile?seconds=30" > cpu.pprof
```

### Method 2: go tool pprof (Direct)

```bash
# Interactive mode
go tool pprof http://localhost:6060/debug/pprof/heap

# Web UI mode
go tool pprof -http=:8081 http://localhost:6060/debug/pprof/heap
```

### Method 3: Comparison (Before/After)

```bash
# Collect baseline
curl http://localhost:6060/debug/pprof/heap > before.pprof

# Wait for suspected leak
sleep 60

# Collect after
curl http://localhost:6060/debug/pprof/heap > after.pprof

# Compare (shows growth)
go tool pprof -base=before.pprof after.pprof
```

---

## Analysis Commands

### Interactive Mode Commands

```bash
$ go tool pprof heap.pprof
(pprof) help           # Show all commands

# Top allocators
(pprof) top            # Top 10 by flat
(pprof) top20          # Top 20
(pprof) top -cum       # Top by cumulative

# View specific function
(pprof) list functionName
(pprof) list main.     # All functions in main package

# Call graph
(pprof) web            # Open in browser
(pprof) svg            # Generate SVG
(pprof) png            # Generate PNG

# Filter
(pprof) top -focus=mypackage
(pprof) top -ignore=runtime

# Memory modes
(pprof) alloc_space    # Total allocated bytes
(pprof) alloc_objects  # Total allocated objects
(pprof) inuse_space    # Currently in use bytes
(pprof) inuse_objects  # Currently in use objects
```

### Command Line Options

```bash
# Direct to web UI
go tool pprof -http=:8081 heap.pprof

# Generate specific output
go tool pprof -svg heap.pprof > heap.svg
go tool pprof -png heap.pprof > heap.png
go tool pprof -text heap.pprof

# Compare profiles
go tool pprof -base=before.pprof after.pprof

# Filter by function
go tool pprof -focus=mypackage heap.pprof

# Show source
go tool pprof -source_path=/path/to/src heap.pprof
```

---

## Web UI Guide

### Starting Web UI

```bash
go tool pprof -http=:8081 heap.pprof
```

Opens browser at `http://localhost:8081`

### Views Available

1. **Top**: List of functions by resource usage
2. **Graph**: Call graph visualization
3. **Flame Graph**: Hierarchical view of call stacks
4. **Peek**: Source code with annotations
5. **Source**: Full source with line-by-line metrics
6. **Disassemble**: Assembly with metrics

### Navigation Tips

- Click function names to focus
- Use "Refine" menu to filter
- Toggle between flat/cumulative
- Use search box to find functions

### Flame Graph Interpretation

```
Width = Resource usage (time/memory)
Height = Call stack depth
Color = Usually random, sometimes indicates package

Wide bars at top = Direct consumers
Wide bars at bottom = Root causes
```

---

## Production Usage

### Secure pprof Endpoint

```go
func setupPprof(adminPassword string) {
    mux := http.NewServeMux()
    
    // Add basic auth
    mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
        user, pass, ok := r.BasicAuth()
        if !ok || user != "admin" || pass != adminPassword {
            w.Header().Set("WWW-Authenticate", `Basic realm="pprof"`)
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        pprof.Index(w, r)
    })
    
    // Listen on internal port only
    go http.ListenAndServe("127.0.0.1:6060", mux)
}
```

### Automated Collection

```go
func collectProfiles(ctx context.Context, interval time.Duration, dir string) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case t := <-ticker.C:
            timestamp := t.Format("20060102-150405")
            
            // Collect heap profile
            f, _ := os.Create(fmt.Sprintf("%s/heap-%s.pprof", dir, timestamp))
            pprof.WriteHeapProfile(f)
            f.Close()
            
            // Collect goroutine profile
            f, _ = os.Create(fmt.Sprintf("%s/goroutine-%s.pprof", dir, timestamp))
            pprof.Lookup("goroutine").WriteTo(f, 0)
            f.Close()
        }
    }
}
```

### Conditional Profiling

```go
var (
    profilingEnabled = flag.Bool("profile", false, "Enable profiling")
)

func main() {
    flag.Parse()
    
    if *profilingEnabled {
        go func() {
            log.Println("pprof enabled on :6060")
            http.ListenAndServe("localhost:6060", nil)
        }()
    }
    
    // Application code
}
```

---

## Common Workflows

### Workflow 1: Detecting Goroutine Leaks

```bash
# 1. Get baseline goroutine count
curl -s http://localhost:6060/debug/pprof/goroutine | head -1
# goroutine profile: total 10

# 2. Run your workload

# 3. Check goroutine count again
curl -s http://localhost:6060/debug/pprof/goroutine | head -1
# goroutine profile: total 1010  ← Growing!

# 4. Collect detailed profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof

# 5. Analyze
go tool pprof -http=:8081 goroutine.pprof

# 6. Look for large counts in same function
```

### Workflow 2: Finding Memory Leaks

```bash
# 1. Collect baseline heap
curl http://localhost:6060/debug/pprof/heap > heap1.pprof

# 2. Wait (e.g., 5 minutes)
sleep 300

# 3. Collect second heap
curl http://localhost:6060/debug/pprof/heap > heap2.pprof

# 4. Compare to see growth
go tool pprof -base=heap1.pprof heap2.pprof

# 5. In pprof:
(pprof) top
(pprof) list suspiciousFunction
```

### Workflow 3: CPU Optimization

```bash
# 1. Collect CPU profile during load
curl "http://localhost:6060/debug/pprof/profile?seconds=30" > cpu.pprof

# 2. Analyze
go tool pprof -http=:8081 cpu.pprof

# 3. Look at flame graph for hot paths
# 4. Check "top" for expensive functions
# 5. Use "list" to see line-by-line costs
```

### Workflow 4: Continuous Monitoring

```bash
# Watch goroutine count
watch -n 5 "curl -s http://localhost:6060/debug/pprof/goroutine | head -1"

# Watch heap size
watch -n 5 "curl -s http://localhost:6060/debug/pprof/heap?debug=1 | grep 'Alloc ='"
```

---

## Troubleshooting

### "No source code available"

```bash
# Provide source path
go tool pprof -source_path=/path/to/src heap.pprof
```

### "Profile is empty"

- Check that profiling endpoint is accessible
- For CPU profile, ensure workload is running during collection
- For block/mutex profiles, ensure they're enabled

### "Connection refused"

- Check pprof server is running
- Verify port number
- Check if bound to localhost vs 0.0.0.0

### Large profile files

```bash
# Compress profiles
gzip heap.pprof

# pprof can read compressed files
go tool pprof heap.pprof.gz
```

---

## Quick Reference

| Profile | URL | Use For |
|---------|-----|---------|
| Goroutine | `/debug/pprof/goroutine` | Goroutine leaks |
| Heap | `/debug/pprof/heap` | Memory leaks |
| Allocs | `/debug/pprof/allocs` | Allocation hotspots |
| CPU | `/debug/pprof/profile?seconds=30` | Performance |
| Block | `/debug/pprof/block` | Blocking operations |
| Mutex | `/debug/pprof/mutex` | Lock contention |
| Trace | `/debug/pprof/trace?seconds=5` | Execution trace |

---

**Return to**: [Tools Setup](./README.md)

