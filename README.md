# As software engineer you:
- write simple, concise and idiomatic code in go language.
- prefer code readability and code maintainability over sophisticated and fancy constructs
- avoid nested code blocks, i.e. nested if, nested if-else, nested for loops
- use packages for logical components
- use continue, break and early return
- generalize commonly used patterns and repeated code constructs

# Application reliability
- use slog logger for logging
- log to a file and to a console
- must only log warnings and errors
- always handle errors
- propagate errors
- never use panic
- never use any fatal function
- never use os.Exit()
- use NotifyContext for handling termination signal
- handle context cancellation
- move files using Rename() function
- assume files are moved within singled device
- externalize application configuration as file with all parameters
- fail when configuration files does not exist
- configuration is always valid
- run simultanously components periodically at different intervals
- pass configuration as value to components
- each component periodically monitors folder recursively and process file list

# Configuration file folder paths
- files/incoming
- files/landed
- files/compressed
- files/decompressed
- files/accepted
- files/completed
- files/gcs
- files/final/rejected
- files/final/failed
- files/final/uploaded
- files/final/dropped
- manifests/incoming
- manifests/landed
- manifests/uploaded
- manifests/registered
- manifests/gcs
- manifests/completed
- manifests/dropped
- manifests/failed

# Configuration intervals
- folder monitoring interval in ms
- manifests creation time threshold in ms
- manifests creation file count threshold
- maximum file age for upload in ms

# Write File Monitor component:
- starts with empty previous map of file name to file size
- monitors files recursively in folder files/incoming and creates current map of file name to file size
- compares file sizes from previous map with file sizes from current map
- file without a change in size in two consecutive listings is considered as complete
- updated modification time for complete file
- moves complete file to folder files/landed
- assigns current map variable to previous map variable

# Write File Filter component:
- monitors files recursively in folder files/landed
- calculates predicate on file content
- when predicate result is true moves file to folder files/accepted
- when predicate result is false moves file to folder files/final/rejected
- in case of error moves file to folder files/final/failed

# Write File Decompressor component:
- monitors files recursively in folder files/compressed
- decompresses zip archive file to folder files/incoming
- after successful decompression moves archive file to: files/decompressed
- in case of error moves file to folder files/final/failed


# Write File Uploader component:
- monitors files recursively in folder files/accepted
- when file is older than an hour moves file to folder files/final/dropped
- emulates copying files to GCS bucket as copying to folder files/gcs
- after successful upload moves file to folder files/final/uploaded
- in case of error moves file to folder files/final/failed

# Write Manifest Creator component:
- monitors files recursively in folder manifests/incoming
- for each manifest file in folder manifest/incoming move each file from manifest to appropriate relative folder in folder files/completed then move manifest to folder manifests/landed
- monitors files recursively in folder files/final
- creates manifest file in folder manifests/incoming after every one hour or after reaching 10000 files in all monitored folders
- manifest file name must include timestamp in UTC zone in format YYYY-MM-DD_HHMMss_SSSSSS
- manifest file must include list of file paths and file sizes

# Write Manifest Uploader component:
- monitors files recursively in folder manifestlanded
- when file is older than an hour moves manifest file to folder manifests/dropped
- creates temporary manifest in memory by filtering uploaded file paths from original manifest
- emulates copying temporary manifest to GCS bucket as copying to folder manifests/gcs
- after successful upload moves manifest file to folder manifests/uploaded
- in case of error moves manifest file to folder manifests/failed

# Write Manifest Registrar component:
- monitors files recursively in folder manifests/uploaded
- emulates registering in database name of manifest file, count of uploaded files, count of failed files, count of dropped files, count of rejected files as appending line to register file
- after successful upload moves manifest file to folder manifests/registered
- in case of error moves manifest file to folder manifests/failed

# Write Manifest Cleaner component:
- monitors manifest files recursively in folder manifests/registered
- moves manifest files to folder manifests/completed