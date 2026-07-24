# Changelog

All notable changes to this project will be documented in this file.

## [v0.2.3] - 2026-07-24
- handle Flush syscall failure

## [v0.2.2] - 2026-05-12

### Changed
- Refactored `Logger` interface to use formatted logging methods (`Debugf`, `Infof`, `Warnf`, `Errorf`).
- Updated internal logging to use the new formatted methods.

### Added
- Added Codecov configuration for coverage tracking.

## [v0.2.1] - 2026-05-11

### Added
- Added structured logging support using `log/slog` (Standard Library).
- Added `SetLogger` Option to configure a custom `slog.Logger`.
- Implemented logging wrappers (`Debug`, `Info`, `Warn`, `Error`) in `bqConfig`.
- Added detailed internal logging to `arenaManager` for better observability of initialization, garbage collection, and memory management.

## [v0.2.0] - 2026-05-08

### Added
- Added `GC()` method to `MmapQueue` to manually trigger arena file cleanup.
- Added background GC support in `arenaManager` to delete old arena files based on consumer progress.
- Added `version.go` to manage project versioning.
- Added `CHANGELOG.md` to track project history.

### Changed
- Refactored `arenaManager` to use `map[int]*mmap.File` instead of a slice for better management of non-contiguous arena files after deletion.
- Simplified `setArenaPath` logic in `arenaManager` to use `fmt.Sprintf` and `path.Join`.
- Updated global `head` metadata in GC logic to ensure new consumers start from the earliest available data.

### Fixed
- Fixed a potential infinite hang in concurrency tests by adding consumer deadlines and proper synchronization.
- Fixed metadata consistency issues where global head was not correctly tracking the slowest consumer.

## [v0.1.0] - Previous
- Initial release with basic `mmap` persistent queue functionality.
- Support for multiple consumers via `Consumer` API.
- Support for tagged messages.
