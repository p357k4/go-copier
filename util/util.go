// util/util.go
package util

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// EnsureDirExists checks if a directory exists and creates it if it doesn't.
func EnsureDirExists(dirPath string) error {
	// 0755: owner read/write/execute, group read/execute, others read/execute
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dirPath, err)
	}
	return nil
}

// MoveFile renames a file from srcPath to destPath.
// Assumes srcPath and destPath are on the same device for efficient rename.
func MoveFile(srcPath, destPath string) error {
	// Ensure the destination directory exists before attempting to move
	destDir := filepath.Dir(destPath)
	if err := EnsureDirExists(destDir); err != nil {
		return fmt.Errorf("failed to ensure destination directory exists for %q: %w", destPath, err)
	}

	// Rename the file
	// os.Rename is an atomic operation on POSIX systems if src and dest are on the same filesystem.
	err := os.Rename(srcPath, destPath)
	if err != nil {
		return fmt.Errorf("failed to move file from %q to %q: %w", srcPath, destPath, err)
	}

	return nil
}

// GetFiles walks the directory recursively and returns a list of file paths.
// It respects the context for cancellation. Errors encountered during walk
// for individual entries are logged but do not stop the walk unless the
// context is cancelled.
func GetFiles(ctx context.Context, rootDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		// Check context cancellation at the start of the walk function
		select {
		case <-ctx.Done():
			// Propagate context cancellation error to stop the walk
			return ctx.Err()
		default:
		}

		if err != nil {
			// Log the error for this specific entry but continue walking
			slog.Warn("error walking directory entry", "path", path, "error", err)
			return nil // Skip this entry but continue the walk
		}

		// Only add files, ignore directories
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	// Check if the error returned by WalkDir was due to context cancellation
	if err == context.Canceled {
		return nil, err
	}
	// Return any other errors from WalkDir
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %q: %w", rootDir, err)
	}

	return files, nil
}

// GetFileInfoMap walks the directory recursively and returns a map of file path to file size.
// It respects the context for cancellation. Errors encountered during walk or stat
// for individual entries are logged but do not stop the walk unless the
// context is cancelled.
func GetFileInfoMap(ctx context.Context, rootDir string) (map[string]int64, error) {
	fileInfoMap := make(map[string]int64)
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err() // Propagate context cancellation
		default:
		}

		if err != nil {
			slog.Warn("error walking directory entry for info map", "path", path, "error", err)
			return nil // Skip this entry, continue walk
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			slog.Warn("error getting file info", "path", path, "error", err)
			return nil // Skip this file entry, continue walk
		}

		fileInfoMap[path] = info.Size()
		return nil
	})

	if err == context.Canceled {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get file info map for %q: %w", rootDir, err)
	}

	return fileInfoMap, nil
}

// SafeBaseName returns the base name of a path, replacing potentially problematic
// characters (like path separators or leading dots) with underscores to make
// it safe for use as a filename component, especially in flat directories.
// This helps prevent path traversal issues if the original path contains ".."
// or starts with "/".
func SafeBaseName(path string) string {
	// Get the base name
	base := filepath.Base(path)

	// Replace any path separators within the base name (shouldn't happen with filepath.Base, but defensive)
	base = strings.ReplaceAll(base, string(filepath.Separator), "_")
	base = strings.ReplaceAll(base, "/", "_") // Handle forward slash on all OS

	// Handle potential leading dots from hidden files or special names like "." or ".."
	// Simple replace won't handle "." or ".." correctly if they are the entire name.
	// filepath.Base already handles "." and ".." by returning them literally.
	// If the base name is "." or "..", replace it to avoid issues.
	if base == "." {
		return "_"
	}
	if base == ".." {
		return "__"
	}

	// Replace leading dots that are part of filenames like ".config"
	if strings.HasPrefix(base, ".") {
		base = "_" + strings.TrimPrefix(base, ".")
	}

	return base
}
