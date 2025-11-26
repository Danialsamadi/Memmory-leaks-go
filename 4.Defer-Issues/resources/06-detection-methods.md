# Detection Methods for Defer Issues

**Reading Time**: 20 minutes

---

## Introduction

Detecting defer-in-loop issues requires a combination of static analysis, runtime monitoring, and code review practices. This document covers all available detection methods and how to integrate them into your workflow.

---

## Method 1: Static Analysis with golangci-lint

The most effective way to catch defer issues before they reach production.

### Installation

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Or via brew (macOS)
brew install golangci-lint
```

### Configuration

Create `.golangci.yml` in your project root:

```yaml
linters:
  enable:
    - gocritic
    - gosec
    - errcheck

linters-settings:
  gocritic:
    enabled-checks:
      - deferInLoop
      - deferUnlambda
      - evalOrder

  errcheck:
    check-blank: true
    check-type-assertions: true
    exclude-functions:
      - (io.Closer).Close  # Optional: ignore unchecked Close()
```

### Running

```bash
# Run all enabled linters
golangci-lint run ./...

# Run only gocritic
golangci-lint run --enable=gocritic --disable-all ./...

# Show only defer issues
golangci-lint run ./... 2>&1 | grep -i defer
```

### Sample Output

```
file.go:45:3: deferInLoop: defer in a loop may lead to resource leaks (gocritic)
file.go:78:3: deferUnlambda: defered func should be a simple function call (gocritic)
```

### CI Integration

```yaml
# .github/workflows/lint.yml
name: Lint
on: [push, pull_request]

jobs:
  golangci-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: golangci/golangci-lint-action@v3
        with:
          version: latest
          args: --enable=gocritic
```

---

## Method 2: Custom go/ast Scanner

For more control, write a custom AST-based scanner.

### Implementation

```go
package main

import (
    "fmt"
    "go/ast"
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
)

func main() {
    filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() || filepath.Ext(path) != ".go" {
            return nil
        }
        checkFile(path)
        return nil
    })
}

func checkFile(path string) {
    fset := token.NewFileSet()
    file, err := parser.ParseFile(fset, path, nil, 0)
    if err != nil {
        return
    }

    ast.Inspect(file, func(n ast.Node) bool {
        // Look for for loops
        switch stmt := n.(type) {
        case *ast.ForStmt, *ast.RangeStmt:
            checkLoopForDefer(fset, stmt, path)
        }
        return true
    })
}

func checkLoopForDefer(fset *token.FileSet, loop ast.Node, path string) {
    ast.Inspect(loop, func(n ast.Node) bool {
        if def, ok := n.(*ast.DeferStmt); ok {
            pos := fset.Position(def.Pos())
            fmt.Printf("%s:%d: defer inside loop\n", path, pos.Line)
        }
        return true
    })
}
```

### Usage

```bash
go run defer_checker.go
# Output:
# process.go:45: defer inside loop
# handler.go:123: defer inside loop
```

---

## Method 3: Runtime Monitoring

Detect defer accumulation at runtime by monitoring resource usage.

### File Descriptor Monitoring

```go
package main

import (
    "log"
    "os"
    "runtime"
    "time"
)

func monitorFileDescriptors(interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for range ticker.C {
        fdCount := countFileDescriptors()
        goroutines := runtime.NumGoroutine()
        
        log.Printf("FDs: %d, Goroutines: %d", fdCount, goroutines)
        
        // Alert if FDs are growing unexpectedly
        if fdCount > 500 {
            log.Printf("WARNING: High file descriptor count: %d", fdCount)
        }
    }
}

func countFileDescriptors() int {
    // macOS
    if entries, err := os.ReadDir("/dev/fd"); err == nil {
        return len(entries)
    }
    // Linux
    if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
        return len(entries)
    }
    return -1
}
```

### Memory Monitoring

```go
func monitorMemory(interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    var lastAllocs uint64

    for range ticker.C {
        var m runtime.MemStats
        runtime.ReadMemStats(&m)
        
        allocRate := m.Mallocs - lastAllocs
        lastAllocs = m.Mallocs
        
        log.Printf("Heap: %d KB, Allocs/sec: %d", m.HeapAlloc/1024, allocRate)
        
        // Alert on high allocation rate (possible defer accumulation)
        if allocRate > 100000 {
            log.Printf("WARNING: High allocation rate: %d/sec", allocRate)
        }
    }
}
```

### Integration with Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    fdGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_open_file_descriptors",
        Help: "Current number of open file descriptors",
    })
    heapGauge = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "app_heap_alloc_bytes",
        Help: "Current heap allocation in bytes",
    })
)

func updateMetrics() {
    fdGauge.Set(float64(countFileDescriptors()))
    
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    heapGauge.Set(float64(m.HeapAlloc))
}
```

---

## Method 4: pprof Analysis

Use Go's built-in profiler to identify defer-related issues.

### Setup

```go
import (
    "net/http"
    _ "net/http/pprof"
)

func main() {
    go func() {
        http.ListenAndServe("localhost:6060", nil)
    }()
    // ... rest of application
}
```

### Heap Profile Analysis

```bash
# Collect heap profile during execution
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# Analyze allocations
go tool pprof -alloc_objects heap.pprof
(pprof) top
(pprof) list processFiles  # Look at specific function
```

### What to Look For

```
(pprof) top -alloc_objects
Showing nodes accounting for 10000, 100% of 10000 total
      flat  flat%   sum%        cum   cum%
     10000 100.00% 100.00%     10000 100.00%  runtime.deferproc
```

**Red Flag**: High allocation counts in `runtime.deferproc` during loop execution.

### Compare Profiles

```bash
# Collect baseline
curl http://localhost:6060/debug/pprof/heap > baseline.pprof

# Wait during batch processing
sleep 60

# Collect during processing
curl http://localhost:6060/debug/pprof/heap > during.pprof

# Compare
go tool pprof -http=:8080 -base=baseline.pprof during.pprof
```

---

## Method 5: Code Review Checklist

Manual code review remains important for catching subtle issues.

### Checklist

When reviewing code, check for:

- [ ] **Defer inside `for` loop**
  ```go
  // ❌ Red flag
  for _, item := range items {
      file := open(item)
      defer file.Close()  // Accumulates!
  }
  ```

- [ ] **Defer inside `range` loop**
  ```go
  // ❌ Red flag
  for range ch {
      conn := connect()
      defer conn.Close()  // Accumulates!
  }
  ```

- [ ] **Defer in infinite loop**
  ```go
  // ❌ Critical issue
  for {
      item := <-ch
      resource := acquire(item)
      defer resource.Release()  // Never executes!
  }
  ```

- [ ] **Closure captures loop variable** (pre-Go 1.22)
  ```go
  // ❌ Bug in pre-Go 1.22
  for _, v := range items {
      defer func() { use(v) }()  // All use same 'v'
  }
  ```

- [ ] **Defer before error check**
  ```go
  // ❌ Panic risk
  file, err := os.Open(path)
  defer file.Close()  // Panic if err != nil
  if err != nil {
      return err
  }
  ```

### Automation in PR Reviews

Add review bots or hooks:

```yaml
# reviewdog integration
name: reviewdog
on: [pull_request]
jobs:
  golangci-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: reviewdog/action-golangci-lint@v2
        with:
          filter_mode: diff_context
          golangci_lint_flags: "--enable=gocritic"
```

---

## Method 6: Testing with Low Limits

Force issues to appear in tests.

### Test Setup

```go
func TestProcessFiles(t *testing.T) {
    // Create many test files
    paths := createTempFiles(t, 2000)  // More than default FD limit
    
    // This should not exceed FD limits
    err := processFiles(paths)
    if err != nil {
        t.Errorf("Failed to process files: %v", err)
    }
}
```

### Running with Low Limits

```bash
# Set low file descriptor limit
ulimit -n 256

# Run tests
go test -v ./...

# If defer-in-loop exists, test fails with:
# "too many open files"
```

### Dockerized Testing

```dockerfile
FROM golang:1.21

# Set low resource limits
RUN ulimit -n 256

COPY . /app
WORKDIR /app

CMD ["go", "test", "-v", "./..."]
```

---

## Method 7: Benchmark Testing

Write benchmarks that expose defer accumulation.

### Implementation

```go
func BenchmarkProcessFilesDefer(b *testing.B) {
    paths := createTempFiles(b, 100)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        processWithDeferInLoop(paths)  // Current implementation
    }
}

func BenchmarkProcessFilesFixed(b *testing.B) {
    paths := createTempFiles(b, 100)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        processWithExtractedFunction(paths)  // Fixed implementation
    }
}
```

### Running

```bash
go test -bench=BenchmarkProcessFiles -benchmem

# Compare allocations
# Defer-in-loop: 100 allocs/op (one per file)
# Fixed: 0 allocs/op (or much fewer)
```

---

## Method 8: Escape Analysis

Use Go's escape analysis to see defer behavior.

### Command

```bash
go build -gcflags="-m" 2>&1 | grep defer
```

### Sample Output

```
./process.go:45:10: defer argument escapes to heap
./process.go:45:10: defer closure escapes to heap
./handler.go:78:10: defer argument does not escape
```

**Red Flag**: "defer argument escapes to heap" inside a loop means heap-allocated defers.

---

## Detection Summary Matrix

| Method | Catches | Timing | Setup Effort |
|--------|---------|--------|--------------|
| golangci-lint | Defer in loop | Pre-commit | Low |
| Custom AST | Custom patterns | Pre-commit | Medium |
| Runtime monitoring | Accumulation | Production | Medium |
| pprof | Memory pressure | Debug | Low |
| Code review | All patterns | PR review | None |
| Low-limit tests | FD exhaustion | CI | Low |
| Benchmarks | Performance | Development | Medium |
| Escape analysis | Heap allocation | Development | None |

---

## Recommended Setup

### Minimum (All Projects)

1. **golangci-lint with gocritic** in CI
2. **Code review checklist** for PRs
3. **pprof endpoint** in staging/production

### Medium (Production Systems)

Add to minimum:
1. **Runtime metrics** (Prometheus) for FD count
2. **Low-limit tests** in CI
3. **Escape analysis** in pre-commit hook

### Maximum (Critical Systems)

Add to medium:
1. **Custom AST scanner** for organization-specific patterns
2. **Benchmarks** comparing implementations
3. **Alerting** on FD/memory anomalies

---

## Integration Example: Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

# Run golangci-lint
golangci-lint run --enable=gocritic --new-from-rev=HEAD~1
if [ $? -ne 0 ]; then
    echo "Linting failed. Please fix issues before committing."
    exit 1
fi

# Check for defer-in-loop patterns
if grep -r "for.*{" --include="*.go" | xargs -I {} sh -c 'grep -A 5 {} | grep -q "defer"'; then
    echo "WARNING: Potential defer-in-loop detected. Please review."
fi
```

---

## Key Takeaways

1. **Static analysis is the first line of defense** — catches issues before they reach production

2. **Runtime monitoring catches what static analysis misses** — especially in dynamic scenarios

3. **Test with production-like data volumes** — defer issues only manifest at scale

4. **Multiple detection methods are complementary** — no single method catches everything

5. **Automate detection in CI/CD** — consistent enforcement prevents regressions

---

## Further Reading

- [Refactoring Patterns](04-refactoring-patterns.md) — How to fix detected issues
- [Performance Impact](05-performance-impact.md) — Understanding the cost
- [Benchmarks and Case Studies](07-benchmarks.md) — Real-world examples

