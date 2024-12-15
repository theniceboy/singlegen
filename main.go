package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	gitignore "github.com/sabhiram/go-gitignore"
)

type IgnoreList struct {
	gitIgnore    *gitignore.GitIgnore
	singleIgnore *gitignore.GitIgnore
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
	// Always ignore specific files and directories
	switch {
	case strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) ||
		strings.HasPrefix(path, ".git"+string(filepath.Separator)) ||
		path == ".git" ||
		path == ".gitignore" ||
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

func processFile(path string, info os.FileInfo, outputFile *os.File) error {
	if info.IsDir() {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write file header with path and metadata
	header := fmt.Sprintf("\n### File: %s\n### Size: %d bytes\n### Last Modified: %s\n\n",
		path, info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
	if _, err := outputFile.WriteString(header); err != nil {
		return err
	}

	// Copy file content
	if _, err := io.Copy(outputFile, file); err != nil {
		return err
	}

	// Add a newline after the file content
	if _, err := outputFile.WriteString("\n"); err != nil {
		return err
	}

	return nil
}

func main() {
	// Parse command line arguments
	dirPath := flag.String("dir", ".", "Directory to scan (default: current working directory)")
	outputPath := flag.String("output", "combined_output.txt", "Output file path")
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

	// Walk through directory
	err = filepath.Walk(*dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the output file itself
		absOutputPath, _ := filepath.Abs(*outputPath)
		absPath, _ := filepath.Abs(path)
		if absPath == absOutputPath {
			return nil
		}

		// Check if path should be ignored
		relPath, err := filepath.Rel(*dirPath, path)
		if err != nil {
			return err
		}

		if ignoreList.shouldIgnore(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		return processFile(path, info, outputFile)
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing files: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully combined files into: %s\n", *outputPath)
}
