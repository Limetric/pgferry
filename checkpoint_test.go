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
	summary := checkpointCompatibilitySummary{
		SourceType:         "mysql",
		SourceDBName:       "appdb",
		TargetSchema:       "public",
		SourceSnapshotMode: "none",
		ChunkSize:          100000,
		IdentifierCase:     "snake",
		TypeMapping:        defaultTypeMappingConfig(),
		Tables: []checkpointCompatibilityTable{
			{SourceName: "users", PGName: "users", TableHash: "users-hash"},
		},
	}
	return testCheckpointCompatibilityWithSummary(summary)
}

func testCheckpointCompatibilityWithSummary(summary checkpointCompatibilitySummary) checkpointCompatibility {
	fingerprint, err := checkpointCompatibilityFingerprint(summary)
	if err != nil {
		panic(err)
	}
	return checkpointCompatibility{
		Fingerprint: fingerprint,
		Summary:     &summary,
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	state.recordFullTable("users", 1000)
	state.recordChunk("orders", testChunk(0), 500, 3)
	state.recordChunk("orders", testChunk(1), 300, 3)

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
	if loaded.Compatibility == nil {
		t.Fatal("Compatibility should not be nil")
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
	state.recordChunk("t1", testChunk(0), 100, 3)
	state.recordChunk("t1", testChunk(1), 200, 3)
	state.recordChunk("t1", testChunk(2), 150, 3)

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
	if mgr.IsChunkCompleted("anything", testChunk(0)) {
		t.Error("noop should never report chunk completed")
	}

	// Record calls should not panic
	mgr.RecordFullTable("t1", 1000)
	mgr.RecordChunk("t1", testChunk(0), 500, 3)

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
	if mgr.IsChunkCompleted("t1", testChunk(0)) {
		t.Error("fresh manager should not report chunk completed")
	}
	if mgr.state.Compatibility == nil {
		t.Fatal("Compatibility should not be nil")
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
	state.recordChunk("orders", testChunk(0), 500, 3)
	state.recordChunk("orders", testChunk(1), 300, 3)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	// nil compatibility disables resume-shape validation; this test exercises
	// persisted skip-set loading only.
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
	if !mgr.IsChunkCompleted("orders", testChunk(0)) {
		t.Error("orders chunk 0 should be completed")
	}
	if !mgr.IsChunkCompleted("orders", testChunk(1)) {
		t.Error("orders chunk 1 should be completed")
	}
	if mgr.IsChunkCompleted("orders", testChunk(2)) {
		t.Error("orders chunk 2 should not be completed")
	}
	if mgr.IsChunkCompleted("nonexistent", testChunk(0)) {
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
		mgr.RecordChunk("t1", testChunk(i), 100, checkpointFlushCount+5)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("checkpoint file should not exist before flush threshold")
	}

	// One more record should trigger the flush
	mgr.RecordChunk("t1", testChunk(checkpointFlushCount-1), 100, checkpointFlushCount+5)
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
				mgr.RecordChunk(tableName, testChunk(idx), 100, numWorkers*chunksPerWorker)
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
	mgr.RecordChunk("t1", testChunk(0), 100, 5)

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
	mgr.RecordChunk("orders", testChunk(0), 500, 5)
	mgr.RecordChunk("orders", testChunk(1), 500, 5)
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
	if !mgr2.IsChunkCompleted("orders", testChunk(0)) {
		t.Error("resumed manager should skip orders chunk 0")
	}
	if !mgr2.IsChunkCompleted("orders", testChunk(1)) {
		t.Error("resumed manager should skip orders chunk 1")
	}
	if mgr2.IsChunkCompleted("orders", testChunk(2)) {
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

	mgr.RecordChunk("t1", testChunk(0), 100, 5)

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
	state.recordChunk("users", testChunk(0), 100, 2)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatible := compat
	incompatibleSummary := *compat.Summary
	incompatibleSummary.ChunkSize = 50000
	incompatible = testCheckpointCompatibilityWithSummary(incompatibleSummary)

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "chunk_size changed") {
		t.Fatalf("expected chunk_size mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsChangedMigrationMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatible := compat
	incompatibleSummary := *compat.Summary
	incompatibleSummary.SchemaOnly = true
	incompatible = testCheckpointCompatibilityWithSummary(incompatibleSummary)

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "schema_only changed") {
		t.Fatalf("expected migration mode mismatch, got: %v", err)
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
	typeMap := effectiveTypeMapping(&cfg)

	compat, err := buildCheckpointCompatibility(&cfg, schema, src, "appdb", typeMap)
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

	changedCompat, err := buildCheckpointCompatibility(&cfg, schema, src, "appdb", typeMap)
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

func TestPersistentCheckpointManager_RejectsPreChunkPlannerFixCheckpoint(t *testing.T) {
	// Version 2 checkpoints were written by a planner whose chunk ordinals mapped
	// to different key ranges (pre-#257). Resuming one would skip or re-copy rows.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte(`{"version":2,"started_at":"2026-01-01T00:00:00Z","tables":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	compat := testCheckpointCompatibility()
	_, err := newPersistentCheckpointManager(path, &compat)
	if err == nil {
		t.Fatal("expected pre-planner-fix checkpoint rejection")
	}
	if !strings.Contains(err.Error(), "older pgferry version") {
		t.Fatalf("expected legacy version message, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsMissingCompatibilityMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(path, []byte(`{"version":3,"started_at":"2026-01-01T00:00:00Z","tables":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	compat := testCheckpointCompatibility()
	_, err := newPersistentCheckpointManager(path, &compat)
	if err == nil {
		t.Fatal("expected missing compatibility metadata rejection")
	}
	if !strings.Contains(err.Error(), "missing resume compatibility metadata") {
		t.Fatalf("expected missing metadata message, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsChangedSnapshotMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatible := compat
	incompatibleSummary := *compat.Summary
	incompatibleSummary.SourceSnapshotMode = "single_tx"
	incompatible = testCheckpointCompatibilityWithSummary(incompatibleSummary)

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "source snapshot mode changed") {
		t.Fatalf("expected source snapshot mode mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsChangedUnloggedTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatibleSummary := *compat.Summary
	incompatibleSummary.UnloggedTables = true
	incompatible := testCheckpointCompatibilityWithSummary(incompatibleSummary)

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "unlogged_tables changed") {
		t.Fatalf("expected unlogged_tables mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_RejectsChangedFilteredTableSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatibleSummary := *compat.Summary
	incompatibleSummary.Tables = append(incompatibleSummary.Tables, checkpointCompatibilityTable{
		SourceName: "orders",
		PGName:     "orders",
		TableHash:  "orders-hash",
	})
	incompatible := testCheckpointCompatibilityWithSummary(incompatibleSummary)

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "table added") {
		t.Fatalf("expected filtered table-set mismatch, got: %v", err)
	}
}

func TestPersistentCheckpointManager_FallsBackToGenericFingerprintReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	incompatible := compat
	incompatible.Fingerprint = "manually-changed-fingerprint"

	_, err := newPersistentCheckpointManager(path, &incompatible)
	if err == nil {
		t.Fatal("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "migration compatibility fingerprint changed") {
		t.Fatalf("expected fallback fingerprint message, got: %v", err)
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

func TestPersistentCheckpointManager_RejectsMovedKeyRangeOnResume(t *testing.T) {
	// Chunk ordinals are only meaningful relative to the key range they were
	// planned over: chunk i covers [min + i*chunkSize, ...). If rows are inserted
	// between runs, MAX moves, chunk i denotes a different slice of the table, and
	// trusting the ordinal would skip or duplicate rows. Fail loudly instead.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	first, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	original := planChunks(1, 250, 100)
	if err := first.PrepareTablePlan("orders", 1, 250, original); err != nil {
		t.Fatalf("PrepareTablePlan: %v", err)
	}
	// Complete the last chunk only, then crash.
	last := original[len(original)-1]
	first.RecordChunk("orders", last, 50, len(original))
	if err := first.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// The source grew to 400 before the retry, so the replanned chunk 2 covers a
	// different range than the one recorded.
	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	grown := planChunks(1, 400, 100)
	err = resumed.PrepareTablePlan("orders", 1, 400, grown)
	if err == nil {
		t.Fatal("expected the resume to be refused after the source key range moved")
	}
	if !strings.Contains(err.Error(), "key range changed") {
		t.Fatalf("error should explain the moved key range, got: %v", err)
	}
}

func TestPersistentCheckpointManager_AcceptsUnchangedKeyRangeOnResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	first, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	chunks := planChunks(1, 250, 100)
	if err := first.PrepareTablePlan("orders", 1, 250, chunks); err != nil {
		t.Fatalf("PrepareTablePlan: %v", err)
	}
	first.RecordChunk("orders", chunks[0], 100, len(chunks))
	if err := first.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if err := resumed.PrepareTablePlan("orders", 1, 250, chunks); err != nil {
		t.Fatalf("an unchanged key range must resume cleanly, got: %v", err)
	}
	if !resumed.IsChunkCompleted("orders", chunks[0]) {
		t.Error("chunk 0 was completed and should be skipped on resume")
	}
	if resumed.IsChunkCompleted("orders", chunks[1]) {
		t.Error("chunk 1 was never completed and must be copied")
	}
}

func TestPersistentCheckpointManager_ChunkMatchIsBoundsAwareNotOrdinalOnly(t *testing.T) {
	// Second line of defence: even if a chunk with the same ordinal appears, it is
	// only treated as completed when it covers the same key bounds.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	mgr, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	recorded := Chunk{Index: 2, LowerBound: 201, UpperBound: 250, IsLast: true}
	mgr.RecordChunk("orders", recorded, 50, 3)
	if err := mgr.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if !resumed.IsChunkCompleted("orders", recorded) {
		t.Error("the identical chunk should be reported completed")
	}
	sameOrdinalDifferentRange := Chunk{Index: 2, LowerBound: 201, UpperBound: 301, IsLast: false}
	if resumed.IsChunkCompleted("orders", sameOrdinalDifferentRange) {
		t.Error("a chunk with the same ordinal but different bounds must not count as completed")
	}
}

func TestCheckpointRecordsChunkBoundsAndKeyRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	state := newCheckpointState()
	state.recordKeyRange("orders", 1, 250)
	state.recordChunk("orders", Chunk{Index: 0, LowerBound: 1, UpperBound: 101}, 100, 3)
	if err := saveCheckpoint(path, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tc := loaded.Tables["orders"]
	if tc.KeyRange == nil {
		t.Fatal("key range was not persisted")
	}
	if tc.KeyRange.Min != 1 || tc.KeyRange.Max != 250 {
		t.Errorf("key range = %+v, want {1 250}", tc.KeyRange)
	}
	res := tc.CompletedChunks[0]
	if res.LowerBound != 1 || res.UpperBound != 101 {
		t.Errorf("chunk bounds = %d..%d, want 1..101", res.LowerBound, res.UpperBound)
	}
}

func TestPersistentCheckpointManager_RefusesResumeAfterCrashDuringFullTableCopy(t *testing.T) {
	// The scenario from #260: a full-table COPY of a PK-less table commits 40M rows
	// in PostgreSQL, then the machine dies before the checkpoint records it. Nothing
	// in the target can detect the resulting duplicates, so the resume must refuse
	// rather than copy the table a second time.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	crashed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := crashed.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}
	// The marker must already be durable — a hard crash runs no more code here.
	// (No Flush(), no ClearInFlight(): simulate kill -9.)

	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if resumed.IsTableDone("events") {
		t.Fatal("the interrupted table must not be reported as done")
	}
	err = resumed.BeginFullTable("events")
	if err == nil {
		t.Fatal("expected the resume to refuse a table whose full-table copy was interrupted by a crash")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error should warn about duplication, got: %v", err)
	}
}

func TestPersistentCheckpointManager_CleanExitClearsInFlightMarker(t *testing.T) {
	// A cancelled or failed COPY rolls back, so a clean exit must not leave a marker
	// that would block a legitimate resume.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	first, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := first.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}
	// Simulate an interrupt: the COPY rolled back, and the run exits cleanly.
	first.ClearInFlight()
	if err := first.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if err := resumed.BeginFullTable("events"); err != nil {
		t.Fatalf("a cleanly interrupted table must be resumable, got: %v", err)
	}
}

func TestPersistentCheckpointManager_CompletedFullTableResumesAsDone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	first, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := first.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}
	first.RecordFullTable("events", 40_000_000)
	if err := first.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	resumed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if !resumed.IsTableDone("events") {
		t.Error("a completed full-table copy should be skipped on resume")
	}
	// Recording completion must clear the in-flight marker, or the table would be
	// both done and blocked.
	if err := resumed.BeginFullTable("events"); err != nil {
		t.Errorf("a completed table must not carry a stale in-flight marker: %v", err)
	}
}

func TestBeginFullTableIsDurableBeforeTheCopyStarts(t *testing.T) {
	// The marker exists to detect a crash between the COPY commit and the checkpoint
	// write. If it is only in memory, it cannot detect the crash it exists for.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	mgr, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}

	// Read the file directly: no Flush() was called after BeginFullTable.
	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil || loaded.Tables["events"] == nil {
		t.Fatal("BeginFullTable did not persist the table entry")
	}
	if !loaded.Tables["events"].InFlight {
		t.Error("the in-flight marker must be on disk before the COPY starts")
	}
}

func TestPersistentCheckpointManager_CrashMarkerSurvivesTheRefusedResume(t *testing.T) {
	// A refused resume still exits "cleanly" and clears in-flight markers. It must
	// clear only the markers IT created — if it also cleared the one inherited from
	// the crashed run, the next resume would happily copy the ambiguous table and
	// duplicate it, defeating the guard entirely.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	crashed, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := crashed.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}
	// Hard crash: no ClearInFlight, no Flush.

	// Second run: refuses, then exits cleanly (which calls ClearInFlight + Flush).
	second, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("resume manager: %v", err)
	}
	if err := second.BeginFullTable("events"); err == nil {
		t.Fatal("the second run should have refused the ambiguous table")
	}
	second.ClearInFlight()
	if err := second.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Third run must STILL refuse.
	third, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("third manager: %v", err)
	}
	if err := third.BeginFullTable("events"); err == nil {
		t.Fatal("the crash marker was erased by the refused resume; a third run would silently duplicate the table")
	}
}

func TestBeginFullTableIsDurableEvenWhileAnotherFlushIsInProgress(t *testing.T) {
	// Flush() skips when a flush is already running. BeginFullTable must not rely on
	// that path, or its marker can be lost exactly when concurrent workers are busy.
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	compat := testCheckpointCompatibility()

	mgr, err := newPersistentCheckpointManager(path, &compat)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Simulate a flush already in progress.
	mgr.mu.Lock()
	mgr.flushing = true
	mgr.mu.Unlock()

	if err := mgr.BeginFullTable("events"); err != nil {
		t.Fatalf("BeginFullTable: %v", err)
	}

	loaded, err := loadCheckpoint(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil || loaded.Tables["events"] == nil || !loaded.Tables["events"].InFlight {
		t.Fatal("the in-flight marker was not durable while another flush was in progress")
	}
}
