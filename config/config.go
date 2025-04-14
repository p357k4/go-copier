// config/config.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config holds the application configuration parameters.
type Config struct {
	FilesIncomingDir       string `json:"files_incoming_dir"`
	FilesLandedDir         string `json:"files_landed_dir"`
	FilesCompressedDir     string `json:"files_compressed_dir"`
	FilesDecompressedDir   string `json:"files_decompressed_dir"`
	FilesAcceptedDir       string `json:"files_accepted_dir"`
	FilesCompletedDir      string `json:"files_completed_dir"`
	FilesGCSDir            string `json:"files_gcs_dir"`
	FilesFinalRejectedDir  string `json:"files_final_rejected_dir"`
	FilesFinalFailedDir    string `json:"files_final_failed_dir"`
	FilesFinalUploadedDir  string `json:"files_final_uploaded_dir"`
	FilesFinalDroppedDir   string `json:"files_final_dropped_dir"`
	ManifestsIncomingDir   string `json:"manifests_incoming_dir"`
	ManifestsLandedDir     string `json:"manifests_landed_dir"`
	ManifestsUploadedDir   string `json:"manifests_uploaded_dir"`
	ManifestsRegisteredDir string `json:"manifests_registered_dir"`
	ManifestsGCSDir        string `json:"manifests_gcs_dir"`
	ManifestsCompletedDir  string `json:"manifests_completed_dir"`
	ManifestsDroppedDir    string `json:"manifests_dropped_dir"`
	ManifestsFailedDir     string `json:"manifests_failed_dir"`

	FolderMonitorIntervalMs         int `json:"folder_monitor_interval_ms"`
	ManifestCreationTimeThresholdMs int `json:"manifest_creation_time_threshold_ms"`
	ManifestCreationFileCountThreshold int `json:"manifest_creation_file_count_threshold"`
	MaximumFileAgeForUploadMs       int `json:"maximum_file_age_for_upload_ms"`
}

// LoadConfig reads configuration from the specified file path.
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", configPath, err)
	}

	var cfg Config
	// Although the prompt says config is always valid, a real app might add validation.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// GetMonitorInterval returns the folder monitor interval as time.Duration.
func (c *Config) GetMonitorInterval() time.Duration {
	return time.Duration(c.FolderMonitorIntervalMs) * time.Millisecond
}

// GetManifestCreationTimeThreshold returns the manifest creation time threshold as time.Duration.
func (c *Config) GetManifestCreationTimeThreshold() time.Duration {
	return time.Duration(c.ManifestCreationTimeThresholdMs) * time.Millisecond
}

// GetMaximumFileAgeForUpload returns the maximum file age for upload as time.Duration.
func (c *Config) GetMaximumFileAgeForUpload() time.Duration {
	return time.Duration(c.MaximumFileAgeForUploadMs) * time.Millisecond
}