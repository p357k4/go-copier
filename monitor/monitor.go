// monitor/monitor.go
package monitor

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"go-copier/config" // Replace with your actual module path
	"go-copier/util"   // Replace with your actual module path
)

// FileMonitor monitors an incoming directory for stable files.
type FileMonitor struct {
	config *config.Config
	// previousFiles maps file path to size from the last scan.
	previousFiles map[string]int64
}

// NewFileMonitor creates a new FileMonitor.
func NewFileMonitor(cfg *config.Config) *FileMonitor {
	return &FileMonitor{
		config:        cfg,
		previousFiles: make(map[string]int64), // Start with empty map
	}
}

// Run starts the monitoring process. It runs until the context is cancelled.
func (m *FileMonitor) Run(ctx context.Context) {
	slog.Info("FileMonitor started", "source", m.config.FilesIncomingDir)

	ticker := time.NewTicker(m.config.GetMonitorInterval())
	defer ticker.Stop()

	// Perform an initial scan immediately
	// Errors during the initial scan are logged and the monitor continues to wait for the first tick.
	m.processFiles(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("FileMonitor shutting down", "source", m.config.FilesIncomingDir)
			return
		case <-ticker.C:
			m.processFiles(ctx)
		}
	}
}

// processFiles scans the directory, compares with the previous scan, and moves stable files.
func (m *FileMonitor) processFiles(ctx context.Context) {
	slog.Debug("FileMonitor scanning", "source", m.config.FilesIncomingDir)

	currentFiles, err := util.GetFileInfoMap(ctx, m.config.FilesIncomingDir)
	if err != nil {
		slog.Error("FileMonitor failed to scan directory", "source", m.config.FilesIncomingDir, "error", err)
		return
	}

	movedCount := 0
	for path, size := range currentFiles {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		prevSize, existsInPrevious := m.previousFiles[path]

		// A file is considered complete/stable if it existed in the previous scan
		// AND its size has not changed between the two consecutive scans.
		if !existsInPrevious || prevSize != size {
			continue
		}

		// Destination path uses the base name in the landed directory.
		// If preserving subdirectories was required, this would need adjustment.
		destPath := filepath.Join(m.config.FilesLandedDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, destPath); err != nil {
			slog.Error("FileMonitor failed to move stable file", "source", path, "destination", destPath, "error", err)
			continue
		}
		
		slog.Debug("FileMonitor successfully moved stable file", "source", path, "destination", destPath)
		movedCount++
	}

	m.previousFiles = currentFiles

	slog.Debug("FileMonitor scan complete", "source", m.config.FilesIncomingDir, "moved_count", movedCount)
}
