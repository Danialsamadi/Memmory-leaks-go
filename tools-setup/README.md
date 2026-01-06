# Tools Setup Guide

This directory contains comprehensive setup guides for all tools used in memory leak detection and profiling.

## Quick Links

| Tool | Purpose | Guide |
|------|---------|-------|
| **pprof** | Go's built-in profiler | [pprof-complete-guide.md](./pprof-complete-guide.md) |
| **Prometheus** | Metrics collection | [prometheus-setup.md](./prometheus-setup.md) |
| **Grafana** | Visualization | [grafana-dashboards.md](./grafana-dashboards.md) |

## Recommended Setup Order

1. **pprof** - Essential for all Go profiling
2. **Prometheus** - For production monitoring
3. **Grafana** - For visualization and alerting

## Quick Start

### Minimal Setup (Development)

Just use pprof - it's built into Go:

```go
import _ "net/http/pprof"

func main() {
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    // Your application code
}
```

Then access profiles at `http://localhost:6060/debug/pprof/`

### Full Setup (Production)

1. Add pprof endpoint (secured)
2. Deploy Prometheus for metrics
3. Configure Grafana dashboards
4. Set up alerting rules

See individual guides for detailed instructions.

