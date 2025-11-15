# HTTP Connection Pooling in Go

**Read Time**: ~18 minutes

**Prerequisites**: Basic HTTP knowledge, understanding of TCP connections

**Summary**: Learn how Go's HTTP client manages connections, why response bodies must be closed, how connection pooling works, and common patterns that cause connection leaks.

---

## Introduction

Go's `net/http` package includes sophisticated connection pooling to reuse TCP connections across HTTP requests. However, this optimization requires proper resource management—**unclosed response bodies are one of the most common causes of production incidents in Go applications**.

## The Problem: HTTP Response Bodies as Resources

Unlike simple values, HTTP response bodies are **streaming resources** backed by open network connections. When you make an HTTP request:

```go
resp, err := http.Get("https://api.example.com/data")
```

Under the hood:
1. Go opens (or reuses) a TCP connection
2. Sends HTTP request over the connection
3. Reads HTTP response headers
4. Returns `*http.Response` with `Body` still streaming
5. **Connection remains open** waiting for body to be read and closed

### The Golden Rule

**Every `http.Response.Body` MUST be closed, even if you don't read it.**

```go
resp, err := http.Get(url)
if err != nil {
    return err
}
defer resp.Body.Close()  // ✅ Always required
```

## How Go's HTTP Client Connection Pool Works

### Connection Pool Architecture

```
┌─────────────────────────────────────────────┐
│          http.Client                        │
│                                             │
│  ┌───────────────────────────────────────┐ │
│  │      http.Transport (Pool Manager)    │ │
│  │                                       │ │
│  │  Connection Pool (per host)           │ │
│  │  ┌─────────────────────────────────┐  │ │
│  │  │  api.example.com:443            │  │ │
│  │  │  • conn1 [IDLE]                 │  │ │
│  │  │  • conn2 [IN-USE]               │  │ │
│  │  │  • conn3 [IDLE]                 │  │ │
│  │  └─────────────────────────────────┘  │ │
│  │  ┌─────────────────────────────────┐  │ │
│  │  │  api.other.com:443              │  │ │
│  │  │  • conn1 [IDLE]                 │  │ │
│  │  └─────────────────────────────────┘  │ │
│  └───────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

### Default Connection Pool Settings

```go
// Go's default HTTP client uses these settings:
DefaultTransport = &http.Transport{
    MaxIdleConns:          100,              // Total idle connections across all hosts
    MaxIdleConnsPerHost:   2,                // Idle connections per host
    MaxConnsPerHost:       0,                // Unlimited active connections per host
    IdleConnTimeout:       90 * time.Second, // How long idle connections stay alive
    ExpectContinueTimeout: 1 * time.Second,
    // ... more settings
}
```

### Connection Lifecycle

```
1. Request Made
   ↓
2. Check Pool for Idle Connection
   ↓
   ├─ Found → Reuse Connection
   │          ↓
   │       4. Send Request
   │          ↓
   │       5. Receive Response
   │          ↓
   │       6. Body.Close() called
   │          ↓
   │       7. Return Connection to Pool (IDLE)
   │
   └─ Not Found → Create New Connection
              ↓
           3. TCP Handshake (slow!)
              ↓
           4. TLS Handshake if HTTPS (very slow!)
              ↓
           [Continue as above...]
```

### What Happens Without `Body.Close()`

```go
// ❌ BAD: Body never closed
resp, err := http.Get(url)
if err != nil {
    return err
}
// Body still holds connection open!
// Connection CANNOT be reused
// Stuck in "IN-USE" state until:
//   • TCP timeout (minutes)
//   • Connection limit reached
//   • Application crashes
```

**Result**: Connection pool exhaustion, new requests fail or block.

## Common Connection Leak Patterns

### Pattern 1: Early Return Without Close

```go
// ❌ LEAK: Early return on status check
func fetchData(url string) ([]byte, error) {
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("bad status: %d", resp.StatusCode)
        // ❌ Body never closed!
    }
    
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}

// ✅ FIX: Close immediately after error check
func fetchData(url string) ([]byte, error) {
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()  // ✅ Always closes, even on early return
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("bad status: %d", resp.StatusCode)
    }
    
    return io.ReadAll(resp.Body)
}
```

### Pattern 2: Not Reading Full Body

```go
// ⚠️ SUBOPTIMAL: Body not fully read
func checkStatus(url string) (bool, error) {
    resp, err := http.Get(url)
    if err != nil {
        return false, err
    }
    defer resp.Body.Close()
    
    return resp.StatusCode == 200, nil
    // Body closed, but not read → connection may not be reused
}

// ✅ BETTER: Read and discard body for connection reuse
func checkStatus(url string) (bool, error) {
    resp, err := http.Get(url)
    if err != nil {
        return false, err
    }
    defer func() {
        // Drain body to enable connection reuse
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()
    
    return resp.StatusCode == 200, nil
}
```

### Pattern 3: Ignoring Error Responses

```go
// ❌ LEAK: Error responses also need closing
func mustFetch(url string) []byte {
    resp, err := http.Get(url)
    if err != nil {
        log.Fatal(err)
    }
    
    if resp.StatusCode != 200 {
        log.Fatalf("bad status: %d", resp.StatusCode)
        // ❌ Body never closed!
    }
    
    defer resp.Body.Close()
    data, _ := io.ReadAll(resp.Body)
    return data
}
```

### Pattern 4: Forgotten Close in Goroutines

```go
// ❌ LEAK: Goroutine exits, response never closed
func fetchAsync(url string, results chan<- []byte) {
    go func() {
        resp, err := http.Get(url)
        if err != nil {
            results <- nil
            return  // ❌ Goroutine exits, body never closed
        }
        
        data, _ := io.ReadAll(resp.Body)
        results <- data
        // ❌ Body never closed!
    }()
}

// ✅ FIX: Ensure cleanup in goroutine
func fetchAsync(url string, results chan<- []byte) {
    go func() {
        resp, err := http.Get(url)
        if err != nil {
            results <- nil
            return
        }
        defer resp.Body.Close()  // ✅ Cleanup in goroutine
        
        data, _ := io.ReadAll(resp.Body)
        results <- data
    }()
}
```

## Configuring Connection Pools

### Custom HTTP Client with Optimal Settings

```go
// Production-ready HTTP client configuration
func newHTTPClient() *http.Client {
    transport := &http.Transport{
        // Connection pool settings
        MaxIdleConns:        100,             // Total idle connections
        MaxIdleConnsPerHost: 10,              // Idle connections per host
        MaxConnsPerHost:     100,             // Max connections per host (0 = unlimited)
        
        // Timeout settings
        IdleConnTimeout:       90 * time.Second,  // How long idle connections stay open
        TLSHandshakeTimeout:   10 * time.Second,  // TLS handshake timeout
        ExpectContinueTimeout: 1 * time.Second,   // Expect: 100-continue timeout
        ResponseHeaderTimeout: 10 * time.Second,  // Time to read response headers
        
        // Keep-alive settings
        DisableKeepAlives: false,  // Enable connection reuse
        
        // Compression
        DisableCompression: false,  // Enable gzip
        
        // Proxy settings
        Proxy: http.ProxyFromEnvironment,
    }
    
    return &http.Client{
        Transport: transport,
        Timeout:   30 * time.Second,  // Overall request timeout
        
        // Don't follow redirects automatically (optional)
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) >= 10 {
                return fmt.Errorf("too many redirects")
            }
            return nil
        },
    }
}
```

### Monitoring Connection Pool Health

```go
type ConnectionPoolMonitor struct {
    client *http.Client
}

func (m *ConnectionPoolMonitor) GetStats() map[string]int {
    transport := m.client.Transport.(*http.Transport)
    
    // Unfortunately, Go doesn't expose detailed pool stats directly
    // But we can observe behavior with these metrics:
    
    return map[string]int{
        "max_idle_conns":          transport.MaxIdleConns,
        "max_idle_conns_per_host": transport.MaxIdleConnsPerHost,
        "max_conns_per_host":      transport.MaxConnsPerHost,
        // For actual counts, need to use runtime debug or pprof
    }
}

// Use pprof to inspect connection states
// curl http://localhost:6060/debug/pprof/goroutine?debug=1 | grep "http"
```

## Connection Pool Best Practices

### 1. Reuse HTTP Clients (Don't Create Per-Request)

```go
// ❌ BAD: Creates new client (and pool) for each request
func fetchData(url string) ([]byte, error) {
    client := &http.Client{}  // New pool every time!
    resp, err := client.Get(url)
    // No connection reuse possible
}

// ✅ GOOD: Reuse client across requests
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConnsPerHost: 10,
    },
}

func fetchData(url string) ([]byte, error) {
    resp, err := httpClient.Get(url)
    // Connections reused efficiently
}
```

### 2. Always Set Timeouts

```go
// ❌ BAD: No timeouts = connections hang forever
client := &http.Client{}

// ✅ GOOD: Multiple layers of timeouts
client := &http.Client{
    Timeout: 30 * time.Second,  // Overall request timeout
    Transport: &http.Transport{
        DialContext: (&net.Dialer{
            Timeout:   5 * time.Second,  // Connection timeout
            KeepAlive: 30 * time.Second,
        }).DialContext,
        TLSHandshakeTimeout:   10 * time.Second,  // TLS timeout
        ResponseHeaderTimeout: 10 * time.Second,  // Header read timeout
        IdleConnTimeout:       90 * time.Second,  // Idle connection timeout
    },
}
```

### 3. Proper Error Response Handling

```go
// ✅ Correct pattern for all cases
func robustFetch(url string) ([]byte, error) {
    resp, err := httpClient.Get(url)
    if err != nil {
        return nil, err
    }
    
    // Close body in all cases
    defer func() {
        // Drain remaining body for connection reuse
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()
    
    // Check status
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
    }
    
    // Read body
    return io.ReadAll(resp.Body)
}
```

### 4. Limit Concurrent Requests

```go
// Use semaphore to limit concurrent HTTP requests
type RateLimitedClient struct {
    client *http.Client
    sem    chan struct{}
}

func NewRateLimitedClient(maxConcurrent int) *RateLimitedClient {
    return &RateLimitedClient{
        client: &http.Client{
            Timeout: 30 * time.Second,
            Transport: &http.Transport{
                MaxIdleConnsPerHost: maxConcurrent,
                MaxConnsPerHost:     maxConcurrent,
            },
        },
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (c *RateLimitedClient) Get(url string) (*http.Response, error) {
    c.sem <- struct{}{}        // Acquire semaphore
    defer func() { <-c.sem }() // Release semaphore
    
    return c.client.Get(url)
}
```

## Detecting HTTP Connection Leaks

### Method 1: Goroutine Profiling

HTTP connections leak goroutines too:

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1 | grep -A 10 "http"
```

Look for:
- `net/http.(*persistConn).readLoop`
- `net/http.(*persistConn).writeLoop`
- Growing count of these goroutines = connection leak

### Method 2: Network Monitoring

```bash
# Monitor established connections
watch -n 1 "netstat -an | grep ESTABLISHED | grep :443 | wc -l"

# Check connection states
netstat -an | awk '{print $6}' | sort | uniq -c

# Look for CLOSE_WAIT (application-side leak)
netstat -an | grep CLOSE_WAIT | wc -l
```

### Method 3: HTTP Transport Metrics

```go
// Instrument HTTP client with metrics
type InstrumentedTransport struct {
    rt http.RoundTripper
}

func (t *InstrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    start := time.Now()
    
    // Increment in-flight requests
    inflightRequests.Inc()
    defer inflightRequests.Dec()
    
    resp, err := t.rt.RoundTrip(req)
    
    // Record metrics
    duration := time.Since(start)
    requestDuration.Observe(duration.Seconds())
    
    if err != nil {
        requestErrors.Inc()
        return nil, err
    }
    
    requestTotal.WithLabelValues(
        req.Method,
        fmt.Sprintf("%d", resp.StatusCode),
    ).Inc()
    
    return resp, nil
}
```

## Case Study: The Cascading Connection Leak

**Scenario**: API gateway making 100 req/s to backend services.

**Configuration**:
```go
client := &http.Client{
    Transport: &http.Transport{
        MaxIdleConnsPerHost: 2,  // ⚠️ Too low!
    },
}
```

**What Happened**:
1. 100 req/s needs ~20-50 concurrent connections
2. Pool only holds 2 idle connections per host
3. Every request creates new connection (TCP + TLS handshake)
4. Responses not always closed properly on error paths
5. Leaked connections accumulate

**Timeline**:
- **Hour 1**: 50 leaked connections, performance degrading
- **Hour 2**: 500 leaked connections, latency spikes
- **Hour 3**: 1000+ connections, hitting OS limits
- **Hour 4**: Service crashes, cascades to other services

**Fix**:
```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConnsPerHost: 50,   // ✅ Match concurrent load
        MaxConnsPerHost:     100,  // ✅ Limit total connections
        IdleConnTimeout:     90 * time.Second,
    },
}

// AND fix all response body handling
defer resp.Body.Close()
```

## Key Takeaways

1. **HTTP response bodies MUST be closed** - no exceptions

2. **Connection pooling is automatic** but requires proper resource cleanup

3. **Unclosed bodies prevent connection reuse** - huge performance impact

4. **Always defer `resp.Body.Close()`** immediately after error check

5. **Read and discard body** (`io.Copy(io.Discard, resp.Body)`) for optimal reuse

6. **Reuse HTTP clients** - don't create new clients per request

7. **Configure pool sizes** based on concurrent request load

8. **Set timeouts at multiple levels** - never use default infinite timeout

---

## References

- https://go.dev/blog/pipelines - Go blog on timeouts and cancellation
- https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/ - Cloudflare's timeout guide
- `net/http` package documentation

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - General defer patterns
- [Defer Mechanics](04-defer-mechanics.md) - Advanced defer usage
- [Monitoring Strategies](06-monitoring-strategies.md) - Production monitoring

