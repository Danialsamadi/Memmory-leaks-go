# Go Memory Leaks:

## Quick Links

- [Learning Path](./LEARNING_PATH.md)
- [5 Leak Types](#leak-types-overview)
- [Tools Setup](#tools-setup)
- [Visual Guides](#visual-guides)
- [Real-World Examples](#real-world-examples)

## Overview

Memory leaks in Go are a critical yet often misunderstood topic. Despite Go's garbage collector handling most memory management automatically, several patterns can lead to memory leaks, degraded performance, and system instability. This repository provides a comprehensive, hands-on educational resource for understanding, detecting, and fixing the five most common types of memory leaks in Go applications.

Unlike other resources that only show problematic code, this repository includes complete working examples, detailed profiling instructions, visual diagrams, and extensive learning materials. Each leak type comes with both a leaky version and a fixed version, allowing you to compare behavior and understand the impact of proper resource management.

This repository is organized into five main categories of memory leaks, each with its own directory containing runnable examples, profiling guides, and deep-dive resources. Whether you're a beginner learning Go concurrency or an experienced developer debugging production issues, this repository provides the tools and knowledge you need.

## Why This Matters

Memory leaks in production can lead to:
- Application crashes due to out-of-memory errors
- Degraded performance as garbage collection struggles
- Increased infrastructure costs from resource waste
- Poor user experience from slow response times
- Difficult-to-debug issues that only appear under load

Understanding these patterns helps you write robust, production-ready Go code from the start.

## How This Repository Is Organized

Each leak type has its own directory with:
- **README.md**: Overview, detection methods, and examples
- **example.go**: Runnable code demonstrating the leak
- **fixed_example.go**: Corrected version showing proper patterns
- **pprof_analysis.md**: Step-by-step profiling instructions
- **resources/**: 7+ deep-dive markdown files covering theory, internals, and practice

Additional directories provide:
- **tools-setup/**: Complete guides for pprof, trace, Delve, and IDE integration
- **visual-guides/**: Flowcharts, decision trees, and diagrams
- **scripts/**: Automation for running examples and collecting profiles

## Learning Path

This repository supports multiple learning approaches:

### Beginner Path
Start with [Learning Path Guide](./LEARNING_PATH.md) which walks you through:
1. Setting up profiling tools
2. Running your first leak example
3. Understanding goroutine leaks (the most common type)
4. Using pprof to detect the issue
5. Applying the fix and verifying success

### Intermediate Path
Work through all five leak types systematically, studying the profiling output and internal mechanisms for each.

### Advanced Path
Deep-dive into Go runtime internals, study production case studies, and learn to create custom detection tooling.

### By Use Case
- **"I have a production memory leak"**: Start with [Detection Decision Tree](./visual-guides/detection-decision-tree.md)
- **"I want to learn Go concurrency"**: Start with [Goroutine Leaks](./1.Goroutine-Leaks-Most-Common/)
- **"I need to teach others"**: Use the [Visual Guides](./visual-guides/) for presentations

## Prerequisites

### Required Software
- **Go 1.20 or later**: For running examples
- **graphviz**: For pprof graph visualization
- **git**: For cloning this repository

### Installation

```bash
# Install Go (if not already installed)
# Visit https://go.dev/dl/

# Install graphviz (macOS)
brew install graphviz

# Install graphviz (Ubuntu/Debian)
sudo apt-get install graphviz

# Install graphviz (Windows)
choco install graphviz
```

### Recommended Tools
- **Delve**: Go debugger for advanced debugging
- **GoLand or VS Code**: IDEs with built-in profiling support
- **Docker**: For containerized profiling examples

## Quick Start

Get started in under 5 minutes:

```bash
# Clone the repository
git clone https://github.com/Danialsamadi/Memmory-leaks-go.git
cd go-memory-leaks-educational

# Run your first leak example
cd 1.Goroutine-Leaks-Most-Common
go run example.go

# In another terminal, collect a goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof

# View the profile in your browser
go tool pprof -http=:8081 goroutine_fixedEX.pprof

# Now run the fixed version and see the difference
go run fixed_example.go
```

Expected output from leaky version:
```
[START] Goroutines: 1
[AFTER 2s] Goroutines: 101
[AFTER 4s] Goroutines: 201
[AFTER 6s] Goroutines: 301
```

Expected output from fixed version:
```
[START] Goroutines: 1
[AFTER 2s] Goroutines: 1
[AFTER 4s] Goroutines: 1
[AFTER 6s] Goroutines: 1
```

## Leak Types Overview

| # | Type | Category | Detection | Fix Strategy | Link |
|---|------|----------|-----------|--------------|------|
| 1 | Goroutine Leaks | Most Common | `runtime.NumGoroutine()` | Context + Channels | [Details](./1.Goroutine-Leaks-Most-Common/) |
| 2 | Long-Lived References | Memory | Heap Profile | Cache Limits + Clone | [Details](./2.Long-Lived-References/) |
| 3 | Resource Leaks | Unclosed | `lsof` + pprof | `defer Close()` | [Details](./3.Resource-Leaks/) |
| 4 | Defer Issues | Loop-Related | Stack Growth | Refactor Loop | [Details](./4.Defer-Issues/) |
| 5 | Unbounded Resources | Unlimited | Goroutine Count | Worker Pool | [Details](./5.Unbounded-Resources/) |

### 1. Goroutine Leaks (Most Common)

**What**: Goroutines that never terminate, accumulating over time and consuming memory.

**Common Causes**:
- Channels that are never read or closed
- Missing context cancellation
- Blocking operations without timeouts
- HTTP handlers spawning goroutines without cleanup

**Impact**: High - Can quickly exhaust system resources

**Detection**: Monitor `runtime.NumGoroutine()` over time

[Go to Goroutine Leaks Directory](./1.Goroutine-Leaks-Most-Common/)

### 2. Long-Lived References

**What**: Objects kept in memory longer than necessary due to retained references.

**Common Causes**:
- Unbounded caches without eviction policies
- Slice reslicing keeping the underlying array alive
- Global variables holding large data structures
- Event listeners not being removed

**Impact**: Medium-High - Gradual memory growth over days/weeks

**Detection**: Heap profiling showing unexpected allocations

[Go to Long-Lived References Directory](./2.Long-Lived-References/)

### 3. Resource Leaks

**What**: OS resources (file descriptors, timers, connections) not being released.

**Common Causes**:
- Files opened but not closed
- HTTP response bodies not closed
- Timers/Tickers not stopped
- Database connections not returned to pool

**Impact**: High - Can hit OS limits and crash the application

**Detection**: `lsof` command, file descriptor count, timer profiles

[Go to Resource Leaks Directory](./3.Resource-Leaks/)

### 4. Defer Issues

**What**: Excessive use of defer in loops causing memory to accumulate until function exit.

**Common Causes**:
- Using `defer` inside loops that process many items
- Not understanding defer's LIFO execution model
- Deferring in recursive functions

**Impact**: Medium - Can cause memory spikes during long-running operations

**Detection**: Stack growth, heap profiling of deferred functions

[Go to Defer Issues Directory](./4.Defer-Issues/)

### 5. Unbounded Resources

**What**: Creating resources without limits, allowing unlimited goroutines or connections.

**Common Causes**:
- Spawning a goroutine per request without rate limiting
- Accepting unlimited concurrent connections
- Creating unbuffered channels in high-throughput scenarios
- Not using worker pools

**Impact**: High - Can cause rapid resource exhaustion under load

**Detection**: Goroutine count spikes, CPU saturation

[Go to Unbounded Resources Directory](./5.Unbounded-Resources/)

## Tools Setup

Comprehensive guides for profiling and debugging tools:

- [pprof Complete Guide](./tools-setup/pprof-complete-guide.md) - Web UI, CLI, interpretation
- [go tool trace Guide](./tools-setup/go-tool-trace-guide.md) - Execution tracing and timeline analysis
- [Delve Debugging Guide](./tools-setup/delve-debugging-guide.md) - Interactive debugging of memory leaks
- [GoLand Profiling](./tools-setup/goland-profiling.md) - Using JetBrains GoLand for profiling
- [VS Code Debugging](./tools-setup/vscode-debugging.md) - Profiling setup in Visual Studio Code
- [Docker Profiling](./tools-setup/docker-profiling.md) - Profiling containerized applications

Quick tool reference:

```bash
# Collect goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof

# Collect heap profile
curl http://localhost:6060/debug/pprof/heap > heap.pprof

# Collect allocation profile
curl http://localhost:6060/debug/pprof/allocs > allocs.pprof

# View profile interactively
go tool pprof -http=:8081 profile.pprof

# Compare two profiles
go tool pprof -base=before.pprof after.pprof

# Collect execution trace
curl http://localhost:6060/debug/pprof/trace?seconds=10 > trace.out
go tool trace trace.out
```

## Visual Guides

Visual learning resources for understanding memory leak patterns:

- [Memory Leak Flowchart](./visual-guides/memory-leak-flowchart.md) - Decision tree for identifying leak types
- [Detection Decision Tree](./visual-guides/detection-decision-tree.md) - Step-by-step diagnosis process
- [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md) - Comprehensive fix patterns by leak type
- [Goroutine Timeline](./visual-guides/goroutine-timeline.md) - Visual goroutine lifecycle diagrams
- [Memory Growth Patterns](./visual-guides/memory-growth-patterns.md) - Graphs showing different leak signatures

These guides are particularly useful for:
- Teaching and presenting to teams
- Quick reference during debugging
- Understanding the big picture before diving into code
- Choosing the right detection approach

## Real-World Examples

Each leak type directory includes production examples in `resources/07-real-world-examples.md`:

1. **Goroutine Leaks**: HTTP server spawning unbounded goroutines, webhook processors
2. **Long-Lived References**: In-memory caches in web services, session stores
3. **Resource Leaks**: CSV processing jobs, log file rotation issues
4. **Defer Issues**: Large file batch processing, recursive directory traversal
5. **Unbounded Resources**: API rate limiting failures, connection pool exhaustion

These examples are drawn from real production issues and include:
- The problematic code pattern
- How the issue manifested in production
- Detection process and metrics
- The fix applied
- Lessons learned

## Contributing

Contributions are welcome! Areas where you can help:

- Additional real-world examples from your experience
- More visual diagrams and flowcharts
- IDE integration guides for other editors
- Translations to other languages
- Performance benchmark comparisons
- Additional profiling tool guides

Please open an issue first to discuss significant changes.

## License

MIT License - Feel free to use this for learning, teaching, or reference.

## Additional Resources

### Official Go Documentation
- [Diagnostics](https://go.dev/doc/diagnostics)
- [pprof Package](https://pkg.go.dev/net/http/pprof)
- [Runtime Package](https://pkg.go.dev/runtime)

### Recommended Reading
- "Go Memory Management" blog series
- "High Performance Go" workshop materials
- Go runtime source code (especially scheduler and GC)

### Community
- Go Forum - Memory Management category
- Reddit r/golang
- Gopher Slack #performance channel

## Acknowledgments

This repository synthesizes knowledge from:
- Go team's official documentation
- Production debugging experiences
- Community contributions and blog posts
- Academic research on memory management

---

**Start Learning**: [Begin with the Learning Path](./LEARNING_PATH.md) or [jump into Goroutine Leaks](./1.Goroutine-Leaks-Most-Common/)

