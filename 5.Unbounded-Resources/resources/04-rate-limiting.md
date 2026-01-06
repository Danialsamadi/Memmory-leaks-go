# Rate Limiting Strategies in Go

**Read Time**: 22 minutes

**Prerequisites**: Understanding of concurrency and time-based operations

---

## Table of Contents

1. [Why Rate Limiting](#why-rate-limiting)
2. [Token Bucket Algorithm](#token-bucket-algorithm)
3. [Sliding Window](#sliding-window)
4. [Implementation Examples](#implementation-examples)
5. [Distributed Rate Limiting](#distributed-rate-limiting)

---

## Why Rate Limiting

Rate limiting protects your system from:
- Traffic spikes overwhelming resources
- Abusive clients consuming unfair share
- Cascading failures in distributed systems
- Resource exhaustion attacks

### Without Rate Limiting

```go
// BAD: Unlimited requests
func handler(w http.ResponseWriter, r *http.Request) {
    // Process every request - no protection
    processRequest(r)
}
```

### With Rate Limiting

```go
// GOOD: Protected endpoint
func handler(limiter *rate.Limiter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !limiter.Allow() {
            http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
            return
        }
        processRequest(r)
    }
}
```

---

## Token Bucket Algorithm

The most common rate limiting algorithm. Tokens are added at a fixed rate; each request consumes a token.

### How It Works

```
Token Bucket:
┌─────────────────┐
│ ○ ○ ○ ○ ○ ○ ○ ○ │  Bucket (capacity: 10)
│   (8 tokens)    │
└────────┬────────┘
         │
    ┌────▼────┐
    │ Request │ → Consumes 1 token
    └─────────┘

Refill: 10 tokens/second
```

### Using golang.org/x/time/rate

```go
import "golang.org/x/time/rate"

// Create limiter: 100 requests/second, burst of 200
limiter := rate.NewLimiter(rate.Limit(100), 200)

// Non-blocking check
if limiter.Allow() {
    // Process request
}

// Blocking wait
ctx := context.Background()
if err := limiter.Wait(ctx); err != nil {
    // Context cancelled
}

// Wait with timeout
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
if err := limiter.Wait(ctx); err != nil {
    // Timeout or cancelled
}

// Reserve for future
reservation := limiter.Reserve()
if !reservation.OK() {
    // Cannot satisfy request
}
delay := reservation.Delay()
time.Sleep(delay)
// Now process
```

### Custom Token Bucket

```go
type TokenBucket struct {
    tokens     float64
    capacity   float64
    refillRate float64 // tokens per second
    lastRefill time.Time
    mu         sync.Mutex
}

func NewTokenBucket(capacity, refillRate float64) *TokenBucket {
    return &TokenBucket{
        tokens:     capacity,
        capacity:   capacity,
        refillRate: refillRate,
        lastRefill: time.Now(),
    }
}

func (tb *TokenBucket) Allow() bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()
    
    // Refill tokens based on elapsed time
    now := time.Now()
    elapsed := now.Sub(tb.lastRefill).Seconds()
    tb.tokens += elapsed * tb.refillRate
    if tb.tokens > tb.capacity {
        tb.tokens = tb.capacity
    }
    tb.lastRefill = now
    
    // Try to consume token
    if tb.tokens >= 1 {
        tb.tokens--
        return true
    }
    return false
}
```

---

## Sliding Window

More accurate than fixed windows, prevents burst at window boundaries.

### Fixed Window Problem

```
Window 1         Window 2
[----100----][----100----]
         ↑
    100 requests at end of window 1
    100 requests at start of window 2
    = 200 requests in 1 second (2x limit!)
```

### Sliding Window Solution

```go
type SlidingWindow struct {
    windowSize time.Duration
    limit      int
    requests   []time.Time
    mu         sync.Mutex
}

func NewSlidingWindow(windowSize time.Duration, limit int) *SlidingWindow {
    return &SlidingWindow{
        windowSize: windowSize,
        limit:      limit,
        requests:   make([]time.Time, 0, limit),
    }
}

func (sw *SlidingWindow) Allow() bool {
    sw.mu.Lock()
    defer sw.mu.Unlock()
    
    now := time.Now()
    windowStart := now.Add(-sw.windowSize)
    
    // Remove expired requests
    validRequests := sw.requests[:0]
    for _, t := range sw.requests {
        if t.After(windowStart) {
            validRequests = append(validRequests, t)
        }
    }
    sw.requests = validRequests
    
    // Check limit
    if len(sw.requests) >= sw.limit {
        return false
    }
    
    // Record new request
    sw.requests = append(sw.requests, now)
    return true
}
```

### Sliding Window Counter (Memory Efficient)

```go
type SlidingWindowCounter struct {
    windowSize    time.Duration
    limit         int
    currentCount  int
    previousCount int
    currentStart  time.Time
    mu            sync.Mutex
}

func (swc *SlidingWindowCounter) Allow() bool {
    swc.mu.Lock()
    defer swc.mu.Unlock()
    
    now := time.Now()
    
    // Check if we need to slide window
    if now.Sub(swc.currentStart) >= swc.windowSize {
        swc.previousCount = swc.currentCount
        swc.currentCount = 0
        swc.currentStart = now.Truncate(swc.windowSize)
    }
    
    // Calculate weighted count
    elapsed := now.Sub(swc.currentStart)
    weight := float64(swc.windowSize-elapsed) / float64(swc.windowSize)
    estimatedCount := float64(swc.previousCount)*weight + float64(swc.currentCount)
    
    if estimatedCount >= float64(swc.limit) {
        return false
    }
    
    swc.currentCount++
    return true
}
```

---

## Implementation Examples

### Per-Client Rate Limiting

```go
type ClientRateLimiter struct {
    limiters sync.Map // map[clientID]*rate.Limiter
    limit    rate.Limit
    burst    int
}

func NewClientRateLimiter(rps int, burst int) *ClientRateLimiter {
    return &ClientRateLimiter{
        limit: rate.Limit(rps),
        burst: burst,
    }
}

func (crl *ClientRateLimiter) GetLimiter(clientID string) *rate.Limiter {
    limiter, ok := crl.limiters.Load(clientID)
    if ok {
        return limiter.(*rate.Limiter)
    }
    
    // Create new limiter for client
    newLimiter := rate.NewLimiter(crl.limit, crl.burst)
    actual, _ := crl.limiters.LoadOrStore(clientID, newLimiter)
    return actual.(*rate.Limiter)
}

func (crl *ClientRateLimiter) Allow(clientID string) bool {
    return crl.GetLimiter(clientID).Allow()
}

// HTTP middleware
func RateLimitMiddleware(crl *ClientRateLimiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            clientID := r.Header.Get("X-Client-ID")
            if clientID == "" {
                clientID = r.RemoteAddr
            }
            
            if !crl.Allow(clientID) {
                w.Header().Set("Retry-After", "1")
                http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### Tiered Rate Limiting

```go
type Tier int

const (
    TierFree Tier = iota
    TierBasic
    TierPro
    TierEnterprise
)

type TieredRateLimiter struct {
    limits map[Tier]rate.Limit
    bursts map[Tier]int
    limiters sync.Map
}

func NewTieredRateLimiter() *TieredRateLimiter {
    return &TieredRateLimiter{
        limits: map[Tier]rate.Limit{
            TierFree:       rate.Limit(10),   // 10 req/sec
            TierBasic:      rate.Limit(100),  // 100 req/sec
            TierPro:        rate.Limit(1000), // 1000 req/sec
            TierEnterprise: rate.Inf,         // Unlimited
        },
        bursts: map[Tier]int{
            TierFree:       20,
            TierBasic:      200,
            TierPro:        2000,
            TierEnterprise: 10000,
        },
    }
}

func (trl *TieredRateLimiter) Allow(clientID string, tier Tier) bool {
    key := fmt.Sprintf("%s:%d", clientID, tier)
    
    limiter, ok := trl.limiters.Load(key)
    if !ok {
        newLimiter := rate.NewLimiter(trl.limits[tier], trl.bursts[tier])
        limiter, _ = trl.limiters.LoadOrStore(key, newLimiter)
    }
    
    return limiter.(*rate.Limiter).Allow()
}
```

### Endpoint-Specific Limits

```go
type EndpointRateLimiter struct {
    limits map[string]*rate.Limiter
    mu     sync.RWMutex
}

func NewEndpointRateLimiter(config map[string]int) *EndpointRateLimiter {
    erl := &EndpointRateLimiter{
        limits: make(map[string]*rate.Limiter),
    }
    
    for endpoint, rps := range config {
        erl.limits[endpoint] = rate.NewLimiter(rate.Limit(rps), rps*2)
    }
    
    return erl
}

func (erl *EndpointRateLimiter) Allow(endpoint string) bool {
    erl.mu.RLock()
    limiter, ok := erl.limits[endpoint]
    erl.mu.RUnlock()
    
    if !ok {
        return true // No limit configured
    }
    
    return limiter.Allow()
}

// Usage
config := map[string]int{
    "/api/search":  100,  // 100 req/sec
    "/api/upload":  10,   // 10 req/sec
    "/api/webhook": 1000, // 1000 req/sec
}
limiter := NewEndpointRateLimiter(config)
```

---

## Distributed Rate Limiting

For multi-instance deployments, use centralized storage.

### Redis-Based Rate Limiter

```go
import "github.com/go-redis/redis/v8"

type RedisRateLimiter struct {
    client     *redis.Client
    keyPrefix  string
    limit      int
    windowSize time.Duration
}

func NewRedisRateLimiter(client *redis.Client, limit int, window time.Duration) *RedisRateLimiter {
    return &RedisRateLimiter{
        client:     client,
        keyPrefix:  "ratelimit:",
        limit:      limit,
        windowSize: window,
    }
}

func (rrl *RedisRateLimiter) Allow(ctx context.Context, clientID string) (bool, error) {
    key := rrl.keyPrefix + clientID
    now := time.Now().UnixNano()
    windowStart := now - int64(rrl.windowSize)
    
    pipe := rrl.client.Pipeline()
    
    // Remove old entries
    pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
    
    // Count current entries
    countCmd := pipe.ZCard(ctx, key)
    
    // Add new entry
    pipe.ZAdd(ctx, key, &redis.Z{Score: float64(now), Member: now})
    
    // Set expiry
    pipe.Expire(ctx, key, rrl.windowSize)
    
    _, err := pipe.Exec(ctx)
    if err != nil {
        return false, err
    }
    
    count := countCmd.Val()
    return count < int64(rrl.limit), nil
}
```

### Lua Script for Atomic Operations

```go
const rateLimitScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- Remove old entries
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- Get current count
local count = redis.call('ZCARD', key)

if count < limit then
    -- Add new entry
    redis.call('ZADD', key, now, now)
    redis.call('EXPIRE', key, window / 1000000000)
    return 1
end

return 0
`

func (rrl *RedisRateLimiter) AllowAtomic(ctx context.Context, clientID string) (bool, error) {
    key := rrl.keyPrefix + clientID
    now := time.Now().UnixNano()
    
    result, err := rrl.client.Eval(ctx, rateLimitScript, []string{key}, 
        rrl.limit, 
        int64(rrl.windowSize), 
        now,
    ).Int()
    
    if err != nil {
        return false, err
    }
    
    return result == 1, nil
}
```

---

## Summary

| Algorithm | Pros | Cons | Use Case |
|-----------|------|------|----------|
| Token Bucket | Simple, allows bursts | Memory for tokens | General purpose |
| Sliding Window | Accurate, smooth | More memory | Strict limits |
| Fixed Window | Very simple | Boundary bursts | Low-stakes limits |
| Distributed | Multi-instance | Network latency | Microservices |

**Best Practices**:
1. Use `golang.org/x/time/rate` for local limiting
2. Add per-client limits to prevent abuse
3. Return `Retry-After` header on rejection
4. Monitor rate limit hits as a metric
5. Use distributed limiting for multi-instance

---

**Next**: [Load Shedding](./05-load-shedding.md)

