# Production Examples

**Read Time**: 30 minutes

**Related**: [Cache Patterns](./04-cache-patterns.md) | [Back to README](../README.md)

---

## Case Study 1: Session Store Memory Leak

**Problem**: Web application stored user sessions in an unbounded map, causing gradual memory growth over months.

**Impact**: 
- Memory grew to 8 GB over 3 months
- Application crashes during traffic spikes
- Restart required weekly

**Solution**: 
- Implemented TTL-based expiration (30-day inactive sessions)
- Added background cleanup goroutine
- Result: Memory stable at ~500 MB

## Case Study 2: Metrics Aggregation

**Problem**: Monitoring service kept all historical metrics in memory.

**Solution**:
- Changed to rolling window (last 24 hours)
- Older data written to time-series database
- Memory reduced from 12 GB to 200 MB

## Case Study 3: Image Thumbnail Cache

**Problem**: Image service cached all generated thumbnails, growing to 20 GB.

**Solution**:
- LRU cache with 1000-item limit
- Regenerate thumbnails on cache miss (acceptable latency)
- Memory stable at ~50 MB

---

**Return to**: [Long-Lived References README](../README.md)

