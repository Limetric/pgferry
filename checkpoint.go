package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

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
