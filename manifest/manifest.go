// manifest/manifest.go
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-copier/config" // Replace with your actual module path
	"go-copier/util"   // Replace with your actual module path
)

// ManifestFile represents a single file entry in a manifest.
type ManifestFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Manifest represents the content of a manifest file.
type Manifest struct {
	CreatedAt time.Time      `json:"created_at"`
	Files     []ManifestFile `json:"files"`
}

// ManifestCreator creates manifest files from processed files and processes incoming manifests.
type ManifestCreator struct {
	config *config.Config
	// lastManifestTime is the time the last manifest was successfully created.
	lastManifestTime time.Time
	// Directories to monitor for manifest creation (files in final/* directories).
	creationSourceDirs []string
}

// NewManifestCreator creates a new ManifestCreator.
func NewManifestCreator(cfg *config.Config) *ManifestCreator {
	// Define source directories for manifest creation (files that have reached a final state)
	creationSourceDirs := []string{
		cfg.FilesFinalRejectedDir,
		cfg.FilesFinalFailedDir,
		cfg.FilesFinalUploadedDir,
		cfg.FilesFinalDroppedDir,
	}

	return &ManifestCreator{
		config:             cfg,
		lastManifestTime:   time.Now(), // Initialize with current time
		creationSourceDirs: creationSourceDirs,
	}
}

// Run starts the manifest creation and processing process. It runs until context is cancelled.
func (mc *ManifestCreator) Run(ctx context.Context) {
	slog.Info("ManifestCreator started", "creation_sources", mc.creationSourceDirs, "processing_source", mc.config.ManifestsIncomingDir)

	// Ticker for periodic checking (both creation triggers and processing incoming)
	ticker := time.NewTicker(mc.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ManifestCreator shutting down")
			return
		case <-ticker.C:
			mc.checkAndCreateManifest(ctx)
			mc.processIncomingManifests(ctx)
		}
	}
}

// checkAndCreateManifest checks thresholds and creates a manifest if needed.
func (mc *ManifestCreator) checkAndCreateManifest(ctx context.Context) {
	slog.Debug("ManifestCreator checking thresholds for creation")

	// Scan source directories to count files and gather info
	currentFiles := make(map[string]int64)
	totalFiles := 0
	for _, dir := range mc.creationSourceDirs {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}
		infoMap, err := util.GetFileInfoMap(ctx, dir)
		if err != nil {
			// Log the error but attempt to continue scanning other directories
			slog.Error("ManifestCreator failed to scan creation source directory", "source", dir, "error", err)
			continue // Continue to next source directory
		}
		for path, size := range infoMap {
			currentFiles[path] = size
			totalFiles++
		}
	}

	now := time.Now()
	timeSinceLastManifest := now.Sub(mc.lastManifestTime)

	if totalFiles < mc.config.ManifestCreationFileCountThreshold && timeSinceLastManifest < mc.config.GetManifestCreationTimeThreshold() {
		return
	}

	if totalFiles == 0 {
		return
	}

	// Create a manifest only if triggered and there are files to include
	manifestFiles := make([]ManifestFile, 0, len(currentFiles))
	for path, size := range currentFiles {
		manifestFiles = append(manifestFiles, ManifestFile{Path: path, Size: size})
	}

	manifest := Manifest{
		CreatedAt: now.UTC(),
		Files:     manifestFiles,
	}

	// Create manifest file name with UTC timestamp format YYYY-MM-DD_HHMMss_SSSSSS
	manifestFileName := fmt.Sprintf("%s_%s.json",
		now.UTC().Format("2006-01-02_150405"),
		now.UTC().Format("999999")) // SSSSSS for microseconds
	manifestPath := filepath.Join(mc.config.ManifestsIncomingDir, manifestFileName)

	if err := mc.writeManifest(ctx, manifest, manifestPath); err != nil {
		slog.Error("ManifestCreator failed to write manifest file", "path", manifestPath, "error", err)
		return
	}
	slog.Info("ManifestCreator successfully created manifest file", "path", manifestPath, "file_count", len(manifest.Files))
	mc.lastManifestTime = now
}

// writeManifest writes the manifest struct to a JSON file.
// It uses a temporary file and rename for basic atomicity.
func (mc *ManifestCreator) writeManifest(ctx context.Context, manifest Manifest, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(path)
	if err := util.EnsureDirExists(destDir); err != nil {
		return fmt.Errorf("failed to ensure destination directory exists for manifest %q: %w", path, err)
	}

	// Write the data to a temporary file first for atomicity
	tempPath := path + ".tmp"
	// Use 0644 permissions: owner read/write, group read, others read
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary manifest file %q: %w", tempPath, err)
	}

	// Rename the temporary file to the final path
	if err := os.Rename(tempPath, path); err != nil {
		// Attempt to clean up temp file on rename error
		os.Remove(tempPath) // Ignore error on cleanup attempt
		return fmt.Errorf("failed to rename temporary manifest file from %q to %q: %w", tempPath, path, err)
	}

	return nil
}

// processIncomingManifests scans the manifests/incoming directory
// and moves files listed within each manifest to files/completed.
func (mc *ManifestCreator) processIncomingManifests(ctx context.Context) {
	slog.Debug("ManifestCreator scanning for incoming manifests", "source", mc.config.ManifestsIncomingDir)

	manifestFiles, err := util.GetFiles(ctx, mc.config.ManifestsIncomingDir)
	if err != nil {
		slog.Error("ManifestCreator failed to scan manifests incoming directory", "source", mc.config.ManifestsIncomingDir, "error", err)
		return
	}

	for _, manifestPath := range manifestFiles {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		slog.Info("ManifestCreator processing incoming manifest", "path", manifestPath)

		// Read the manifest file
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			slog.Error("ManifestCreator failed to read incoming manifest file", "path", manifestPath, "error", err)
			// If reading fails, move the manifest to the failed directory.
			destPath := filepath.Join(mc.config.ManifestsFailedDir, util.SafeBaseName(manifestPath))
			if err := util.MoveFile(manifestPath, destPath); err != nil {
				slog.Error("ManifestCreator failed to move manifest to failed after read error", "source", manifestPath, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestCreator moved manifest to failed after read error", "source", manifestPath, "destination", destPath)
			}

			continue // Process next manifest
		}

		// Unmarshal the manifest content
		var manifest Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			slog.Error("ManifestCreator failed to unmarshal incoming manifest file", "path", manifestPath, "error", err)
			// If unmarshalling fails, move the manifest to the failed directory.
			destPath := filepath.Join(mc.config.ManifestsFailedDir, util.SafeBaseName(manifestPath))
			if err := util.MoveFile(manifestPath, destPath); err != nil {
				slog.Error("ManifestCreator failed to move manifest to failed after unmarshal error", "source", manifestPath, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestCreator moved manifest to failed after unmarshal error", "source", manifestPath, "destination", destPath)
			}
			continue // Process next manifest
		}

		// Process each file listed in the manifest
		for _, fileEntry := range manifest.Files {
			select {
			case <-ctx.Done():
				return // Stop processing on context cancellation
			default:
			}

			// The path in the manifest is the source path of the file to be moved.
			sourcePath := fileEntry.Path
			// The destination is files/completed, using the base name for flatness.
			// If preserving directory structure were needed, this would be more complex.
			destPath := filepath.Join(mc.config.FilesCompletedDir, util.SafeBaseName(sourcePath))
			if err := util.MoveFile(sourcePath, destPath); err != nil {
				slog.Error("ManifestCreator failed to move file listed in manifest to completed", "source", sourcePath, "destination", destPath, "manifest", manifestPath, "error", err)
				continue
			}

			slog.Debug("ManifestCreator successfully moved file listed in manifest", "source", sourcePath, "destination", destPath, "manifest", manifestPath)
		}

		// After processing all file entries in the manifest, move the manifest file itself.
		// This move happens regardless of whether all listed files were successfully moved.
		manifestDestPath := filepath.Join(mc.config.ManifestsLandedDir, util.SafeBaseName(manifestPath))
		if err := util.MoveFile(manifestPath, manifestDestPath); err != nil {
			slog.Error("ManifestCreator failed to move processed manifest to landed", "source", manifestPath, "destination", manifestDestPath, "error", err)
			// If the move to manifests/landed fails, move the manifest to manifests/failed.
			failDestPath := filepath.Join(mc.config.ManifestsFailedDir, util.SafeBaseName(manifestPath))
			if err := util.MoveFile(manifestPath, failDestPath); err != nil {
				slog.Error("ManifestCreator failed to move manifest to failed after failing to move to landed", "source", manifestPath, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("ManifestCreator moved manifest to failed after failing to move to landed", "source", manifestPath, "destination", failDestPath)
			}
		} else {
			slog.Info("ManifestCreator successfully moved processed manifest to landed", "source", manifestPath, "destination", manifestDestPath)
		}
	}

	slog.Debug("ManifestCreator incoming manifest processing complete")
}

// ManifestUploader uploads manifest files to a destination (emulated as a directory move).
type ManifestUploader struct {
	config *config.Config
}

// NewManifestUploader creates a new ManifestUploader.
func NewManifestUploader(cfg *config.Config) *ManifestUploader {
	return &ManifestUploader{
		config: cfg,
	}
}

// Run starts the manifest upload process. It runs until context is cancelled.
func (mu *ManifestUploader) Run(ctx context.Context) {
	slog.Info("ManifestUploader started", "source", mu.config.ManifestsLandedDir)

	ticker := time.NewTicker(mu.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ManifestUploader shutting down", "source", mu.config.ManifestsLandedDir)
			return
		case <-ticker.C:
			mu.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and uploads or drops manifests based on age.
func (mu *ManifestUploader) processFiles(ctx context.Context) {
	slog.Debug("ManifestUploader scanning", "source", mu.config.ManifestsLandedDir)

	manifestFiles, err := util.GetFiles(ctx, mu.config.ManifestsLandedDir)
	if err != nil {
		slog.Error("ManifestUploader failed to scan directory", "source", mu.config.ManifestsLandedDir, "error", err)
		return
	}

	// Reuse the file age threshold for manifests
	maxAge := mu.config.GetMaximumFileAgeForUpload()
	now := time.Now()

	for _, path := range manifestFiles {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		info, err := os.Stat(path)
		if err != nil {
			slog.Error("ManifestUploader failed to get file info for age check", "path", path, "error", err)
			// If stat fails, move the manifest to failed.
			destPath := filepath.Join(mu.config.ManifestsFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("ManifestUploader failed to move manifest to failed after stat error", "source", path, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestUploader moved manifest to failed after stat error", "source", path, "destination", destPath)
			}
			continue // Process next manifest
		}

		manifestAge := now.Sub(info.ModTime()) // Calculate age using ModTime

		if manifestAge > maxAge {
			slog.Info("ManifestUploader dropping old manifest", "path", path, "age", manifestAge, "max_age", maxAge)
			destPath := filepath.Join(mu.config.ManifestsDroppedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("ManifestUploader failed to move dropped manifest", "source", path, "destination", destPath, "error", err)
				// If move fails, try moving to failed.
				failDestPath := filepath.Join(mu.config.ManifestsFailedDir, util.SafeBaseName(path))
				if err := util.MoveFile(path, failDestPath); err != nil {
					slog.Error("ManifestUploader failed to move manifest to failed after failing to move to dropped", "source", path, "destination", failDestPath, "error", err)
				} else {
					slog.Warn("ManifestUploader moved manifest to failed after failing to move to dropped", "source", path, "destination", failDestPath)
				}
			} else {
				slog.Debug("ManifestUploader moved dropped manifest", "source", path, "destination", destPath)
			}

			continue
		}

		slog.Info("ManifestUploader uploading manifest", "path", path)

		// Emulate copying temporary manifest to GCS bucket as copying to folder manifests/gcs
		// Interpretation: Copy the original manifest file content to the GCS directory.
		// The instruction about filtering is unclear and ignored for simplicity.
		gcsDestPath := filepath.Join(mu.config.ManifestsGCSDir, util.SafeBaseName(path))
		// Using MoveFile as the emulation of copying (as per the "move files using Rename()" instruction).
		if err := util.MoveFile(path, gcsDestPath); err != nil {
			slog.Error("ManifestUploader failed to emulate GCS upload for manifest", "source", path, "destination", gcsDestPath, "error", err)
			// Upload failed, move original manifest (still at 'path' if MoveFile failed) to failed.
			failDestPath := filepath.Join(mu.config.ManifestsFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, failDestPath); err != nil {
				slog.Error("ManifestUploader failed to move manifest to failed after upload error", "source", path, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("ManifestUploader moved manifest to failed after upload error", "source", path, "destination", failDestPath)
			}

			continue
		}

		slog.Info("ManifestUploader successfully uploaded manifest (emulated)", "path", path)
		// After successful emulation (move to GCS_DIR), move the manifest from GCS_DIR to manifests/uploaded.
		uploadedDestPath := filepath.Join(mu.config.ManifestsUploadedDir, util.SafeBaseName(path)) // Use original base name
		if err := util.MoveFile(gcsDestPath, uploadedDestPath); err != nil {
			slog.Error("ManifestUploader failed to move manifest to manifests/uploaded after successful GCS emulation", "source", gcsDestPath, "destination", uploadedDestPath, "error", err)
			// This is a cleanup step. The manifest is in GCS_DIR. Move it from GCS_DIR to failed.
			failDestPath := filepath.Join(mu.config.ManifestsFailedDir, util.SafeBaseName(gcsDestPath)) // Use base name from GCS_DIR path
			if err := util.MoveFile(gcsDestPath, failDestPath); err != nil {
				slog.Error("ManifestUploader failed to move manifest to failed after failing to move from GCS_DIR to uploaded", "source", gcsDestPath, "destination", failDestPath, "error", err)
			} else {
				slog.Warn("ManifestUploader moved manifest to failed after failing to move from GCS_DIR to uploaded", "source", gcsDestPath, "destination", failDestPath)
			}

			continue
		}

		slog.Debug("ManifestUploader moved manifest to manifests/uploaded", "source", gcsDestPath, "destination", uploadedDestPath)
	}

	slog.Debug("ManifestUploader scan complete", "source", mu.config.ManifestsLandedDir)
}

// ManifestRegistrar registers manifest information.
type ManifestRegistrar struct {
	config *config.Config
	// registrationFilePath is the path to the file where registration info is appended.
	registrationFilePath string
}

// NewManifestRegistrar creates a new ManifestRegistrar.
func NewManifestRegistrar(cfg *config.Config) *ManifestRegistrar {
	// Define the registration file path. Placing it in a 'meta' or 'logs' directory
	// or making it configurable would be better. For simplicity, place it adjacent
	// to the manifests directories.
	registrationFilePath := filepath.Join(filepath.Dir(cfg.ManifestsRegisteredDir), "register.log")

	return &ManifestRegistrar{
		config:               cfg,
		registrationFilePath: registrationFilePath,
	}
}

// Run starts the manifest registration process. It runs until context is cancelled.
func (mr *ManifestRegistrar) Run(ctx context.Context) {
	slog.Info("ManifestRegistrar started", "source", mr.config.ManifestsUploadedDir, "register_file", mr.registrationFilePath)

	// Ensure the directory for the registration file exists.
	regFileDir := filepath.Dir(mr.registrationFilePath)
	if err := util.EnsureDirExists(regFileDir); err != nil {
		slog.Error("ManifestRegistrar failed to ensure registration file directory exists", "path", regFileDir, "error", err)
		// Cannot proceed without a place to write the registration log. Exit this goroutine.
		return
	}

	ticker := time.NewTicker(mr.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ManifestRegistrar shutting down", "source", mr.config.ManifestsUploadedDir)
			return
		case <-ticker.C:
			mr.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and registers manifests.
func (mr *ManifestRegistrar) processFiles(ctx context.Context) {
	slog.Debug("ManifestRegistrar scanning", "source", mr.config.ManifestsUploadedDir)

	manifestFiles, err := util.GetFiles(ctx, mr.config.ManifestsUploadedDir)
	if err != nil {
		slog.Error("ManifestRegistrar failed to scan directory", "source", mr.config.ManifestsUploadedDir, "error", err)
		return
	}

	for _, path := range manifestFiles {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		slog.Info("ManifestRegistrar processing manifest", "path", path)

		// Read the manifest content
		manifestData, err := os.ReadFile(path)
		if err != nil {
			slog.Error("ManifestRegistrar failed to read manifest for registration", "path", path, "error", err)
			// On error, move manifest to failed directory.
			destPath := filepath.Join(mr.config.ManifestsFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("ManifestRegistrar failed to move manifest to failed after read error during registration prep", "source", path, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestRegistrar moved manifest to failed after read error during registration prep", "source", path, "destination", destPath)
			}
			continue // Process next manifest
		}

		// Unmarshal the manifest
		var manifest Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			slog.Error("ManifestRegistrar failed to unmarshal manifest for registration", "path", path, "error", err)
			// On error, move manifest to failed directory.
			destPath := filepath.Join(mr.config.ManifestsFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("ManifestRegistrar failed to move manifest to failed after unmarshal error during registration prep", "source", path, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestRegistrar moved manifest to failed after unmarshal error during registration prep", "source", path, "destination", destPath)
			}
			continue // Process next manifest
		}

		// Emulate registering in database: append line to register file
		// Count files based on their source directories indicated in the manifest paths.
		registrationLine := fmt.Sprintf("%s | Manifest: %s | Total Files: %d | Uploaded: %d | Failed: %d | Dropped: %d | Rejected: %d\n",
			time.Now().UTC().Format(time.RFC3339), // Timestamp of registration
			filepath.Base(path),                   // Base name of the manifest file
			len(manifest.Files),                   // Total files listed in the manifest
			mr.countFilesInManifestByDir(manifest, mr.config.FilesFinalUploadedDir),
			mr.countFilesInManifestByDir(manifest, mr.config.FilesFinalFailedDir),
			mr.countFilesInManifestByDir(manifest, mr.config.FilesFinalDroppedDir),
			mr.countFilesInManifestByDir(manifest, mr.config.FilesFinalRejectedDir),
		)

		slog.Debug("ManifestRegistrar registering line", "line", registrationLine)

		if registerErr := mr.registerManifest(ctx, registrationLine); registerErr != nil {
			slog.Error("ManifestRegistrar failed to register manifest info", "path", path, "error", registerErr)
			// Registration failed, move original manifest to failed.
			destPath := filepath.Join(mr.config.ManifestsFailedDir, util.SafeBaseName(path))
			if err := util.MoveFile(path, destPath); err != nil {
				slog.Error("ManifestRegistrar failed to move manifest to failed after registration error", "source", path, "destination", destPath, "error", err)
			} else {
				slog.Warn("ManifestRegistrar moved manifest to failed after registration error", "source", path, "destination", destPath)
			}
			continue // Process next manifest
		}

		slog.Info("ManifestRegistrar successfully registered manifest", "path", path)
		// After successful registration, move the original manifest file to manifests/registered.
		registeredDestPath := filepath.Join(mr.config.ManifestsRegisteredDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, registeredDestPath); err != nil {
			slog.Error("ManifestRegistrar failed to move manifest to manifests/registered after successful registration", "source", path, "destination", registeredDestPath, "error", err)
			// This is a cleanup step after successful registration. Log the error.
			// The manifest stays in /uploaded and might be processed/registered again on the next scan,
			// leading to duplicate entries in the register file. A more robust system would prevent this.
		} else {
			slog.Debug("ManifestRegistrar moved manifest to manifests/registered", "source", path, "destination", registeredDestPath)
		}
	}

	slog.Debug("ManifestRegistrar scan complete", "source", mr.config.ManifestsUploadedDir)
}

// countFilesInManifestByDir counts files in the manifest whose original paths were under a given directory.
// This relies on the paths stored in the manifest correctly reflecting the file's state when the manifest was created.
func (mr *ManifestRegistrar) countFilesInManifestByDir(manifest Manifest, dir string) int {
	count := 0
	// Ensure the directory path ends with a separator for accurate prefix matching.
	// Handle both "/" and "\" separators.
	dir = strings.TrimSuffix(dir, string(os.PathSeparator)) + string(os.PathSeparator)
	dir = strings.TrimSuffix(dir, "/") + "/" // Also handle "/" explicitly

	for _, fileEntry := range manifest.Files {
		// Case-insensitive comparison might be needed depending on the OS filesystem.
		// Using strings.HasPrefix for simplicity here.
		if strings.HasPrefix(fileEntry.Path, dir) {
			count++
		}
	}
	return count
}

// registerManifest appends the line to the registration file.
// It includes context cancellation check and fsync for durability.
func (mr *ManifestRegistrar) registerManifest(ctx context.Context, line string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Open the file in append mode, create if not exists, and grant write permission to owner.
	f, err := os.OpenFile(mr.registrationFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open registration file %q: %w", mr.registrationFilePath, err)
	}
	// Defer closing the file
	defer f.Close()

	// Append the line to the file
	_, err = f.WriteString(line)
	if err != nil {
		// Log write error, but also attempt to sync before returning.
		slog.Error("failed to write line to registration file", "path", mr.registrationFilePath, "error", err)
		// Attempt to flush buffered writes to the underlying file descriptor

		if err := f.Sync(); err != nil {
			slog.Error("failed to sync registration file after write error", "path", mr.registrationFilePath, "error", err)
		}

		return fmt.Errorf("failed to write to registration file %q: %w", mr.registrationFilePath, err)
	}

	// Explicitly sync the file data and metadata to storage for durability.
	// This is important to ensure the registration is recorded even if the system crashes.
	if err := f.Sync(); err != nil {
		slog.Error("failed to sync registration file after write", "path", mr.registrationFilePath, "error", err)
		// Return the sync error
		return fmt.Errorf("failed to sync registration file %q after write: %w", mr.registrationFilePath, err)
	}

	return nil // Success
}

// ManifestCleaner moves registered manifests to the completed directory.
type ManifestCleaner struct {
	config *config.Config
}

// NewManifestCleaner creates a new ManifestCleaner.
func NewManifestCleaner(cfg *config.Config) *ManifestCleaner {
	return &ManifestCleaner{
		config: cfg,
	}
}

// Run starts the manifest cleaning process. It runs until context is cancelled.
func (mc *ManifestCleaner) Run(ctx context.Context) {
	slog.Info("ManifestCleaner started", "source", mc.config.ManifestsRegisteredDir)

	ticker := time.NewTicker(mc.config.GetMonitorInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("ManifestCleaner shutting down", "source", mc.config.ManifestsRegisteredDir)
			return
		case <-ticker.C:
			mc.processFiles(ctx)
		}
	}
}

// processFiles scans the directory and moves manifests to completed.
func (mc *ManifestCleaner) processFiles(ctx context.Context) {
	slog.Debug("ManifestCleaner scanning", "source", mc.config.ManifestsRegisteredDir)

	manifestFiles, err := util.GetFiles(ctx, mc.config.ManifestsRegisteredDir)
	if err != nil {
		slog.Error("ManifestCleaner failed to scan directory", "source", mc.config.ManifestsRegisteredDir, "error", err)
		return
	}

	for _, path := range manifestFiles {
		select {
		case <-ctx.Done():
			return // Stop processing on context cancellation
		default:
		}

		slog.Info("ManifestCleaner processing manifest", "path", path)

		// Move the manifest file to the completed directory.
		// Destination path uses the base name for flatness.
		destPath := filepath.Join(mc.config.ManifestsCompletedDir, util.SafeBaseName(path))
		if err := util.MoveFile(path, destPath); err != nil {
			slog.Error("ManifestCleaner failed to move manifest to completed", "source", path, "destination", destPath, "error", err)
		} else {
			slog.Debug("ManifestCleaner moved manifest to completed", "source", path, "destination", destPath)
		}
	}

	slog.Debug("ManifestCleaner scan complete", "source", mc.config.ManifestsRegisteredDir)
}
