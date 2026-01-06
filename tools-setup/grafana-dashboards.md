# Grafana Dashboards for Go Leak Detection

**Visualization and alerting for memory and resource leaks**

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Dashboard Panels](#dashboard-panels)
3. [Complete Dashboard JSON](#complete-dashboard-json)
4. [Alert Configuration](#alert-configuration)

---

## Quick Start

### Install Grafana

**Docker**:
```bash
docker run -d -p 3000:3000 grafana/grafana
```

**macOS**:
```bash
brew install grafana
brew services start grafana
```

Access at `http://localhost:3000` (default: admin/admin)

### Add Prometheus Data Source

1. Go to Configuration → Data Sources
2. Add data source → Prometheus
3. URL: `http://localhost:9090` (or your Prometheus URL)
4. Save & Test

---

## Dashboard Panels

### Panel 1: Goroutine Count

```json
{
  "title": "Goroutine Count",
  "type": "timeseries",
  "targets": [
    {
      "expr": "app_goroutines",
      "legendFormat": "Goroutines"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "color": {"mode": "palette-classic"},
      "thresholds": {
        "mode": "absolute",
        "steps": [
          {"color": "green", "value": null},
          {"color": "yellow", "value": 1000},
          {"color": "red", "value": 5000}
        ]
      }
    }
  }
}
```

### Panel 2: Goroutine Growth Rate

```json
{
  "title": "Goroutine Growth Rate",
  "type": "timeseries",
  "targets": [
    {
      "expr": "rate(app_goroutines[5m]) * 60",
      "legendFormat": "Growth/min"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "unit": "short",
      "thresholds": {
        "mode": "absolute",
        "steps": [
          {"color": "green", "value": null},
          {"color": "yellow", "value": 10},
          {"color": "red", "value": 100}
        ]
      }
    }
  }
}
```

### Panel 3: Heap Memory

```json
{
  "title": "Heap Memory",
  "type": "timeseries",
  "targets": [
    {
      "expr": "app_heap_alloc_bytes",
      "legendFormat": "Heap Alloc"
    },
    {
      "expr": "go_memstats_heap_inuse_bytes",
      "legendFormat": "Heap In Use"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "unit": "bytes"
    }
  }
}
```

### Panel 4: Memory Growth Rate

```json
{
  "title": "Memory Growth Rate",
  "type": "timeseries",
  "targets": [
    {
      "expr": "rate(app_heap_alloc_bytes[5m])",
      "legendFormat": "Growth Rate"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "unit": "Bps"
    }
  }
}
```

### Panel 5: Queue Depth

```json
{
  "title": "Queue Depth",
  "type": "gauge",
  "targets": [
    {
      "expr": "app_queue_depth",
      "legendFormat": "Queue Depth"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "max": 1000,
      "thresholds": {
        "mode": "absolute",
        "steps": [
          {"color": "green", "value": null},
          {"color": "yellow", "value": 500},
          {"color": "red", "value": 800}
        ]
      }
    }
  }
}
```

### Panel 6: Task Rejection Rate

```json
{
  "title": "Task Rejections",
  "type": "timeseries",
  "targets": [
    {
      "expr": "rate(app_tasks_rejected_total[5m])",
      "legendFormat": "Rejections/sec"
    }
  ],
  "fieldConfig": {
    "defaults": {
      "unit": "reqps"
    }
  }
}
```

### Panel 7: Open File Descriptors

```json
{
  "title": "Open File Descriptors",
  "type": "timeseries",
  "targets": [
    {
      "expr": "app_open_files",
      "legendFormat": "Open Files"
    },
    {
      "expr": "process_max_fds",
      "legendFormat": "Max FDs"
    }
  ]
}
```

### Panel 8: Database Connections

```json
{
  "title": "Database Pool",
  "type": "timeseries",
  "targets": [
    {
      "expr": "app_db_pool_active",
      "legendFormat": "Active"
    },
    {
      "expr": "app_db_pool_idle",
      "legendFormat": "Idle"
    }
  ]
}
```

---

## Complete Dashboard JSON

Save as `go-leak-detection.json` and import into Grafana:

```json
{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "title": "Goroutine Count",
      "type": "timeseries",
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 0},
      "targets": [
        {
          "datasource": {"type": "prometheus"},
          "expr": "app_goroutines",
          "legendFormat": "Goroutines"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"},
          "custom": {
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {"legend": false, "tooltip": false, "viz": false},
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {"type": "linear"},
            "showPoints": "never",
            "spanNulls": false,
            "stacking": {"group": "A", "mode": "none"},
            "thresholdsStyle": {"mode": "line"}
          },
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "yellow", "value": 1000},
              {"color": "red", "value": 5000}
            ]
          }
        }
      }
    },
    {
      "title": "Heap Memory",
      "type": "timeseries",
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 0},
      "targets": [
        {
          "datasource": {"type": "prometheus"},
          "expr": "app_heap_alloc_bytes",
          "legendFormat": "Heap Alloc"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "bytes"
        }
      }
    },
    {
      "title": "Queue Depth",
      "type": "gauge",
      "gridPos": {"h": 8, "w": 6, "x": 0, "y": 8},
      "targets": [
        {
          "datasource": {"type": "prometheus"},
          "expr": "app_queue_depth"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "max": 1000,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "yellow", "value": 500},
              {"color": "red", "value": 800}
            ]
          }
        }
      }
    },
    {
      "title": "Task Rejections",
      "type": "stat",
      "gridPos": {"h": 8, "w": 6, "x": 6, "y": 8},
      "targets": [
        {
          "datasource": {"type": "prometheus"},
          "expr": "rate(app_tasks_rejected_total[5m])",
          "legendFormat": "Rejections/sec"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "reqps",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "red", "value": 1}
            ]
          }
        }
      }
    },
    {
      "title": "Open Files",
      "type": "timeseries",
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 8},
      "targets": [
        {
          "datasource": {"type": "prometheus"},
          "expr": "app_open_files",
          "legendFormat": "Open Files"
        }
      ]
    }
  ],
  "refresh": "5s",
  "schemaVersion": 38,
  "style": "dark",
  "tags": ["go", "leak-detection"],
  "templating": {"list": []},
  "time": {"from": "now-1h", "to": "now"},
  "timepicker": {},
  "timezone": "",
  "title": "Go Leak Detection",
  "uid": "go-leak-detection",
  "version": 1
}
```

---

## Alert Configuration

### Grafana Alert Rules

1. Go to Alerting → Alert rules
2. Create new alert rule

**Goroutine Leak Alert**:
```yaml
Name: Goroutine Leak Suspected
Condition: app_goroutines > 5000
Duration: 5m
Severity: critical
```

**Memory Leak Alert**:
```yaml
Name: Memory Leak Suspected
Condition: rate(app_heap_alloc_bytes[5m]) > 10000000
Duration: 30m
Severity: warning
```

**Queue Backpressure Alert**:
```yaml
Name: Queue Near Capacity
Condition: app_queue_depth / app_queue_capacity > 0.8
Duration: 5m
Severity: warning
```

### Notification Channels

1. Go to Alerting → Contact points
2. Add contact point (Slack, Email, PagerDuty, etc.)
3. Configure notification policies

---

**Return to**: [Tools Setup](./README.md)

