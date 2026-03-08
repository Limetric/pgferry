package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CheckpointState persists the progress of a chunked migration.
type CheckpointState struct {
	Version   int                        `json:"version"`
	StartedAt time.Time                  `json:"started_at"`
	Tables    map[string]*TableCheckpoint `json:"tables"`
}

// TableCheckpoint tracks per-table progress.
type TableCheckpoint struct {
	ChunkCount      int                  `json:"chunk_count"`
	CompletedChunks map[int]ChunkResult  `json:"completed_chunks"`
	FullTableDone   bool                 `json:"full_table_done"`
	TotalRowsCopied int64                `json:"total_rows_copied"`
}

// ChunkResult records the outcome of a single chunk copy.
type ChunkResult struct {
	CompletedAt time.Time `json:"completed_at"`
	RowsCopied  int64     `json:"rows_copied"`
}

// newCheckpointState creates a fresh checkpoint state.
func newCheckpointState() *CheckpointState {
	return &CheckpointState{
		Version:   1,
		StartedAt: time.Now(),
		Tables:    make(map[string]*TableCheckpoint),
	}
}

// loadCheckpoint reads checkpoint state from a JSON file.
// Returns nil, nil if the file does not exist.
func loadCheckpoint(path string) (*CheckpointState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var state CheckpointState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
	if state.Version != 1 {
		return nil, fmt.Errorf("unsupported checkpoint version %d (expected 1)", state.Version)
	}
	if state.Tables == nil {
		state.Tables = make(map[string]*TableCheckpoint)
	}
	for _, tc := range state.Tables {
		if tc.CompletedChunks == nil {
			tc.CompletedChunks = make(map[int]ChunkResult)
		}
	}
	return &state, nil
}

// saveCheckpoint writes checkpoint state to a JSON file atomically
// (write to temp file, then rename).
func saveCheckpoint(path string, state *CheckpointState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	return writeCheckpointFile(path, data)
}

// writeCheckpointFile writes pre-marshaled data to the checkpoint path atomically.
func writeCheckpointFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pgferry_checkpoint_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp checkpoint: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp checkpoint: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename checkpoint: %w", err)
	}
	return nil
}

// deleteCheckpoint removes the checkpoint file. No error if it doesn't exist.
func deleteCheckpoint(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	return nil
}

// checkpointPath returns the checkpoint file path for a given config directory.
func checkpointPath(configDir string) string {
	return filepath.Join(configDir, "pgferry_checkpoint.json")
}

// isChunkCompleted checks if a specific chunk has been completed in the checkpoint.
func (cs *CheckpointState) isChunkCompleted(tableName string, chunkIndex int) bool {
	tc, ok := cs.Tables[tableName]
	if !ok {
		return false
	}
	_, completed := tc.CompletedChunks[chunkIndex]
	return completed
}

// isTableDone checks if a table's full-table copy has been completed.
func (cs *CheckpointState) isTableDone(tableName string) bool {
	tc, ok := cs.Tables[tableName]
	if !ok {
		return false
	}
	return tc.FullTableDone
}

// recordChunk records a completed chunk in the checkpoint state.
func (cs *CheckpointState) recordChunk(tableName string, chunkIndex int, rowsCopied int64, chunkCount int) {
	tc, ok := cs.Tables[tableName]
	if !ok {
		tc = &TableCheckpoint{
			ChunkCount:      chunkCount,
			CompletedChunks: make(map[int]ChunkResult),
		}
		cs.Tables[tableName] = tc
	}
	tc.CompletedChunks[chunkIndex] = ChunkResult{
		CompletedAt: time.Now(),
		RowsCopied:  rowsCopied,
	}
	tc.TotalRowsCopied += rowsCopied
}

// recordFullTable records a completed full-table copy in the checkpoint state.
func (cs *CheckpointState) recordFullTable(tableName string, rowsCopied int64) {
	tc, ok := cs.Tables[tableName]
	if !ok {
		tc = &TableCheckpoint{
			CompletedChunks: make(map[int]ChunkResult),
		}
		cs.Tables[tableName] = tc
	}
	tc.FullTableDone = true
	tc.TotalRowsCopied = rowsCopied
}

// checkpointManager abstracts checkpoint persistence during data migration.
// When resume is disabled, a noop implementation avoids all filesystem overhead.
type checkpointManager interface {
	// IsTableDone reports whether a table was fully completed in a previous run.
	IsTableDone(tableName string) bool
	// IsChunkCompleted reports whether a specific chunk was completed in a previous run.
	IsChunkCompleted(tableName string, chunkIndex int) bool
	// RecordFullTable records a completed full-table copy.
	RecordFullTable(tableName string, rowsCopied int64)
	// RecordChunk records a completed chunk.
	RecordChunk(tableName string, chunkIndex int, rowsCopied int64, chunkCount int)
	// Flush forces pending state to disk. No-op when resume is disabled.
	Flush() error
	// Cleanup removes the checkpoint file after successful migration.
	Cleanup() error
}

// noopCheckpointManager is used when resume=false. All methods are no-ops
// during the hot path, avoiding any checkpoint file I/O. Cleanup still removes
// stale checkpoint files left by previous resume=true runs so they cannot be
// accidentally loaded if resume is later re-enabled.
type noopCheckpointManager struct {
	path string // checkpoint file path, used only by Cleanup
}

func (n *noopCheckpointManager) IsTableDone(string) bool             { return false }
func (n *noopCheckpointManager) IsChunkCompleted(string, int) bool   { return false }
func (n *noopCheckpointManager) RecordFullTable(string, int64)       {}
func (n *noopCheckpointManager) RecordChunk(string, int, int64, int) {}
func (n *noopCheckpointManager) Flush() error                        { return nil }
func (n *noopCheckpointManager) Cleanup() error                      { return deleteCheckpoint(n.path) }

const (
	// checkpointFlushCount is the number of completed items before a flush is triggered.
	checkpointFlushCount = 10
	// checkpointFlushInterval is the maximum time between checkpoint flushes.
	checkpointFlushInterval = 5 * time.Second
)

// persistentCheckpointManager writes checkpoint state to disk with batched
// flushing to reduce I/O in the hot path. Thread-safe for concurrent use.
type persistentCheckpointManager struct {
	mu        sync.Mutex
	state     *CheckpointState
	path      string
	dirty     bool
	unflushed int
	lastFlush time.Time
	flushing  bool // true while a file write is in progress, prevents concurrent flushes

	// Pre-computed skip sets from loaded checkpoint (read-only after init).
	skipTables map[string]bool
	skipChunks map[string]map[int]bool
}

// newPersistentCheckpointManager creates a checkpoint manager that persists
// state to disk with batched writes. If a checkpoint file exists at path,
// it is loaded and skip sets are pre-computed for fast lookups.
func newPersistentCheckpointManager(path string) (*persistentCheckpointManager, error) {
	loaded, err := loadCheckpoint(path)
	if err != nil {
		return nil, err
	}

	state := loaded
	if state == nil {
		state = newCheckpointState()
	}

	m := &persistentCheckpointManager{
		state:      state,
		path:       path,
		lastFlush:  time.Now(),
		skipTables: make(map[string]bool),
		skipChunks: make(map[string]map[int]bool),
	}

	if loaded != nil {
		log.Printf("resuming from checkpoint (started %s)", loaded.StartedAt.Format(time.RFC3339))
		for name, tc := range loaded.Tables {
			if tc.FullTableDone {
				m.skipTables[name] = true
			}
			if len(tc.CompletedChunks) > 0 {
				s := make(map[int]bool, len(tc.CompletedChunks))
				for idx := range tc.CompletedChunks {
					s[idx] = true
				}
				m.skipChunks[name] = s
			}
		}
	}

	return m, nil
}

func (m *persistentCheckpointManager) IsTableDone(tableName string) bool {
	return m.skipTables[tableName]
}

func (m *persistentCheckpointManager) IsChunkCompleted(tableName string, chunkIndex int) bool {
	if s, ok := m.skipChunks[tableName]; ok {
		return s[chunkIndex]
	}
	return false
}

func (m *persistentCheckpointManager) RecordFullTable(tableName string, rowsCopied int64) {
	m.mu.Lock()
	m.state.recordFullTable(tableName, rowsCopied)
	m.dirty = true
	m.unflushed++
	shouldFlush := m.shouldFlush()
	m.mu.Unlock()

	if shouldFlush {
		if err := m.Flush(); err != nil {
			log.Printf("WARN: failed to save checkpoint: %v", err)
		}
	}
}

func (m *persistentCheckpointManager) RecordChunk(tableName string, chunkIndex int, rowsCopied int64, chunkCount int) {
	m.mu.Lock()
	m.state.recordChunk(tableName, chunkIndex, rowsCopied, chunkCount)
	m.dirty = true
	m.unflushed++
	shouldFlush := m.shouldFlush()
	m.mu.Unlock()

	if shouldFlush {
		if err := m.Flush(); err != nil {
			log.Printf("WARN: failed to save checkpoint: %v", err)
		}
	}
}

// shouldFlush returns true if a flush is warranted. Must be called with mu held.
func (m *persistentCheckpointManager) shouldFlush() bool {
	return m.unflushed >= checkpointFlushCount || time.Since(m.lastFlush) >= checkpointFlushInterval
}

// Flush writes pending checkpoint state to disk. Only one flush runs at a time
// (guarded by m.flushing) to prevent concurrent writes from racing on file
// rename. The unflushed counter is decremented by the snapshot count rather
// than zeroed, so records added during the write are preserved for the next
// flush cycle. Counters are reset only after a successful write.
func (m *persistentCheckpointManager) Flush() error {
	m.mu.Lock()
	if !m.dirty || m.flushing {
		m.mu.Unlock()
		return nil
	}
	m.flushing = true
	flushedCount := m.unflushed
	data, err := json.Marshal(m.state)
	m.mu.Unlock()

	if err != nil {
		m.mu.Lock()
		m.flushing = false
		m.mu.Unlock()
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	writeErr := writeCheckpointFile(m.path, data)

	m.mu.Lock()
	m.flushing = false
	if writeErr == nil {
		m.unflushed -= flushedCount
		m.dirty = m.unflushed > 0
		m.lastFlush = time.Now()
	}
	m.mu.Unlock()
	return writeErr
}

func (m *persistentCheckpointManager) Cleanup() error {
	return deleteCheckpoint(m.path)
}
