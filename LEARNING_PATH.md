# Learning Path for Go Memory Leaks

This guide provides structured learning paths based on your experience level and specific goals. Follow the path that best matches your needs.

## Table of Contents

- [By Experience Level](#by-experience-level)
  - [Beginner (Start Here)](#beginner-start-here)
  - [Intermediate](#intermediate)
  - [Advanced](#advanced)
- [By Use Case](#by-use-case)
  - [I Have a Memory Leak in Production](#i-have-a-memory-leak-in-production)
  - [I Want to Learn Go Concurrency](#i-want-to-learn-go-concurrency)
  - [I Need to Teach Others About This](#i-need-to-teach-others-about-this)
  - [I'm Code Reviewing](#im-code-reviewing)
- [By Time Available](#by-time-available)
  - [30 Minutes Quick Start](#30-minutes-quick-start)
  - [Half Day Workshop](#half-day-workshop)
  - [Week-Long Deep Dive](#week-long-deep-dive)

---

## By Experience Level

### Beginner (Start Here)

**Estimated Time**: 2-3 hours

If you're new to Go memory leaks or profiling, follow this path:

#### Step 1: Setup and Fundamentals (30 minutes)

1. **Read**: [Root README Quick Start](./README.md#quick-start)
2. **Install**: Ensure you have Go 1.20+, graphviz installed
3. **Read**: [pprof Complete Guide - Installation Section](./tools-setup/pprof-complete-guide.md)
4. **Test**: Run `go version` and `dot -version` to verify setup

#### Step 2: Your First Memory Leak (45 minutes)

1. **Navigate**: `cd 1.Goroutine-Leaks-Most-Common`
2. **Run**: The leaky example
   ```bash
   go run example.go
   ```
3. **Observe**: Watch goroutine count grow in the terminal output
4. **Read**: [Goroutine Leaks README](./1.Goroutine-Leaks-Most-Common/README.md)
5. **Understand**: Why this happens (conceptual level)

#### Step 3: First Profiling Experience (30 minutes)

1. **Run**: `example.go` again in one terminal
2. **Collect**: Profile in another terminal
   ```bash
   curl http://localhost:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof
   ```
3. **View**: Profile in browser
   ```bash
   go tool pprof -http=:8081 goroutine_fixedEX.pprof
   ```
4. **Read**: [pprof Analysis for Goroutine Leaks](./1.Goroutine-Leaks-Most-Common/pprof_analysis.md)
5. **Compare**: Expected output vs. what you see

#### Step 4: Understanding the Fix (30 minutes)

1. **Read**: [Conceptual Explanation](./1.Goroutine-Leaks-Most-Common/resources/01-conceptual-explanation.md)
2. **Read**: Code comparison between `example.go` and `fixed_example.go`
3. **Run**: Fixed version
   ```bash
   go run fixed_example.go
   ```
4. **Observe**: Goroutine count remains stable
5. **Collect**: Profile from fixed version and compare

#### Step 5: Reinforce Learning (30 minutes)

1. **Read**: [Context Pattern Guide](./1.Goroutine-Leaks-Most-Common/resources/04-context-pattern.md)
2. **Read**: [Visual Diagrams](./1.Goroutine-Leaks-Most-Common/resources/06-visual-diagrams.md)
3. **Review**: Key takeaways section in the README
4. **Try**: Modify the example to create your own variation

#### Next Steps for Beginners

Once comfortable with goroutine leaks:
- Move to [Resource Leaks](./3.Resource-Leaks/) (simpler concept)
- Then try [Long-Lived References](./2.Long-Lived-References/)
- Use the [Detection Decision Tree](./visual-guides/detection-decision-tree.md) as reference

---

### Intermediate

**Estimated Time**: 8-12 hours

You have Go experience and understand basic concurrency. Ready to master memory leaks:

#### Phase 1: Complete All Five Leak Types (5 hours)

Work through each leak type systematically:

1. **Goroutine Leaks** (1 hour)
   - Run both examples
   - Read README and first 3 resources
   - Complete pprof analysis
   - [Start here](./1.Goroutine-Leaks-Most-Common/)

2. **Long-Lived References** (1.5 hours)
   - Run cache and reslicing examples
   - Understand GC behavior with heap profiles
   - Read memory model explanation
   - [Start here](./2.Long-Lived-References/)

3. **Resource Leaks** (1 hour)
   - Run file and ticker examples
   - Use `lsof` to observe file descriptors
   - Study cleanup patterns
   - [Start here](./3.Resource-Leaks/)

4. **Defer Issues** (45 minutes)
   - Run loop examples
   - Understand defer LIFO mechanics
   - Learn refactoring strategies
   - [Start here](./4.Defer-Issues/)

5. **Unbounded Resources** (45 minutes)
   - Run goroutine spawning examples
   - Implement worker pool pattern
   - Study semaphore approaches
   - [Start here](./5.Unbounded-Resources/)

#### Phase 2: Deep Dive into Internals (3 hours)

For each leak type, read the `02-*-internals.md` and `03-*-mechanics.md` resources:

1. [Goroutine Internals](./1.Goroutine-Leaks-Most-Common/resources/02-goroutine-internals.md)
2. [GC Behavior](./2.Long-Lived-References/resources/02-gc-behavior.md)
3. [Timer/Ticker Mechanics](./3.Resource-Leaks/resources/03-timer-ticker-mechanics.md)
4. [Defer Stack Internals](./4.Defer-Issues/resources/02-defer-stack-internals.md)
5. [Scheduler Internals](./5.Unbounded-Resources/resources/03-scheduler-internals.md)

#### Phase 3: Practice Detection (2 hours)

1. **Study**: [Detection Decision Tree](./visual-guides/detection-decision-tree.md)
2. **Review**: Detection methods for each leak type
3. **Practice**: Create profiles and compare
4. **Use**: [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md) as reference

#### Phase 4: Real-World Context (2 hours)

Read all `07-real-world-examples.md` files:
1. [Goroutine Real-World Examples](./1.Goroutine-Leaks-Most-Common/resources/07-real-world-examples.md)
2. [Cache Real-World Examples](./2.Long-Lived-References/resources/07-production-examples.md)
3. [Resource Real-World Examples](./3.Resource-Leaks/resources/07-best-practices.md)
4. [Defer Real-World Examples](./4.Defer-Issues/resources/07-benchmarks.md)
5. [Pool Real-World Examples](./5.Unbounded-Resources/resources/07-production-patterns.md)

#### Intermediate Milestones

You should now be able to:
- Identify which type of leak you're dealing with
- Set up profiling in your own applications
- Interpret pprof output confidently
- Apply appropriate fix patterns
- Explain memory leak concepts to others

---

### Advanced

**Estimated Time**: 20+ hours

You're proficient with Go and ready to master memory management at an expert level:

#### Phase 1: Master All Learning Materials (8 hours)

Read **every** resource file in **every** leak type directory:
- All 7 resources per leak type = 35 documents
- Take notes on patterns and edge cases
- Cross-reference between similar concepts

#### Phase 2: Tooling Mastery (5 hours)

Complete all tool setup guides:
1. [pprof Complete Guide](./tools-setup/pprof-complete-guide.md) - Master all commands
2. [go tool trace Guide](./tools-setup/go-tool-trace-guide.md) - Understand execution tracing
3. [Delve Debugging Guide](./tools-setup/delve-debugging-guide.md) - Interactive debugging
4. [GoLand Profiling](./tools-setup/goland-profiling.md) - IDE integration
5. [VS Code Debugging](./tools-setup/vscode-debugging.md) - Editor setup
6. [Docker Profiling](./tools-setup/docker-profiling.md) - Container profiling

**Practice**:
- Use each tool on the same leak
- Compare insights from different tools
- Automate profile collection

#### Phase 3: Custom Examples and Experiments (5 hours)

1. **Create**: Your own leak examples combining multiple patterns
2. **Measure**: Performance impact with benchmarks
3. **Experiment**: Edge cases not covered in examples
4. **Document**: Your findings

Example experiments:
- What happens with 10,000 leaked goroutines?
- How does GC pressure affect detection?
- Can you create a leak that's hard to detect?
- What's the memory overhead per goroutine?

#### Phase 4: Production-Ready Knowledge (3 hours)

1. **Study**: All production examples in detail
2. **Research**: GitHub issues in major Go projects related to memory leaks
3. **Analyze**: How major projects (Kubernetes, Docker, etcd) handle these patterns
4. **Document**: Patterns you discover

#### Phase 5: Contribute (Ongoing)

1. Add new examples from your experience
2. Improve documentation where you found it unclear
3. Create additional visual guides
4. Write blog posts or give talks using this material

#### Advanced Milestones

You should now be able to:
- Debug memory leaks in production under pressure
- Design systems that prevent these leaks from the start
- Create custom profiling and detection tooling
- Teach others effectively about memory management
- Contribute improvements to Go tooling

---

## By Use Case

### I Have a Memory Leak in Production

**Urgent debugging path** (1-2 hours):

#### Step 1: Identify the Leak Type (15 minutes)

1. **Use**: [Detection Decision Tree](./visual-guides/detection-decision-tree.md)
2. **Check**: Basic metrics
   ```bash
   # Goroutine count
   curl http://your-app:6060/debug/pprof/goroutine?debug=1
   
   # Memory stats
   curl http://your-app:6060/debug/pprof/heap?debug=1
   
   # File descriptors (Linux)
   lsof -p $(pgrep your-app) | wc -l
   ```

3. **Analyze**: Growth pattern
   - Steady linear growth → Long-lived references or goroutine leak
   - Sudden jumps → Resource creation without cleanup
   - Growth under load → Unbounded resources

#### Step 2: Collect Profiles (10 minutes)

```bash
# Collect all relevant profiles
curl http://your-app:6060/debug/pprof/goroutine > goroutine_fixedEX.pprof
curl http://your-app:6060/debug/pprof/heap > heap.pprof
curl http://your-app:6060/debug/pprof/allocs > allocs.pprof

# Wait 5 minutes, collect again for comparison
sleep 300
curl http://your-app:6060/debug/pprof/goroutine > goroutine_5min.pprof
curl http://your-app:6060/debug/pprof/heap > heap_5min.pprof
```

#### Step 3: Analyze Profiles (20 minutes)

```bash
# Compare profiles to see what's growing
go tool pprof -base=goroutine_fixedEX.pprof goroutine_5min.pprof
go tool pprof -base=heap.pprof heap_5min.pprof
```

Look for:
- Functions appearing in growth diff
- Blocked goroutines in goroutine profile
- Large allocations in heap profile

#### Step 4: Match to Leak Type (10 minutes)

Based on profiles, jump to the appropriate directory:

- **Growing goroutines in blocked state** → [1.Goroutine-Leaks-Most-Common](./1.Goroutine-Leaks-Most-Common/)
- **Growing heap with cache-like structures** → [2.Long-Lived-References](./2.Long-Lived-References/)
- **Growing file descriptors or timers** → [3.Resource-Leaks](./3.Resource-Leaks/)
- **Stack growth in loops** → [4.Defer-Issues](./4.Defer-Issues/)
- **Goroutines growing under load** → [5.Unbounded-Resources](./5.Unbounded-Resources/)

#### Step 5: Apply Fix Pattern (30 minutes)

1. Read the relevant `pprof_analysis.md`
2. Study the `fixed_example.go` for that leak type
3. Apply the fix pattern to your code
4. Test in staging environment

#### Step 6: Verify Fix (15 minutes)

1. Deploy to staging/canary
2. Collect profiles after running for a while
3. Verify metrics stabilize
4. Monitor for 24 hours before full rollout

---

### I Want to Learn Go Concurrency

**Concurrency-focused path** (6 hours):

#### Part 1: Goroutine Lifecycle (2 hours)

1. **Start**: [Goroutine Leaks Overview](./1.Goroutine-Leaks-Most-Common/README.md)
2. **Read**: [Goroutine Internals](./1.Goroutine-Leaks-Most-Common/resources/02-goroutine-internals.md)
3. **Read**: [Channel Mechanics](./1.Goroutine-Leaks-Most-Common/resources/03-channel-mechanics.md)
4. **Practice**: Run examples and observe behavior

#### Part 2: Context and Cancellation (1.5 hours)

1. **Read**: [Context Pattern](./1.Goroutine-Leaks-Most-Common/resources/04-context-pattern.md)
2. **Study**: How `fixed_example.go` uses context
3. **Practice**: Add context to your own code
4. **Read**: [Visual Diagrams](./1.Goroutine-Leaks-Most-Common/resources/06-visual-diagrams.md)

#### Part 3: Concurrency Patterns (2 hours)

1. **Read**: [Concurrency Patterns](./5.Unbounded-Resources/resources/01-concurrency-patterns.md)
2. **Read**: [Worker Pool Pattern](./5.Unbounded-Resources/resources/04-worker-pool-pattern.md)
3. **Read**: [Semaphore Pattern](./5.Unbounded-Resources/resources/06-semaphore-pattern.md)
4. **Study**: [Scheduler Internals](./5.Unbounded-Resources/resources/03-scheduler-internals.md)

#### Part 4: Practical Application (30 minutes)

1. **Implement**: Your own worker pool
2. **Test**: With various worker counts
3. **Profile**: Your implementation
4. **Compare**: With unbuffered approaches

---

### I Need to Teach Others About This

**Teaching preparation path** (4 hours):

#### Preparation Phase (2 hours)

1. **Review**: All visual guides
   - [Memory Leak Flowchart](./visual-guides/memory-leak-flowchart.md)
   - [Detection Decision Tree](./visual-guides/detection-decision-tree.md)
   - [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md)
   - [Goroutine Timeline](./visual-guides/goroutine-timeline.md)
   - [Memory Growth Patterns](./visual-guides/memory-growth-patterns.md)

2. **Practice**: Running each example live
3. **Prepare**: Slides using visual guides
4. **Test**: pprof display on projector/screen sharing

#### Teaching Structure Suggestions

**For 1-Hour Workshop**:
- 10 min: Overview of leak types (use root README table)
- 20 min: Live demo of goroutine leak (most relatable)
- 20 min: Show pprof analysis
- 10 min: Show fix and compare

**For Half-Day Workshop**:
- Follow the [Beginner Path](#beginner-start-here)
- Add hands-on exercises
- Have attendees run examples themselves

**For Multi-Day Course**:
- Follow the [Intermediate Path](#intermediate)
- Add code review exercises
- Group debugging sessions
- Production case study discussions

#### Teaching Resources to Use

1. **Live Demos**: All `example.go` files
2. **Visual Aids**: All files in `visual-guides/`
3. **Case Studies**: All `07-real-world-examples.md` files
4. **Exercises**: Have students fix intentionally broken code
5. **Cheat Sheet**: [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md)

---

### I'm Code Reviewing

**Code review reference** (ongoing):

#### Before Reviews: Study Patterns (2 hours)

1. **Read**: [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md)
2. **Study**: The "fixed" version of each example
3. **Review**: Detection methods for each leak type
4. **Bookmark**: This learning path for quick reference

#### During Code Reviews: Checklist

Use this checklist for Go code reviews:

**Goroutine Creation**:
- [ ] Is there a goroutine spawn? Check `go func()`
- [ ] Is there cancellation via context?
- [ ] Are channels properly closed?
- [ ] Is there a timeout for blocking operations?
- Reference: [Goroutine Leaks](./1.Goroutine-Leaks-Most-Common/)

**Caches and Collections**:
- [ ] Is there a cache? Is there eviction?
- [ ] Are there size limits?
- [ ] Is there a TTL/expiration?
- [ ] Are large slices being resliced?
- Reference: [Long-Lived References](./2.Long-Lived-References/)

**Resource Usage**:
- [ ] Are files opened? Is there `defer file.Close()`?
- [ ] HTTP response bodies closed?
- [ ] Timers/Tickers stopped?
- [ ] Database connections returned?
- Reference: [Resource Leaks](./3.Resource-Leaks/)

**Defer Usage**:
- [ ] Is defer used in a loop?
- [ ] Is the loop processing many items?
- [ ] Should cleanup be immediate instead?
- Reference: [Defer Issues](./4.Defer-Issues/)

**Concurrency Under Load**:
- [ ] Unbounded goroutine creation per request?
- [ ] Is there rate limiting?
- [ ] Worker pool pattern used?
- [ ] Channel buffer sizes appropriate?
- Reference: [Unbounded Resources](./5.Unbounded-Resources/)

---

## By Time Available

### 30 Minutes Quick Start

Speed run for immediate value:

1. **5 min**: Read [Root README Overview](./README.md#overview)
2. **10 min**: Run [Goroutine Leak Example](./1.Goroutine-Leaks-Most-Common/example.go)
3. **10 min**: Collect and view pprof profile
4. **5 min**: Run fixed version and compare

**Outcome**: Understand one leak type and basic profiling

---

### Half Day Workshop

Comprehensive introduction (4 hours):

**Morning Session** (2 hours):
1. **30 min**: Setup and introduction
2. **45 min**: Goroutine leaks (complete)
3. **45 min**: Resource leaks (complete)

**Break** (15 minutes)

**Afternoon Session** (1.75 hours):
1. **30 min**: Long-lived references
2. **45 min**: Unbounded resources with worker pool
3. **20 min**: Review and Q&A

**Outcome**: Hands-on experience with four major leak types

---

### Week-Long Deep Dive

Comprehensive mastery (40 hours):

**Day 1**: Fundamentals and Goroutine Leaks
- Morning: Setup, tooling, basic profiling
- Afternoon: Goroutine leaks in depth, all resources

**Day 2**: Memory Management
- Morning: Long-lived references, GC behavior
- Afternoon: Heap profiling, optimization techniques

**Day 3**: Resource Management
- Morning: Resource leaks, file descriptors
- Afternoon: Timer management, cleanup patterns

**Day 4**: Advanced Patterns
- Morning: Defer mechanics, unbounded resources
- Afternoon: Worker pools, semaphores, rate limiting

**Day 5**: Production Application
- Morning: Real-world case studies
- Afternoon: Debug production-like scenarios, final project

**Outcome**: Expert-level understanding and practical experience

---

## Progress Tracking

Use this checklist to track your progress:

### Core Examples
- [ ] Goroutine Leaks example run
- [ ] Long-Lived References cache example run
- [ ] Long-Lived References reslicing example run
- [ ] Resource Leaks files example run
- [ ] Resource Leaks ticker example run
- [ ] Defer Issues loop example run
- [ ] Unbounded Resources example run

### Profiling Skills
- [ ] Collected goroutine profile
- [ ] Collected heap profile
- [ ] Used pprof web UI
- [ ] Used pprof CLI
- [ ] Compared profiles with -base flag
- [ ] Interpreted growth patterns

### Conceptual Understanding
- [ ] Can explain each leak type
- [ ] Understand when each occurs
- [ ] Know appropriate fix patterns
- [ ] Can identify leaks in unfamiliar code

### Practical Application
- [ ] Added pprof to own project
- [ ] Fixed a real memory leak
- [ ] Implemented worker pool pattern
- [ ] Used context for cancellation
- [ ] Reviewed code for leak patterns

---

## Next Steps

After completing your chosen path:

1. **Apply**: Use these patterns in your projects
2. **Monitor**: Set up profiling in staging/production
3. **Share**: Teach teammates what you learned
4. **Contribute**: Add examples from your experience
5. **Stay Current**: Follow Go release notes for tooling improvements

## Questions or Stuck?

- Review the [Detection Decision Tree](./visual-guides/detection-decision-tree.md)
- Check the [Fix Strategies Matrix](./visual-guides/fix-strategies-matrix.md)
- Re-read the relevant leak type's conceptual explanation
- Study the real-world examples for similar scenarios
- Experiment with modifications to the examples

---

**Ready to Start?** Go to [Root README](./README.md) or jump directly to [Goroutine Leaks](./1.Goroutine-Leaks-Most-Common/)

