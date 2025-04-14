// decompressor/decompressor.go
package decompressor

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-copier/config" // Replace with your actual module path
	"go-copier/util"   // Replace with your actual module path
)

// FileDecompressor decompresses zip archives.
type FileDecompressor struct {
	config *config.Config
}

// NewFileDecompressor creates a new FileDecompressor.
func NewFileDecompressor(cfg *config.Config) *FileDecompressor {
	return &FileDecompressor{
		config: cfg,
	}
}

// Run starts the decompression process. It runs until the context is cancelled.
func (d *FileDecompressor) Run(ctx context.Context) {
	slog.Info("FileDecompressor started", "source", d.config.FilesCompressedDir)

	ticker := time.NewTicker(d.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("FileDecompressor shutting down", "source", d.config.FilesCompressedDir)
			return
		case <-ticker.C:
			d.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and decompresses zip files.
func (d *FileDecompressor) processFiles(ctx context.Context) {
	slog.Debug("FileDecompressor scanning", "source", d.config.FilesCompressedDir)

	files, err := util.GetFiles(ctx, d.config.FilesCompressedDir)
	if err != nil {
		slog.Error("FileDecompressor failed to scan directory", "source", d.config.FilesCompressedDir, "error", err)
		return
	}

	for _, path := range files {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		// Only process zip files (basic suffix check)
		if !strings.HasSuffix(strings.ToLower(path), ".zip") {
			slog.Debug("FileDecompressor skipping non-zip file", "path", path)
			continue
		}

		slog.Info("FileDecompressor processing zip file", "path", path)

		// Decompress the file
		decompressedFiles, decompressErr := d.decompressZip(ctx, path, d.config.FilesIncomingDir)
		if decompressErr != nil {
			slog.Error("FileDecompressor failed to decompress file", "path", path, "error", decompressErr)
			// On error, move original zip to failed directory.
			destPath := filepath.Join(d.config.FilesFinalFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("FileDecompressor failed to move zip file to failed after decompression error", "source", path, "destination", destPath, "error", err)
			} else {
				slog.Warn("FileDecompressor moved zip file to failed after decompression error", "source", path, "destination", destPath)
			}
			// Note: Partially decompressed files might be left in files/incoming. Cleanup might be needed.
			continue
		}

		slog.Info("FileDecompressor successfully decompressed file", "path", path, "extracted_count", len(decompressedFiles))

		// After successful decompression, move the original archive to files/decompressed.
		destPath := filepath.Join(d.config.FilesDecompressedDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, destPath); err != nil {
			slog.Error("FileDecompressor failed to move zip file after successful decompression", "source", path, "destination", destPath, "error", err)
		} else {
			slog.Debug("FileDecompressor moved zip file after successful decompression", "source", path, "destination", destPath)
		}
	}

	slog.Debug("FileDecompressor scan complete", "source", d.config.FilesCompressedDir)
}

// decompressZip decompresses a zip archive to a destination directory.
// It returns the paths of the extracted files.
func (d *FileDecompressor) decompressZip(ctx context.Context, zipFilePath, destDir string) ([]string, error) {
	extractedFiles := []string{}

	archive, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip archive %q: %w", zipFilePath, err)
	}
	// Defer closing the zip archive reader
	defer archive.Close()

	// Ensure destination directory exists
	if err := util.EnsureDirExists(destDir); err != nil {
		return nil, fmt.Errorf("failed to ensure destination directory %q exists: %w", destDir, err)
	}

	for _, f := range archive.File {
		// Check context cancellation inside the file loop
		select {
		case <-ctx.Done():
			// Return cancellation error. extractedFiles contains files processed so far.
			return extractedFiles, ctx.Err()
		default:
		}

		// Construct the full path for the extracted file/directory
		filePath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip (directory traversal) attacks
		// Ensure the extracted path is safely inside the destination directory.
		if !strings.HasPrefix(filePath, destDir+string(os.PathSeparator)) {
			slog.Warn("zip entry attempted path traversal, skipping", "zip", zipFilePath, "entry", f.Name)
			continue // Skip potentially malicious entry
		}

		// If it's a directory entry, create the directory
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(filePath, f.Mode()); err != nil {
				slog.Error("failed to create directory from zip entry", "zip", zipFilePath, "entry", f.Name, "error", err)
			}
			continue // Move to the next entry in the zip
		}

		// If it's a file entry, extract it
		err := func() error {
			rc, err := f.Open()
			if err != nil {
				// Log the error for this specific file entry but continue processing other entries
				return fmt.Errorf("failed to open zip %s entry file %s: %w", zipFilePath, f.Name, err)
			}
			// Defer closing the file reader for the current entry
			defer rc.Close() // This defer is inside the loop, closing rc for each iteration.

			// Ensure parent directory exists for the file
			if err := util.EnsureDirExists(filepath.Dir(filePath)); err != nil {
				return fmt.Errorf("failed to ensure parent directory for zip entry %q: %w", f.Name, err)
			}

			// Create the output file
			outFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return fmt.Errorf("failed to create output file for zip entry %q: %w", f.Name, err)
			}
			// Defer closing the output file
			defer outFile.Close() // This defer is inside the loop, closing outFile for each iteration.

			// Copy the content from the zip entry to the output file
			if _, err := io.Copy(outFile, rc); err != nil {
				return fmt.Errorf("failed to copy content for zip entry %q: %w", f.Name, err)
			}

			return nil
		}()
		if err != nil {
			slog.Warn("failed to decompress", "path", filePath, "file", f.Name, "error", err)
		}

		extractedFiles = append(extractedFiles, filePath)
		slog.Debug("FileDecompressor extracted file", "path", filePath)
	}

	return extractedFiles, nil
}
