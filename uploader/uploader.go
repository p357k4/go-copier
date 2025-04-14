// uploader/uploader.go
package uploader

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go-copier/config" // Replace with your actual module path
	"go-copier/util"   // Replace with your actual module path
)

// FileUploader uploads files to a destination (emulated as a directory move).
type FileUploader struct {
	config *config.Config
}

// NewFileUploader creates a new FileUploader.
func NewFileUploader(cfg *config.Config) *FileUploader {
	return &FileUploader{
		config: cfg,
	}
}

// Run starts the upload process. It runs until the context is cancelled.
func (u *FileUploader) Run(ctx context.Context) {
	slog.Info("FileUploader started", "source", u.config.FilesAcceptedDir)

	ticker := time.NewTicker(u.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("FileUploader shutting down", "source", u.config.FilesAcceptedDir)
			return
		case <-ticker.C:
			u.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and uploads or drops files based on age.
func (u *FileUploader) processFiles(ctx context.Context) {
	slog.Debug("FileUploader scanning", "source", u.config.FilesAcceptedDir)

	files, err := util.GetFiles(ctx, u.config.FilesAcceptedDir)
	if err != nil {
		slog.Error("FileUploader failed to scan directory", "source", u.config.FilesAcceptedDir, "error", err)
		return
	}

	maxAge := u.config.GetMaximumFileAgeForUpload()
	now := time.Now()

	for _, path := range files {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		info, err := os.Stat(path)
		if err != nil {
			slog.Error("FileUploader failed to get file info for age check", "path", path, "error", err)
			// If we can't stat the file, we can't check its age.
			// It's safer to move it to failed than potentially process it incorrectly or leave it stuck.
			failDestPath := filepath.Join(u.config.FilesFinalFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, failDestPath); err != nil {
				slog.Error("FileUploader failed to move file to failed after stat error", "source", path, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("FileUploader moved file to failed after stat error", "source", path, "destination", failDestPath)
			}
			continue // Process next file
		}

		fileAge := now.Sub(info.ModTime())

		if fileAge > maxAge {
			slog.Info("FileUploader dropping old file", "path", path, "age", fileAge, "max_age", maxAge)
			destPath := filepath.Join(u.config.FilesFinalDroppedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("FileUploader failed to move dropped file", "source", path, "destination", destPath, "error", err)
				// If move fails, try moving to failed as per the rule.
				failDestPath := filepath.Join(u.config.FilesFinalFailedDir, util.SafeBaseName(path))
				if err := util.MoveFile(path, failDestPath); err != nil {
					slog.Error("FileUploader failed to move file to failed after failing to move to dropped", "source", path, "destination", failDestPath, "error", err)
				} else {
					slog.Warn("FileUploader moved file to failed after failing to move to dropped", "source", path, "destination", failDestPath)
				}
			} else {
				slog.Debug("FileUploader moved dropped file", "source", path, "destination", destPath)
			}
			continue
		}

		slog.Debug("FileUploader uploading file", "path", path)
		// Emulate GCS upload by moving to files/gcs
		// The file is moved out of the 'accepted' directory here.
		gcsDestPath := filepath.Join(u.config.FilesGCSDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, gcsDestPath); err != nil {
			slog.Error("FileUploader failed to emulate GCS upload", "source", path, "destination", gcsDestPath, "error", err)

			// Upload failed, move the original file (still at 'path' if MoveFile failed) to failed.
			failDestPath := filepath.Join(u.config.FilesFinalFailedDir, util.SafeBaseName(path))
			if moveErr := util.MoveFile(path, failDestPath); moveErr != nil {
				slog.Error("FileUploader failed to move file to failed after upload error", "source", path, "destination", failDestPath, "error", moveErr)
			} else {
				slog.Warn("FileUploader moved file to failed after upload error", "source", path, "destination", failDestPath)
			}
			continue
		}

		slog.Info("FileUploader successfully uploaded file (emulated)", "path", path)
		// After successful emulation (move to GCS_DIR), move the file from GCS_DIR to final/uploaded.
		uploadedDestPath := filepath.Join(u.config.FilesFinalUploadedDir, util.SafeBaseName(path)) // Use original base name
		if err := util.MoveFile(gcsDestPath, uploadedDestPath); err != nil {
			slog.Error("FileUploader failed to move file to final/uploaded after successful GCS emulation", "source", gcsDestPath, "destination", uploadedDestPath, "error", err)
			// This is a cleanup step after successful upload emulation.
			// The file is in GCS_DIR. If moving it to final/uploaded fails, move it from GCS_DIR to failed.
			failDestPath := filepath.Join(u.config.FilesFinalFailedDir, util.SafeBaseName(gcsDestPath)) // Use base name from GCS_DIR path
			if err := util.MoveFile(gcsDestPath, failDestPath); err != nil {
				slog.Error("FileUploader failed to move file to failed after failing to move from GCS_DIR to uploaded", "source", gcsDestPath, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("FileUploader moved file to failed after failing to move from GCS_DIR to uploaded", "source", gcsDestPath, "destination", failDestPath)
			}
			continue
		}

		slog.Debug("FileUploader moved file to final/uploaded", "source", gcsDestPath, "destination", uploadedDestPath)
	}

	slog.Debug("FileUploader scan complete", "source", u.config.FilesAcceptedDir)
}
