// filter/filter.go
package filter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go-copier/config" // Replace with your actual module path
	"go-copier/util"   // Replace with your actual module path
)

// FileFilter filters files based on content predicate.
type FileFilter struct {
	config    *config.Config
	predicate func(filePath string) (bool, error)
}

// NewFileFilter creates a new FileFilter.
func NewFileFilter(cfg *config.Config) *FileFilter {
	placeholderPredicate := func(filePath string) (bool, error) {
		info, err := os.Stat(filePath)
		if err != nil {
			return false, fmt.Errorf("predicate failed to stat file %q: %w", filePath, err)
		}
		return info.Size() > 0, nil // Example: accept if file is not empty
	}

	return &FileFilter{
		config:    cfg,
		predicate: placeholderPredicate, // Use the placeholder
	}
}

// Run starts the filtering process. It runs until the context is cancelled.
func (f *FileFilter) Run(ctx context.Context) {
	slog.Info("FileFilter started", "source", f.config.FilesLandedDir)

	ticker := time.NewTicker(f.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("FileFilter shutting down", "source", f.config.FilesLandedDir)
			return
		case <-ticker.C:
			f.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and filters files.
func (f *FileFilter) processFiles(ctx context.Context) {
	slog.Debug("FileFilter scanning", "source", f.config.FilesLandedDir)

	files, err := util.GetFiles(ctx, f.config.FilesLandedDir)
	if err != nil {
		slog.Error("FileFilter failed to scan directory", "source", f.config.FilesLandedDir, "error", err)
		return
	}

	for _, path := range files {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		slog.Debug("FileFilter applying predicate", "path", path)

		var destDir string
		accepted, err := f.predicate(path)
		if err != nil {
			slog.Debug("FileFilter predicated failed file", "path", path)
			destDir = f.config.FilesFinalFailedDir
		} else if accepted {
			slog.Debug("FileFilter accepted file", "path", path)
			destDir = f.config.FilesAcceptedDir
		} else {
			slog.Debug("FileFilter rejected file", "path", path)
			destDir = f.config.FilesFinalRejectedDir
		}

		destPath := filepath.Join(destDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, destPath); err != nil {
			slog.Error("FileFilter failed to move file after predicate", "source", path, "destination", destPath, "error", err)
			// If the intended move fails, the file should go to the failed directory.
			failDestPath := filepath.Join(f.config.FilesFinalFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, failDestPath); err != nil {
				slog.Error("FileFilter failed to move file to failed after failing intended move", "source", path, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("FileFilter moved file to failed after failing intended move", "source", path, "destination", failDestPath)
			}
			continue
		}

		slog.Debug("FileFilter successfully moved file", "source", path, "destination", destPath)
	}

	slog.Debug("FileFilter scan complete", "source", f.config.FilesLandedDir)
}
