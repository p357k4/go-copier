// main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"go-copier/config"       // Replace with your actual module path
	"go-copier/decompressor" // Replace with your actual module path
	"go-copier/filter"       // Replace with your actual module path
	"go-copier/logger"       // Replace with your actual module path
	"go-copier/manifest"     // Replace with your actual module path
	"go-copier/monitor"      // Replace with your actual module path
	"go-copier/uploader"     // Replace with your actual module path
	"go-copier/util"         // Replace with your actual module path
)

const configFilePath = "config.json"       // Default config file name
const logFilePath = "logs/application.log" // Default log file name

func main() {
	// --- Setup Logging ---
	// Ensure log directory exists if logFilePath includes dirs
	logDir := filepath.Dir(logFilePath)
	if err := util.EnsureDirExists(logDir); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to create log directory %q: %v\n", logDir, err)
		os.Exit(1) // Use os.Exit only in main for unrecoverable setup errors
	}

	_, err := logger.SetupLogger(logFilePath)
	if err != nil {
		slog.Error("FATAL: Failed to set up logger", "error", err)
		os.Exit(1) // Use os.Exit only in main for unrecoverable setup errors
	}
	slog.Info("Logger set up successfully")

	// --- Load Configuration ---
	cfg, err := config.LoadConfig(configFilePath)
	if err != nil {
		slog.Error("FATAL: Failed to load configuration", "file", configFilePath, "error", err)
		os.Exit(1) // Use os.Exit only in main for unrecoverable setup errors
	}
	slog.Info("Configuration loaded successfully", "file", configFilePath)

	// --- Ensure Directories Exist ---
	dirsToEnsure := []string{
		cfg.FilesIncomingDir,
		cfg.FilesLandedDir,
		cfg.FilesCompressedDir,
		cfg.FilesDecompressedDir,
		cfg.FilesAcceptedDir,
		cfg.FilesCompletedDir,
		cfg.FilesGCSDir,
		cfg.FilesFinalRejectedDir,
		cfg.FilesFinalFailedDir,
		cfg.FilesFinalUploadedDir,
		cfg.FilesFinalDroppedDir,
		cfg.ManifestsIncomingDir,
		cfg.ManifestsLandedDir,
		cfg.ManifestsUploadedDir,
		cfg.ManifestsRegisteredDir,
		cfg.ManifestsGCSDir,
		cfg.ManifestsCompletedDir,
		cfg.ManifestsDroppedDir,
		cfg.ManifestsFailedDir,
		filepath.Dir(logFilePath), // Ensure log dir is created
	}
	for _, dir := range dirsToEnsure {
		if err := util.EnsureDirExists(dir); err != nil {
			slog.Error("FATAL: Failed to create essential directory", "path", dir, "error", err)
			os.Exit(1) // Use os.Exit only in main for unrecoverable setup errors
		}
	}
	slog.Info("All required directories ensured to exist")

	// --- Set up Signal Handling ---
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("Signal handling set up")

	// --- Start Components ---
	var wg sync.WaitGroup

	// File Pipeline Components
	fileMonitor := monitor.NewFileMonitor(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		fileMonitor.Run(ctx)
	}()
	slog.Info("FileMonitor started")

	fileFilter := filter.NewFileFilter(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		fileFilter.Run(ctx)
	}()
	slog.Info("FileFilter started")

	fileDecompressor := decompressor.NewFileDecompressor(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		fileDecompressor.Run(ctx)
	}()
	slog.Info("FileDecompressor started")

	fileUploader := uploader.NewFileUploader(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		fileUploader.Run(ctx)
	}()
	slog.Info("FileUploader started")

	// Manifest Pipeline Components
	manifestCreator := manifest.NewManifestCreator(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		manifestCreator.Run(ctx)
	}()
	slog.Info("ManifestCreator started")

	manifestUploader := manifest.NewManifestUploader(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		manifestUploader.Run(ctx)
	}()
	slog.Info("ManifestUploader started")

	manifestRegistrar := manifest.NewManifestRegistrar(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		manifestRegistrar.Run(ctx)
	}()
	slog.Info("ManifestRegistrar started")

	manifestCleaner := manifest.NewManifestCleaner(cfg)
	wg.Add(1)
	go func() {
		defer wg.Done()
		manifestCleaner.Run(ctx)
	}()
	slog.Info("ManifestCleaner started")

	slog.Info("All components started. Waiting for termination signal.")

	// --- Wait for Termination ---
	<-ctx.Done()

	slog.Info("Termination signal received. Shutting down components...")

	// The context cancellation handles the goroutines stopping themselves.
	// Wait for all goroutines to finish.
	wg.Wait()

	slog.Info("All components shut down gracefully. Exiting.")
}
