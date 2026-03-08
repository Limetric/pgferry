package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	state := newCheckpointState()
	state.recordFullTable("users", 1000)
	state.recordChunk("orders", 0, 500, 3)
	state.recordChunk("orders", 1, 300, 3)

	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded state is nil")
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	if !loaded.isTableDone("users") {
		t.Error("users should be done")
	}
	if loaded.isTableDone("orders") {
		t.Error("orders should not be done (only 2/3 chunks)")
	}
	if !loaded.isChunkCompleted("orders", 0) {
		t.Error("orders chunk 0 should be completed")
	}
	if !loaded.isChunkCompleted("orders", 1) {
		t.Error("orders chunk 1 should be completed")
	}
	if loaded.isChunkCompleted("orders", 2) {
		t.Error("orders chunk 2 should not be completed")
	}
	if loaded.Tables["users"].TotalRowsCopied != 1000 {
		t.Errorf("users TotalRowsCopied = %d, want 1000", loaded.Tables["users"].TotalRowsCopied)
	}
}

func TestCheckpointLoadNonExistent(t *testing.T) {
	state, err := loadCheckpoint("/nonexistent/path/checkpoint.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Fatal("expected nil state for non-existent file")
	}
}

func TestCheckpointDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	state := newCheckpointState()
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := deleteCheckpoint(path); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("checkpoint file should not exist after delete")
	}

	// Delete of non-existent file should not error
	if err := deleteCheckpoint(path); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestCheckpointCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for corrupt checkpoint")
	}
}

func TestCheckpointPath(t *testing.T) {
	got := checkpointPath("/home/user/migrations")
	want := "/home/user/migrations/pgferry_checkpoint.json"
	if got != want {
		t.Errorf("checkpointPath() = %q, want %q", got, want)
	}
}

func TestCheckpointRecordChunkAccumulates(t *testing.T) {
	state := newCheckpointState()
	state.recordChunk("t1", 0, 100, 3)
	state.recordChunk("t1", 1, 200, 3)
	state.recordChunk("t1", 2, 150, 3)

	tc := state.Tables["t1"]
	if tc.TotalRowsCopied != 450 {
		t.Errorf("TotalRowsCopied = %d, want 450", tc.TotalRowsCopied)
	}
	if len(tc.CompletedChunks) != 3 {
		t.Errorf("CompletedChunks count = %d, want 3", len(tc.CompletedChunks))
	}
}

func TestNewCheckpointState(t *testing.T) {
	state := newCheckpointState()
	if state.Version != 1 {
		t.Errorf("Version = %d, want 1", state.Version)
	}
	if state.Tables == nil {
		t.Error("Tables should not be nil")
	}
	if time.Since(state.StartedAt) > time.Minute {
		t.Error("StartedAt should be recent")
	}
}
