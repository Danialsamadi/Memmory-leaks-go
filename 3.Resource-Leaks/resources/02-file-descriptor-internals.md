# File Descriptor Internals and OS Limits

**Read Time**: ~20 minutes

**Prerequisites**: Basic Unix/Linux concepts, understanding of system calls

**Summary**: Deep dive into how operating systems manage file descriptors, why limits exist, what "too many open files" really means, and how to monitor and adjust FD usage.

---

## Introduction

File descriptors (FDs) are one of the most fundamental concepts in Unix-like operating systems, yet they're often misunderstood. When your Go application reports "too many open files," it's hitting OS-level limits on file descriptors—not actually running out of files.

## What is a File Descriptor?

A file descriptor is a **non-negative integer** that the kernel uses to identify an open file in a process. It's essentially an index into the process's file descriptor table.

### The Classic Unix Mantra

> "In Unix, everything is a file"

File descriptors represent:
- Regular files (`/var/log/app.log`)
- Directories (`/etc/`)
- Network sockets (TCP/UDP connections)
- Pipes (inter-process communication)
- Device files (`/dev/null`, `/dev/random`)
- Anonymous pipes (for command chaining)

```
Process File Descriptor Table:
┌────┬──────────────────────────┐
│ FD │ Points To                │
├────┼──────────────────────────┤
│  0 │ stdin (standard input)   │
│  1 │ stdout (standard output) │
│  2 │ stderr (standard error)  │
│  3 │ /var/log/app.log         │
│  4 │ TCP socket 192.168.1.5   │
│  5 │ /data/users.db           │
│  6 │ UDP socket :8080         │
│ ...│                          │
└────┴──────────────────────────┘
```

### How File Descriptors Work

When you open a file in Go:

```go
file, err := os.Open("/var/log/app.log")
// Internally:
// 1. Go calls open(2) syscall
// 2. Kernel allocates lowest available FD number
// 3. Kernel creates entry in process FD table
// 4. Returns FD to Go (wrapped in *os.File)
```

When you close it:

```go
file.Close()
// Internally:
// 1. Go calls close(2) syscall
// 2. Kernel removes entry from FD table
// 3. FD number becomes available for reuse
```

## The Three-Level Limit System

File descriptor limits exist at **three levels**:

```
┌─────────────────────────────────────────┐
│   System-Wide Limit                     │
│   (All processes combined)              │
│   /proc/sys/fs/file-max                 │
│   Typical: 100,000 - 10,000,000         │
└─────────────────────────────────────────┘
              ↑ constrains ↑
┌─────────────────────────────────────────┐
│   Per-User Limit                        │
│   (All processes for a user)            │
│   /etc/security/limits.conf             │
│   Typical: 10,000 - 100,000             │
└─────────────────────────────────────────┘
              ↑ constrains ↑
┌─────────────────────────────────────────┐
│   Per-Process Limit (RLIMIT_NOFILE)    │
│   (Single process)                      │
│   ulimit -n                             │
│   Typical: 1,024 - 65,536               │
└─────────────────────────────────────────┘
```

### 1. System-Wide Limit

**Linux**:
```bash
# View system-wide limit
cat /proc/sys/fs/file-max
# Output: 9223372036854775807 (theoretical max on modern systems)

# View current usage
cat /proc/sys/fs/file-nr
# Output: 1440  0  9223372036854775807
#         ^     ^  ^
#         used  |  max
#               allocated but unused
```

**macOS**:
```bash
# View system-wide limit
sysctl kern.maxfiles
# Output: kern.maxfiles: 122880

# View current usage
sysctl kern.num_files
# Output: kern.num_files: 3456
```

### 2. Per-User Limit

Configured in `/etc/security/limits.conf` (Linux):

```bash
# /etc/security/limits.conf
username  soft  nofile  10000
username  hard  nofile  100000
*         soft  nofile  4096
*         hard  nofile  65536
```

**Soft vs Hard Limits**:
- **Soft limit**: Default limit, can be raised by user up to hard limit
- **Hard limit**: Maximum limit, can only be raised by root

### 3. Per-Process Limit

The limit your Go application actually hits:

```bash
# View current process limit
ulimit -n
# Output: 1024

# Temporarily increase (soft limit, must be ≤ hard limit)
ulimit -n 4096

# View hard limit
ulimit -Hn
# Output: 65536
```

## Why Limits Exist

### Historical Reasons

In early Unix (1970s), file descriptor table was a fixed-size array. Limited hardware meant conservative limits (20-64 FDs per process).

### Modern Reasons

Even with modern hardware, limits serve important purposes:

1. **Prevent Resource Exhaustion**
   - Buggy applications can't consume all system FDs
   - One process can't starve others

2. **DoS Attack Prevention**
   - Malicious processes can't exhaust system resources
   - Fork bombs can't open infinite files

3. **Memory Management**
   - Each FD consumes kernel memory (file table entries)
   - Each open file has associated buffers

4. **Debugging and Monitoring**
   - High FD counts signal potential bugs
   - Early warning system for resource leaks

## Default Limits by Environment

| Environment | Typical Limit | Notes |
|-------------|---------------|-------|
| **Linux Desktop** | 1,024 | Conservative default |
| **Linux Server** | 4,096 - 65,536 | Often increased by admins |
| **macOS** | 256 - 10,240 | Very conservative |
| **Docker Container** | Inherits from host | Can be overridden |
| **Kubernetes Pod** | 1,048,576 | Very high by default |
| **systemd Service** | 65,536 | Configured via `LimitNOFILE` |

## What "Too Many Open Files" Really Means

When you see this error:

```
open /var/log/app.log: too many open files
```

It means:
1. Your process tried to open a file (or socket)
2. All available FD slots (0 to limit-1) are occupied
3. The kernel rejected the `open()` syscall with `EMFILE`

**It does NOT mean**:
- The filesystem is full
- There are too many files on disk
- System RAM is exhausted
- The file doesn't exist

### The Error Code Chain

```
Go Code:
    file, err := os.Open(path)
        ↓
Go Runtime:
    fd, errno := syscall.Open(path, O_RDONLY, 0)
        ↓
Linux Kernel:
    if (current_process_fd_count >= RLIMIT_NOFILE)
        return -EMFILE  // "Too many open files"
        ↓
Go Runtime:
    err = syscall.Errno(EMFILE)
        ↓
Go Code:
    if err != nil {
        // err.Error() == "too many open files"
    }
```

## Monitoring File Descriptor Usage

### Method 1: `/proc` Filesystem (Linux)

```bash
# Count FDs for a process
ls /proc/<PID>/fd | wc -l

# See what they are
ls -l /proc/<PID>/fd

# Example output:
lrwx------ 1 user user 64 Nov 15 10:00 0 -> /dev/pts/0
lrwx------ 1 user user 64 Nov 15 10:00 1 -> /dev/pts/0
lrwx------ 1 user user 64 Nov 15 10:00 2 -> /dev/pts/0
lrwx------ 1 user user 64 Nov 15 10:00 3 -> /var/log/app.log
lrwx------ 1 user user 64 Nov 15 10:00 4 -> socket:[12345]
```

### Method 2: `lsof` (Linux/macOS)

```bash
# List all open files for a process
lsof -p <PID>

# Count open files
lsof -p <PID> | wc -l

# Filter by type
lsof -p <PID> | grep REG   # Regular files
lsof -p <PID> | grep IPv4  # IPv4 sockets
lsof -p <PID> | grep IPv6  # IPv6 sockets

# Watch in real-time
watch -n 1 "lsof -p <PID> | wc -l"
```

### Method 3: `netstat` (Network Connections)

```bash
# Show all TCP connections
netstat -an | grep ESTABLISHED

# Count connections to specific port
netstat -an | grep :8080 | wc -l

# Show connection states
netstat -an | awk '{print $6}' | sort | uniq -c
```

### Method 4: Go Runtime Metrics

```go
package main

import (
    "fmt"
    "runtime"
    "time"
)

func monitorFileDescriptors() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // Goroutines (indirect indicator)
        goroutines := runtime.NumGoroutine()
        
        // Get process limits
        var rLimit syscall.Rlimit
        if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err == nil {
            fmt.Printf("FD Limit: %d (soft), %d (hard)\n", 
                rLimit.Cur, rLimit.Max)
        }
        
        // Note: Go doesn't expose current FD count directly
        // Use /proc or lsof externally
        fmt.Printf("Goroutines: %d\n", goroutines)
    }
}
```

## Increasing File Descriptor Limits

### Temporary (Current Shell Session)

```bash
# Increase soft limit (must be ≤ hard limit)
ulimit -n 4096

# Run your app
go run main.go
```

### Permanent (User-Level)

**Linux** - Edit `/etc/security/limits.conf`:

```bash
# /etc/security/limits.conf
myuser    soft    nofile    10000
myuser    hard    nofile    100000
```

Logout and login for changes to take effect.

**macOS** - Edit `/etc/sysctl.conf` or use `launchd.plist`:

```bash
# Increase system-wide limit
sudo sysctl -w kern.maxfiles=122880
sudo sysctl -w kern.maxfilesperproc=102400
```

### Application-Level (Programmatic)

```go
package main

import (
    "fmt"
    "log"
    "syscall"
)

func increaseFileDescriptorLimit() {
    var rLimit syscall.Rlimit
    
    // Get current limits
    if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Current limits: soft=%d, hard=%d\n", rLimit.Cur, rLimit.Max)
    
    // Increase soft limit to hard limit
    rLimit.Cur = rLimit.Max
    
    if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("New limits: soft=%d, hard=%d\n", rLimit.Cur, rLimit.Max)
}

func main() {
    increaseFileDescriptorLimit()
    // Your application code
}
```

### Docker Container

```dockerfile
# Dockerfile
FROM golang:1.21

# Set file descriptor limit
RUN echo "* soft nofile 65536" >> /etc/security/limits.conf && \
    echo "* hard nofile 65536" >> /etc/security/limits.conf
```

```bash
# docker run command
docker run --ulimit nofile=65536:65536 myapp
```

### Kubernetes Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp
spec:
  containers:
  - name: app
    image: myapp:latest
    resources:
      limits:
        # Note: FD limits not directly configurable
        # But can use init container
    securityContext:
      capabilities:
        add:
        - SYS_RESOURCE  # Allow raising limits
```

### systemd Service

```ini
# /etc/systemd/system/myapp.service
[Unit]
Description=My Go Application

[Service]
Type=simple
ExecStart=/usr/local/bin/myapp
LimitNOFILE=65536
# Soft:Hard format
LimitNOFILE=10000:65536

[Install]
WantedBy=multi-user.target
```

## File Descriptor Leak Detection Patterns

### Pattern 1: Steady Growth

```
FD Count over time:
100 → 150 → 200 → 250 → 300 ...
```

**Diagnosis**: Classic leak, every operation leaves FDs open.

**Solution**: Find unclosed files/sockets, add `defer Close()`.

### Pattern 2: Sawtooth Pattern

```
FD Count over time:
100 → 500 → 100 → 500 → 100 ...
```

**Diagnosis**: Periodic operations (batch jobs) that leak, but are cleaned up eventually (GC finalizers or timeouts).

**Solution**: Still a leak, but masked by eventual cleanup. Fix before traffic increases.

### Pattern 3: Spike and Plateau

```
FD Count over time:
100 → 1000 (spike) → 950 (plateau)
```

**Diagnosis**: Spike from traffic burst, some FDs leak but most are cleaned.

**Solution**: Partial cleanup, some code paths missing `defer Close()`.

### Pattern 4: Threshold Oscillation

```
FD Count over time:
1000 → 1024 (limit) → fail → 950 → 1024 → fail
```

**Diagnosis**: Running at limit, new operations fail and close old FDs, creating oscillation.

**Solution**: Immediate problem, requires restart and leak fix.

## Memory Cost of File Descriptors

Each open file descriptor costs:

**Kernel-side** (per FD):
- File table entry: ~256 bytes
- VFS inode cache: ~600 bytes
- Socket buffer (if network): 128 KB - 4 MB

**User-space** (per FD in Go):
- `*os.File` structure: ~48 bytes
- Runtime bookkeeping: ~32 bytes

**Example**:
- 10,000 leaked FDs ≈ 10 MB kernel memory + 1 MB Go memory
- Not huge, but kernel memory is more precious

**Real Problem**: Exhausting FD slots, not memory.

## Case Study: The Mystery of the Missing Files

**Symptom**: Production service reports "too many open files" but `lsof` shows only 200 FDs.

**Investigation**:

```bash
# Check soft limit
ulimit -n
# Output: 256

# Check FDs
lsof -p <PID> | wc -l
# Output: 198
```

**Diagnosis**: Service running under a different user with lower limits. The 256 limit was being hit.

**Solution**:

```bash
# Check limits for the actual service user
sudo -u serviceuser bash -c "ulimit -n"
# Output: 256 (aha!)

# Increase in systemd service file
LimitNOFILE=4096
```

**Lesson**: Always check limits in the actual runtime environment.

## Best Practices

1. **Monitor FD usage in production** with metrics (current/limit ratio)

2. **Alert at 70% of limit** to investigate before hitting the wall

3. **Test with low limits** during development (`ulimit -n 256`)

4. **Document FD requirements** for your application

5. **Set appropriate limits** based on expected load:
   - Simple CLI tool: 256 is fine
   - Web server handling 100 req/s: 4,096+
   - High-throughput proxy: 65,536+

6. **Never rely on finalizers** to close FDs (GC timing is unpredictable)

7. **Use connection pooling** for databases and HTTP clients

## Key Takeaways

1. **File descriptors are integers** indexing a process's open file table

2. **Three levels of limits**: system-wide, per-user, per-process

3. **"Too many open files" = FD limit reached**, not disk full

4. **Monitoring is essential**: Use `lsof`, `/proc/<pid>/fd`, and metrics

5. **Limits can be increased**, but fixing leaks is better

6. **Each FD costs kernel memory**, but exhaustion happens before memory is an issue

7. **Test with low limits** to expose leaks faster

---

## References

- `man 2 open` - open(2) system call
- `man 2 close` - close(2) system call
- `man limits.conf` - PAM limits configuration
- `man ulimit` - Shell resource limits
- https://www.kernel.org/doc/Documentation/sysctl/fs.txt - Kernel FS parameters

## Further Reading

- [Resource Lifecycle Patterns](01-resource-lifecycle.md) - Proper defer usage
- [Monitoring Strategies](06-monitoring-strategies.md) - Production monitoring
- [Production Case Studies](07-production-case-studies.md) - Real incidents

