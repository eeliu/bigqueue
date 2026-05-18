# Diff between current branch and grandecola/bigqueue@master

This document summarizes the new features and modifications in the current branch compared to `grandecola/bigqueue@master` (effectively the `main` branch).

## 1. New Core Features

### 1.1 Garbage Collection (GC)
- **GC Mechanism**: Introduced both automatic and manual garbage collection for arena files. Users can set the number of expired arena files to retain using the `SetMaxArenasToKeep` configuration.
- **How it works**: The system calculates the minimum consumption point (`minHeadAid`) across all consumers and deletes expired arena files prior to this point to reclaim disk space.
- **Manual Trigger**: Added a `GC()` method to `MmapQueue`, allowing users to manually trigger disk cleanup during off-peak hours.

### 1.2 Tagged Messages
- **EnqueueWithTag**: Allows prepending a tag (up to 255 bytes) to a message during enqueueing.
- **DequeueWithTag**: Allows consumers to retrieve both the message payload and its associated tag without needing to parse the entire message.

### 1.3 Backlog Statistics
- **BacklogBytes**: Added `BacklogBytes()` to the `Consumer` struct. It calculates the exact number of unconsumed bytes (including length prefixes) for that consumer, which is more useful for monitoring storage pressure than simple message counts.

### 1.4 Logging Support
- **Logger Interface**: Defined a standard `Logger` interface (`Debugf`, `Infof`, `Warnf`, `Errorf`) compatible with `slog` and other popular logging libraries.
- **SetLogger Option**: Allows users to inject a custom logger via `SetLogger`, improving observability and debugging capabilities.

## 2. Core Architecture Optimizations

### 2.1 Arena Manager Refactoring (`arenamanager.go`)
- **Data Structure**: Changed `arenas` from a slice (`[]*mmap.File`) to a map (`map[int]*mmap.File`) to handle non-contiguous Arena IDs resulting from garbage collection.
- **Memory Eviction**: Enhanced the `ensureEnoughMem` logic to use LRU-style eviction while ensuring that active Head and Tail arenas remain in memory.

### 2.2 Metadata Management
- **Persistence**: Improved the metadata synchronization mechanism to ensure global head pointers are correctly persisted when updated during GC.

## 3. Testing and Engineering
- **New Test Cases**:
  - `gc_test.go` & `gc_concurrency_test.go`: Deep testing of file cleanup logic.
  - `backlog_test.go`: Validation of backlog byte calculations.
  - `tag_interop_test.go`: Verification of tag feature interoperability.
  - `crash_recovery_test.go`: Enhanced coverage for data consistency during unexpected shutdowns.
- **CI/CD**: Updated GitHub Actions and GolangCI configurations, introducing stricter linting and coverage checks.

## 4. API Summary
- `bigqueue.go`: Added `GC()`.
- `consumer.go`: Added `BacklogBytes()`, `DequeueWithTag()`.
- `config.go`: Added `SetMaxArenasToKeep(int)`, `SetLogger(Logger)`.
- `write.go`: Added `EnqueueWithTag([]byte, []byte)`.
