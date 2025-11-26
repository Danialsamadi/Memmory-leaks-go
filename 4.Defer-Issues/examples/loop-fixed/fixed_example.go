package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

// FileProcessor demonstrates the correct pattern: extracting to a function
// FIXED: Each file is closed immediately after processing
type FileProcessor struct {
	filesProcessed int64
	filesClosed    int64
}

func main() {
	// Start pprof server
	go func() {
		log.Println("pprof server running on http://localhost:6061")
		log.Fatal(http.ListenAndServe("localhost:6061", nil))
	}()

	processor := &FileProcessor{}

	// Print initial state
	initialFDs := countOpenFileDescriptors()
	fmt.Printf("[START] Open file descriptors: %d\n\n", initialFDs)

	// Create temp directory for test files
	tempDir, err := os.MkdirTemp("", "defer-loop-fixed-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	fmt.Println("Processing 500 files with extracted function pattern...")
	fmt.Println("Watch file descriptors stay stable!\n")

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
				closed := atomic.LoadInt64(&processor.filesClosed)
				fmt.Printf("[AFTER %.0fs] Open FDs: %d  |  Files processed: %d  |  Files closed: %d\n",
					elapsed, currentFDs, processed, closed)

				if currentFDs <= initialFDs+5 {
					fmt.Println("✓ No leak! File descriptors stable (max 1 file open at a time)")
				}
			case <-done:
				return
			}
		}
	}()

	// Process files with the correct extracted function pattern
	processor.processFilesCorrectly(tempDir, 500)

	// Stop monitoring
	done <- true

	fmt.Println("\n--- All files processed and closed immediately ---")
	finalFDs := countOpenFileDescriptors()
	fmt.Printf("[FINAL] Open FDs: %d (same as start - no accumulation)\n", finalFDs)
}

// processFilesCorrectly demonstrates the FIX: extract to a separate function
// Each file is opened, processed, and closed before moving to the next
func (fp *FileProcessor) processFilesCorrectly(tempDir string, numFiles int) {
	fmt.Printf("Entering processFilesCorrectly - will process %d files with proper cleanup\n\n", numFiles)

	for i := 0; i < numFiles; i++ {
		// ✅ FIX: Extract file processing to separate function
		// Defer executes at end of processOneFile, not end of this function
		err := fp.processOneFile(tempDir, i)
		if err != nil {
			log.Printf("Error processing file %d: %v", i, err)
		}
	}

	fmt.Printf("\nLoop complete. All %d files processed and closed.\n", numFiles)
	fmt.Printf("Files processed: %d, Files closed: %d\n",
		atomic.LoadInt64(&fp.filesProcessed),
		atomic.LoadInt64(&fp.filesClosed))
}

// processOneFile handles a single file - defer executes at end of THIS function
func (fp *FileProcessor) processOneFile(tempDir string, index int) error {
	filename := fmt.Sprintf("%s/logfile_%d.txt", tempDir, index)

	// Create the file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}

	// ✅ FIX: This defer executes when processOneFile returns
	// NOT when the calling function's loop ends!
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
		atomic.AddInt64(&fp.filesClosed, 1)
	}()

	// Simulate some work
	data := []byte(fmt.Sprintf("Log entry %d - timestamp: %v\n", index, time.Now()))
	if _, err := file.Write(data); err != nil {
		return err
	}

	atomic.AddInt64(&fp.filesProcessed, 1)

	// Slow down to match the leaky version timing
	time.Sleep(10 * time.Millisecond)

	return nil
	// File is closed HERE by defer, before next iteration
}

// countOpenFileDescriptors returns approximate count of open file descriptors
func countOpenFileDescriptors() int {
	// Try to read from /dev/fd on macOS or /proc/self/fd on Linux
	if entries, err := os.ReadDir("/dev/fd"); err == nil {
		return len(entries)
	}
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		return len(entries)
	}
	// Fallback: rough estimate
	return runtime.NumGoroutine() + 5
}

