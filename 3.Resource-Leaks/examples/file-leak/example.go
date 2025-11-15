package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"time"
)

// FileProcessor simulates a service that processes many files
// BUG: Files are opened but never closed, leaking file descriptors
type FileProcessor struct {
	filesOpened int
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
	fmt.Printf("[START] Open file descriptors: %d\n", initialFDs)

	// Create temp directory for test files
	tempDir, err := os.MkdirTemp("", "file-leak-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Simulate continuous file processing
	ticker := time.NewTicker(20 * time.Millisecond) // 50 files/second
	defer ticker.Stop()

	startTime := time.Now()
	reportInterval := 2 * time.Second
	lastReport := startTime

	for range ticker.C {
		// BUG: processFile leaks file descriptors
		if err := processor.processFileBadly(tempDir); err != nil {
			log.Printf("Error processing file: %v", err)
		}

		// Report every 2 seconds
		if time.Since(lastReport) >= reportInterval {
			currentFDs := countOpenFileDescriptors()
			elapsed := time.Since(startTime).Seconds()
			fmt.Printf("[AFTER %.0fs] Open FDs: %d  |  Files opened: %d\n",
				elapsed, currentFDs, processor.filesOpened)

			if currentFDs > initialFDs+100 {
				fmt.Println("\n⚠️  WARNING: File descriptor leak detected!")
				fmt.Println("pprof server running on http://localhost:6060")
				fmt.Println("Run: curl http://localhost:6060/debug/pprof/goroutine > goroutine.pprof")
				fmt.Println("Run: lsof -p", os.Getpid(), "| wc -l")
			}

			lastReport = time.Now()
		}
	}
}

// processFileBadly opens a file but NEVER closes it - causing a leak
func (fp *FileProcessor) processFileBadly(tempDir string) error {
	filename := fmt.Sprintf("%s/logfile_%d.txt", tempDir, fp.filesOpened)

	// BUG: File is opened but never closed!
	file, err := os.Create(filename)
	if err != nil {
		return err
	}

	// Simulate some work
	data := []byte(fmt.Sprintf("Log entry %d\n", fp.filesOpened))
	if _, err := file.Write(data); err != nil {
		return err // Early return without closing file!
	}

	fp.filesOpened++

	// File is never closed - leak!
	return nil
}

// countOpenFileDescriptors returns approximate count of open file descriptors
func countOpenFileDescriptors() int {
	// On Unix-like systems, we can count files in /proc/self/fd/
	// This is a rough approximation

	// For cross-platform compatibility, we'll use a heuristic based on goroutines
	// In reality, use: lsof -p <pid> | wc -l
	return runtime.NumGoroutine() + len(os.Args) + 5 // Rough estimate
}
