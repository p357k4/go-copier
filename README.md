Write application to copy files. Application must:
- use go
- split logical components into packages and files
- use slog logger
- deserialize configuration record object from json file
- create all configuration directories on startup
- use timestamps in UTC zone in format YYYY-MM-DD_HHMMss_SSSSSS
- move files atomically
- log only important facts as warning or errors
- log messages to a files
- write code using idiomatic go constructs
- write code using concise and simple style
- write component functions to be run by component function runner

Component Function Runner:
- accepts component reference as parameter
- accepts interval time as parameter
- uses endless loop until interrupted
- sleeps for specified period

Incoming File Monitoring component function:
- lists files in folder and its subfolders: files/incoming
- uses immutable maps
- starts with empty previous map of file name to file size
- lists files every one minute and creates current map of file name to file size
- compares file sizes from previous map with file sizes from current map
- if file extension is zip moves to folder: files/compressed
- submits task to move each file without a change in size in two consecutive listings to folder: files/landed
- assigns current map variable to previous map variable

File Decompress component function:
- lists files in folder and its subfolders: files/compressed
- decompresses archive file to folder: files/incoming
- in case of error or exception moves file to folder: files/failed


File Filtering component function:
- lists files in folder and its subfolders: files/landed
- uses predicate to filter file
- when predicate result is true moves file to folder: files/accepted
- when predicate result is false moves file to folder: files/rejected
- in case of error or exception moves file to folder: files/failed

File Uploading component function:
- lists files in folder and its subfolders: files/accepted
- when file is older than an hour moves file to folder files/dropped
- emulates GCS file upload as copying to folder: gcs/files
- after successful upload moves file to folder: files/uploaded
- in case of error or exception moves file to folder: files/failed

Manifest Creator component function:
- lists files in folder: manifests/incoming
- for each manifest file in folder: manifest/incoming move each file from manifest to appropriate relative folder in folder: files/completed then move manifest to folder: manifests/landed
- lists files in folders and its subfolders: files/uploaded, files/rejected, files/dropped, files/failed
- creates manifest in folder: manifests/incoming after every one hour or after reaching 10000 files in all monitored folders 
- manifest must include list of file paths and file sizes

Manifest Uploading component function:
- lists files in folder and its subfolders: manifest/landed
- when file is older than an hour moves manifest file to folder: manifests/dropped
- creates temporary manifest in memory by filtering uploaded file paths from original manifest
- emulates GCS temporary manifest file upload as copying to folder: gcs/manifests
- after successful upload moves manifest file to folder: manifests/uploaded
- in case of error or exception moves manifest file to folder: manifests/failed

Manifest Registering component function:
- lists files in folder and its subfolders: manifest/uploaded
- register in database name of manifest file, count of uploaded files, count of failed files, count of dropped files, count of rejected files
- after successful upload moves manifest file to folder: manifests/registered
- in case of error or exception moves manifest file to folder: manifests/failed

Cleaning component function:
- lists manifest files in folder and its subfolders: manifests/registered
- moves manifest files to folder: manifests/completed