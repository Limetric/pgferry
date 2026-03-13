package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCheckpointCompatibility() checkpointCompatibility {
	return checkpointCompatibility{
		Fingerprint: "compat-fingerprint",
		Summary: checkpointCompatibilitySummary{
			SourceType:         "mysql",
			SourceDBName:       "appdb",
			TargetSchema:       "public",
			SourceSnapshotMode: "none",
			ChunkSize:          100000,
			TypeMapping:        defaultTypeMappingConfig(),
			Tables: []checkpointCompatibilityTable{
				{SourceName: "users", PGName: "users", TableHash: "users-hash"},
			},
		},
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
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
	if loaded.Version != checkpointVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, checkpointVersion)
	}
	if loaded.Compatibility.Fingerprint != compat.Fingerprint {
		t.Errorf("Compatibility.Fingerprint = %q, want %q", loaded.Compatibility.Fingerprint, compat.Fingerprint)
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

func TestCheckpointUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte(`{"version":99,"started_at":"2026-01-01T00:00:00Z","tables":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for unsupported checkpoint version")
	}
	if !strings.Contains(err.Error(), "unsupported checkpoint version") {
		t.Errorf("error should mention unsupported version, got: %v", err)
	}
}

func TestNewCheckpointState(t *testing.T) {
	state := newCheckpointState()
	if state.Version != checkpointVersion {
		t.Errorf("Version = %d, want %d", state.Version, checkpointVersion)
	}
	if state.Tables == nil {
		t.Error("Tables should not be nil")
	}
	if time.Since(state.StartedAt) > time.Minute {
		t.Error("StartedAt should be recent")
	}
}

func TestNoopCheckpointManager(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	mgr := &noopCheckpointManager{path: path}

	if mgr.IsTableDone("anything") {
		t.Error("noop should never report table done")
	}
	if mgr.IsChunkCompleted("anything", 0) {
		t.Error("noop should never report chunk completed")
	}

	// Record calls should not panic
	mgr.RecordFullTable("t1", 1000)
	mgr.RecordChunk("t1", 0, 500, 3)

	if err := mgr.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := mgr.Cleanup(); err != nil {
		t.Errorf("Cleanup: %v", err)
	}
}

func TestNoopCheckpointManager_CleansUpStaleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	// Simulate a stale checkpoint from a previous resume=true run
	state := newCheckpointState()
	state.recordFullTable("users", 1000)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save stale checkpoint: %v", err)
	}

	mgr := &noopCheckpointManager{path: path}

	// Cleanup should remove the stale file
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale checkpoint file should be removed after noop cleanup")
	}

	// Cleanup on already-removed file should not error
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestPersistentCheckpointManager_FreshStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	mgr, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if mgr.IsTableDone("t1") {
		t.Error("fresh manager should not report table done")
	}
	if mgr.IsChunkCompleted("t1", 0) {
		t.Error("fresh manager should not report chunk completed")
	}
	if mgr.state.Compatibility.Fingerprint != compat.Fingerprint {
		t.Errorf("Compatibility.Fingerprint = %q, want %q", mgr.state.Compatibility.Fingerprint, compat.Fingerprint)
	}
}

func TestPersistentCheckpointManager_SkipSets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	// Create a checkpoint with some completed work
	state := newCheckpointState()
	state.recordFullTable("users", 1000)
	state.recordChunk("orders", 0, 500, 3)
	state.recordChunk("orders", 1, 300, 3)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if !mgr.IsTableDone("users") {
		t.Error("users should be done")
	}
	if mgr.IsTableDone("orders") {
		t.Error("orders should not be done")
	}
	if !mgr.IsChunkCompleted("orders", 0) {
		t.Error("orders chunk 0 should be completed")
	}
	if !mgr.IsChunkCompleted("orders", 1) {
		t.Error("orders chunk 1 should be completed")
	}
	if mgr.IsChunkCompleted("orders", 2) {
		t.Error("orders chunk 2 should not be completed")
	}
	if mgr.IsChunkCompleted("nonexistent", 0) {
		t.Error("nonexistent table should not have completed chunks")
	}
}

func TestPersistentCheckpointManager_BatchedFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Record fewer items than the flush threshold — file should not exist yet
	for i := 0; i < checkpointFlushCount-1; i++ {
		mgr.RecordChunk("t1", i, 100, checkpointFlushCount+5)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("checkpoint file should not exist before flush threshold")
	}

	// One more record should trigger the flush
	mgr.RecordChunk("t1", checkpointFlushCount-1, 100, checkpointFlushCount+5)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("checkpoint file should exist after flush threshold")
	}

	// Verify the file is loadable and has correct state
	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tc := loaded.Tables["t1"]
	if tc == nil {
		t.Fatal("table t1 not in checkpoint")
	}
	if len(tc.CompletedChunks) != checkpointFlushCount {
		t.Errorf("CompletedChunks = %d, want %d", len(tc.CompletedChunks), checkpointFlushCount)
	}
}

func TestPersistentCheckpointManager_ExplicitFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mgr.RecordFullTable("t1", 500)

	// File should not exist yet (below flush threshold)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not exist before explicit flush")
	}

	// Explicit flush should write the file
	if err := mgr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.isTableDone("t1") {
		t.Error("t1 should be done after flush")
	}

	// Second flush with no new data should be a no-op (no error)
	if err := mgr.Flush(); err != nil {
		t.Fatalf("second flush: %v", err)
	}
}

func TestPersistentCheckpointManager_Cleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mgr.RecordFullTable("t1", 500)
	if err := mgr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("checkpoint file should not exist after cleanup")
	}
}

func TestPersistentCheckpointManager_ConcurrentRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	const numWorkers = 8
	const chunksPerWorker = 20
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			tableName := "t1"
			for i := 0; i < chunksPerWorker; i++ {
				idx := workerID*chunksPerWorker + i
				mgr.RecordChunk(tableName, idx, 100, numWorkers*chunksPerWorker)
			}
		}(w)
	}

	wg.Wait()
	if err := mgr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	tc := loaded.Tables["t1"]
	if tc == nil {
		t.Fatal("table t1 not in checkpoint")
	}
	if len(tc.CompletedChunks) != numWorkers*chunksPerWorker {
		t.Errorf("CompletedChunks = %d, want %d", len(tc.CompletedChunks), numWorkers*chunksPerWorker)
	}
}

func TestPersistentCheckpointManager_TimeBasedFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Force lastFlush into the past to trigger time-based flush
	mgr.mu.Lock()
	mgr.lastFlush = time.Now().Add(-checkpointFlushInterval - time.Second)
	mgr.mu.Unlock()

	// Even a single record should trigger flush due to elapsed time
	mgr.RecordChunk("t1", 0, 100, 5)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("checkpoint file should exist after time-based flush")
	}
}

func TestPersistentCheckpointManager_FlushPreservesProgressOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Simulate partial migration: some chunks succeed, then an error occurs.
	mgr.RecordChunk("orders", 0, 500, 5)
	mgr.RecordChunk("orders", 1, 500, 5)
	mgr.RecordFullTable("users", 1000)

	// Explicit flush (as the error path in migrateData would do)
	if err := mgr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Verify the checkpoint persists partial progress
	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.isTableDone("users") {
		t.Error("users should be done in checkpoint")
	}
	if !loaded.isChunkCompleted("orders", 0) {
		t.Error("orders chunk 0 should be in checkpoint")
	}
	if !loaded.isChunkCompleted("orders", 1) {
		t.Error("orders chunk 1 should be in checkpoint")
	}
	if loaded.isChunkCompleted("orders", 2) {
		t.Error("orders chunk 2 should NOT be in checkpoint (not yet reached)")
	}

	// A new manager loading this checkpoint should skip the completed work
	mgr2, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager 2: %v", err)
	}
	if !mgr2.IsTableDone("users") {
		t.Error("resumed manager should skip users")
	}
	if !mgr2.IsChunkCompleted("orders", 0) {
		t.Error("resumed manager should skip orders chunk 0")
	}
	if !mgr2.IsChunkCompleted("orders", 1) {
		t.Error("resumed manager should skip orders chunk 1")
	}
	if mgr2.IsChunkCompleted("orders", 2) {
		t.Error("resumed manager should not skip orders chunk 2")
	}
}

func TestPersistentCheckpointManager_DirtyRetainedOnWriteFailure(t *testing.T) {
	// Use a path where the directory does not exist so writeCheckpointFile fails.
	path := filepath.Join(t.TempDir(), "nonexistent_subdir", "checkpoint.json")

	mgr, err := newPersistentCheckpointManager(path, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mgr.RecordChunk("t1", 0, 100, 5)

	// Flush should fail because the directory doesn't exist
	if err := mgr.Flush(); err == nil {
		t.Fatal("expected flush to fail with nonexistent directory")
	}

	// dirty should still be true so the next flush retries
	mgr.mu.Lock()
	stillDirty := mgr.dirty
	mgr.mu.Unlock()
	if !stillDirty {
		t.Error("dirty flag should remain true after failed write")
	}
}

func TestPersistentCheckpointManager_RejectsIncompatibleChunkSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	state.recordChunk("users", 0, 100, 2)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatible := compat
	incompatible.Summary.ChunkSize = 50000
	incompatible.Fingerprint = "new-fingerprint"

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "chunk_size changed") {
		t.Fatalf("expected chunk_size mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsChangedHookContent(t *testing.T) {
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "before_data.sql")
	if err := os.WriteFile(hookPath, []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	cfg := defaultMigrationConfig()
	cfg.Schema = "app"
	cfg.Source = SourceConfig{Type: "mysql"}
	cfg.Hooks.BeforeData = []string{"before_data.sql"}
	cfg.configDir = dir

	schema := &Schema{Tables: []Table{{SourceName: "users", PGName: "users"}}}
	src := &mysqlSourceDB{}

	compat, err := buildCheckpointCompatibility(&cfg, schema, src, "appdb")
	if err != nil {
		t.Fatalf("build compatibility: %v", err)
	}

	path := filepath.Join(dir, "checkpoint.json")
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := os.WriteFile(hookPath, []byte("SELECT 2;"), 0644); err != nil {
		t.Fatalf("rewrite hook: %v", err)
	}

	changedCompat, err := buildCheckpointCompatibility(&cfg, schema, src, "appdb")
	if err != nil {
		t.Fatalf("build changed compatibility: %v", err)
	}

	_, err = newPersistentCheckpointManager(path, &changedCompat)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "before_data hook changed") {
		t.Fatalf("expected hook mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsLegacyCheckpointForSafeResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte(`{"version":1,"started_at":"2026-01-01T00:00:00Z","tables":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	compat := testCheckpointCompatibility()
	_, err := newPersistentCheckpointManager(path, &compat)
	if err == nil {
		t.Fatal("expected legacy checkpoint rejection")
	}
	if !strings.Contains(err.Error(), "older pgferry version") {
		t.Fatalf("expected legacy version message, got: %v", err)
	}
}

func TestCheckpointCompactJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	state := newCheckpointState()
	state.recordFullTable("t1", 100)

	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Compact JSON should not contain newlines
	if strings.Contains(string(data), "\n") {
		t.Error("checkpoint should use compact JSON (no newlines)")
	}

	// Should still be loadable
	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.isTableDone("t1") {
		t.Error("t1 should be done")
	}
}
