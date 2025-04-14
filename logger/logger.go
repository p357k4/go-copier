// logger/logger.go
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// SetupLogger initializes the slog logger to write to both console and a file.
// It is configured to only log warnings and errors.
func SetupLogger(logFilePath string) (*slog.Logger, error) {
	// Use os.OpenFile with O_CREATE and O_APPEND
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", logFilePath, err)
	}

	// We want to log Warnings and Errors and above
	level := slog.LevelInfo

	// Create a MultiWriter to write to both console and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	handlerOptions := &slog.HandlerOptions{
		Level: level,
		// AddSource: true, // Optional: add source code file and line
	}

	// Use TextHandler for console/file output
	handler := slog.NewTextHandler(multiWriter, handlerOptions)

	logger := slog.New(handler)

	// Set the default logger
	slog.SetDefault(logger)

	return logger, nil
}