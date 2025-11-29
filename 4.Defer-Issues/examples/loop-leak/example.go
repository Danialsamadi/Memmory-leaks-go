package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// FileProcessor demonstrates the defer-in-loop anti-pattern
// BUG: Defer statements accumulate in the loop, keeping all files open
// until the function returns
type FileProcessor struct {
	filesProcessed int64
	pendingDefers  int64
}

func main() {
	// Start pprof server
	go func() {
		log.Println("pprof server running on http://localhost:6060")
		log.Fatal(http.ListenAndServe("localhost:6060", nil))
	}()

	processor := &FileProcessor{}

	// Print initial state
	initialFDs := countOpenFileDescriptors()
	fmt.Printf("[START] Open file descriptors: %d\n\n", initialFDs)

	// Create temp directory for test files
	tempDir, err := os.MkdirTemp("", "defer-loop-leak-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fmt.Println("Processing 500 files with defer-in-loop pattern...")
	fmt.Println("Watch file descriptors grow until function returns!\n")

	// Start monitoring goroutine
	done := make(chan bool)
	go func() {
		startTime := time.Now()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				currentFDs := countOpenFileDescriptors()
				elapsed := time.Since(startTime).Seconds()
				processed := atomic.LoadInt64(&processor.filesProcessed)
				pending := atomic.LoadInt64(&processor.pendingDefers)
				fmt.Printf("[AFTER %.0fs] Open FDs: %d  |  Files processed: %d  |  Pending defers: %d\n",
					elapsed, currentFDs, processed, pending)

				if currentFDs > initialFDs+100 {
					fmt.Println("\n⚠️  WARNING: Defer accumulation detected!")
					fmt.Println("All files remain open until the function returns.")
				}
			case <-done:
				return
			}
		}
	}()

	// Process files with the buggy defer-in-loop pattern
	processor.processFilesBadly(tempDir, 500)

	// Stop monitoring
	done <- true

	fmt.Println("\n--- Function returned, all defers have now executed ---")
	finalFDs := countOpenFileDescriptors()
	fmt.Printf("[FINAL] Open FDs: %d (back to normal after defers executed)\n", finalFDs)
}

// processFilesBadly demonstrates the ANTI-PATTERN: defer inside a loop
// All 500 files will be opened and stay open until this function returns
func (fp *FileProcessor) processFilesBadly(tempDir string, numFiles int) {
	fmt.Printf("Entering processFilesBadly - will open %d files with defer in loop\n\n", numFiles)

	for i := 0; i < numFiles; i++ {
		filename := fmt.Sprintf("%s/logfile_%d.txt", tempDir, i)

		// Create the file
		file, err := os.Create(filename)
		if err != nil {
			log.Printf("Error creating file: %v", err)
			continue
		}

		// BUG: This defer accumulates!
		// It won't execute until processFilesBadly returns
		// All files stay open during the entire loop!
		defer file.Close()
		atomic.AddInt64(&fp.pendingDefers, 1)

		// Simulate some work
		data := []byte(fmt.Sprintf("Log entry %d - timestamp: %v\n", i, time.Now()))
		if _, err := file.Write(data); err != nil {
			log.Printf("Error writing to file: %v", err)
			continue
		}

		atomic.AddInt64(&fp.filesProcessed, 1)

		// Slow down to see the accumulation
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Printf("\nLoop complete. All %d files processed.\n", numFiles)
	fmt.Printf("Pending defers: %d - about to execute as function returns...\n",
		atomic.LoadInt64(&fp.pendingDefers))

	// All defers execute HERE, in LIFO order
}

// countOpenFileDescriptors returns count of open file descriptors
func countOpenFileDescriptors() int {
	pid := os.Getpid()

	// Try using lsof on macOS/Linux (most accurate)
	cmd := exec.Command("lsof", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err == nil {
		// Count lines, subtract 1 for header
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) > 1 {
			return len(lines) - 1
		}
	}

	// Fallback: Try /proc/self/fd on Linux
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		return len(entries)
	}

	// Fallback: Try /dev/fd on macOS (less accurate but better than nothing)
	if entries, err := os.ReadDir("/dev/fd"); err == nil {
		return len(entries)
	}

	// Last resort: rough estimate
	return runtime.NumGoroutine() + 5
}
