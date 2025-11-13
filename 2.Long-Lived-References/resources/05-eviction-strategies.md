# Eviction Strategies

**Read Time**: 20 minutes

**Related**: [Cache Patterns](./04-cache-patterns.md) | [Back to README](../README.md)

---

## Common Strategies

### 1. LRU (Least Recently Used)
Evicts items not accessed recently. Best for general-purpose caching.

### 2. LFU (Least Frequently Used)
Evicts items accessed least often. Good when access patterns are stable.

### 3. TTL (Time To Live)
Evicts items after a fixed time. Simple, predictable memory usage.

### 4. Size-Based
Evicts when total size exceeds limit. Good for memory-constrained systems.

### 5. Random
Evicts random items. Simple, surprisingly effective for some workloads.

## Choosing a Strategy

- **LRU**: General purpose, good performance
- **TTL**: Predictable expiration needed
- **Size**: Strict memory limits required
- **Combination**: Use multiple strategies together

---

**Return to**: [Long-Lived References README](../README.md)

