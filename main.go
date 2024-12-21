package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	gitignore "github.com/sabhiram/go-gitignore"
)

// FileEntry represents a file to be processed with its metadata
type FileEntry struct {
	path    string
	info    os.FileInfo
	content []byte
	err     error
}

type IgnoreList struct {
	gitIgnore    *gitignore.GitIgnore
	singleIgnore *gitignore.GitIgnore
	mu           sync.RWMutex
}

func NewIgnoreList(dir string) (*IgnoreList, error) {
	il := &IgnoreList{}

	// Load .gitignore
	gitIgnorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitIgnorePath); err == nil {
		gitIgnore, err := gitignore.CompileIgnoreFile(gitIgnorePath)
		if err != nil {
			return nil, fmt.Errorf("error loading .gitignore: %v", err)
		}
		il.gitIgnore = gitIgnore
	}

	// Load .singlegenignore
	singleIgnorePath := filepath.Join(dir, ".singlegenignore")
	if _, err := os.Stat(singleIgnorePath); err == nil {
		singleIgnore, err := gitignore.CompileIgnoreFile(singleIgnorePath)
		if err != nil {
			return nil, fmt.Errorf("error loading .singlegenignore: %v", err)
		}
		il.singleIgnore = singleIgnore
	}

	return il, nil
}

func (il *IgnoreList) shouldIgnore(path string) bool {
	il.mu.RLock()
	defer il.mu.RUnlock()

	// Always ignore specific files and directories
	switch {
	case strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) ||
		strings.HasPrefix(path, ".git"+string(filepath.Separator)) ||
		path == ".git" ||
		path == ".gitignore" ||
		path == ".DS_Store" ||
		path == ".singlegenignore":
		return true
	}

	// Check gitignore patterns
	if il.gitIgnore != nil && il.gitIgnore.MatchesPath(path) {
		return true
	}

	// Check singlegenignore patterns
	if il.singleIgnore != nil && il.singleIgnore.MatchesPath(path) {
		return true
	}

	return false
}

func processFile(path string, info os.FileInfo) (*FileEntry, error) {
	if info.IsDir() {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return &FileEntry{
		path:    path,
		info:    info,
		content: content,
	}, nil
}

func writeFileEntry(outputFile *os.File, entry *FileEntry) error {
	header := fmt.Sprintf("\n### File: %s\n### Size: %d bytes\n### Last Modified: %s\n\n",
		entry.path, entry.info.Size(), entry.info.ModTime().Format("2006-01-02 15:04:05"))

	if _, err := outputFile.WriteString(header); err != nil {
		return err
	}

	if _, err := outputFile.Write(entry.content); err != nil {
		return err
	}

	if _, err := outputFile.WriteString("\n"); err != nil {
		return err
	}

	return nil
}

func worker(jobs <-chan string, results chan<- *FileEntry, ignoreList *IgnoreList, dirPath string, wg *sync.WaitGroup) {
	defer wg.Done()

	for path := range jobs {
		info, err := os.Stat(path)
		if err != nil {
			results <- &FileEntry{path: path, err: err}
			continue
		}

		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			results <- &FileEntry{path: path, err: err}
			continue
		}

		if ignoreList.shouldIgnore(relPath) {
			continue
		}

		entry, err := processFile(path, info)
		if err != nil {
			results <- &FileEntry{path: path, err: err}
			continue
		}

		if entry != nil {
			results <- entry
		}
	}
}

func main() {
	// Parse command line arguments
	dirPath := flag.String("dir", ".", "Directory to scan (default: current working directory)")
	outputPath := flag.String("output", "combined_output.txt", "Output file path")
	workers := flag.Int("workers", runtime.NumCPU(), "Number of worker goroutines")
	flag.Parse()

	// Create output file
	outputFile, err := os.Create(*outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outputFile.Close()

	// Initialize ignore lists
	ignoreList, err := NewIgnoreList(*dirPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Write header with metadata
	header := fmt.Sprintf("# Combined File Contents\n# Generated: %s\n# Source Directory: %s\n\n",
		time.Now().Format("2006-01-02 15:04:05"), *dirPath)
	if _, err := outputFile.WriteString(header); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing header: %v\n", err)
		os.Exit(1)
	}

	// Create channels for the worker pool
	jobs := make(chan string)
	results := make(chan *FileEntry)

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go worker(jobs, results, ignoreList, *dirPath, &wg)
	}

	// Start a goroutine to close results channel once all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Start a goroutine to walk the directory and send jobs
	go func() {
		err := filepath.Walk(*dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip the output file itself
			absOutputPath, _ := filepath.Abs(*outputPath)
			absPath, _ := filepath.Abs(path)
			if absPath == absOutputPath {
				return nil
			}

			jobs <- path
			return nil
		})

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
			os.Exit(1)
		}

		close(jobs)
	}()

	// Process results and write to output file
	for entry := range results {
		if entry.err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", entry.path, entry.err)
			continue
		}

		if err := writeFileEntry(outputFile, entry); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", entry.path, err)
		}
	}

	fmt.Printf("Successfully combined files into: %s\n", *outputPath)
}
