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

// checkpointVersion gates whether a saved checkpoint may be resumed. Bump it
// whenever the chunk planner's index-to-key-range mapping changes: completed
// chunks are recorded by ordinal, so a checkpoint written by a planner that
// mapped ordinal i to a different key range would silently skip or re-copy rows
// on resume. Version 3 covers the overflow and sparse-range widening fixes in
// planChunks.
const checkpointVersion = 3

// CheckpointState persists the progress of a chunked migration.
type CheckpointState struct {
	Version       int                         `json:"version"`
	StartedAt     time.Time                   `json:"started_at"`
	Compatibility *checkpointCompatibility    `json:"compatibility,omitempty"`
	Tables        map[string]*TableCheckpoint `json:"tables"`
}

// TableCheckpoint tracks per-table progress.
type TableCheckpoint struct {
	ChunkCount      int                 `json:"chunk_count"`
	CompletedChunks map[int]ChunkResult `json:"completed_chunks"`
	FullTableDone   bool                `json:"full_table_done"`
	TotalRowsCopied int64               `json:"total_rows_copied"`
	// KeyRange is the [MIN, MAX] of the chunk key when the plan was built. Chunk
	// ordinals are only meaningful relative to it: chunk i covers
	// [min + i*chunkSize, ...). If the source key range moves between runs, the
	// same ordinal denotes a different range, so a resume must not trust it.
	KeyRange *CheckpointKeyRange `json:"key_range,omitempty"`
	// InFlight marks a full-table copy that started but has not been recorded as
	// complete. A full-table COPY is all-or-nothing but is committed *before* the
	// checkpoint records it, so a hard crash in that window leaves rows in the
	// target that the checkpoint does not know about. On a table with no primary
	// key nothing would detect the resulting duplicates, so the marker is written
	// durably before the copy starts and a resume that finds it refuses to guess.
	// A clean exit clears it: a cancelled or failed COPY rolls back, leaving no rows.
	InFlight bool `json:"in_flight,omitempty"`
}

// CheckpointKeyRange is the chunk key's MIN/MAX at plan time.
type CheckpointKeyRange struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// ChunkResult records the outcome of a single chunk copy.
type ChunkResult struct {
	CompletedAt time.Time `json:"completed_at"`
	RowsCopied  int64     `json:"rows_copied"`
	// Bounds record the key range this chunk actually covered, so a resume can
	// verify that the replanned chunk with the same ordinal covers the same rows
	// rather than trusting the ordinal alone.
	LowerBound int64 `json:"lower_bound"`
	UpperBound int64 `json:"upper_bound"`
	IsLast     bool  `json:"is_last"`
}

// newCheckpointState creates a fresh checkpoint state.
func newCheckpointState() *CheckpointState {
	return &CheckpointState{
		Version:   checkpointVersion,
		StartedAt: time.Now(),
		Tables:    make(map[string]*TableCheckpoint),
	}
}

func newCheckpointStateWithCompatibility(compat *checkpointCompatibility) *CheckpointState {
	state := newCheckpointState()
	if compat != nil {
		state.Compatibility = cloneCheckpointCompatibility(compat)
	}
	return state
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
	// Older versions still parse so that `checkpoint status` can inspect them and
	// validateCheckpointCompatibility can refuse the resume with actionable advice.
	switch state.Version {
	case 1, 2, checkpointVersion:
	default:
		return nil, fmt.Errorf("unsupported checkpoint version %d (expected 1..%d)", state.Version, checkpointVersion)
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

// writeCheckpointFile writes pre-marshaled data to the checkpoint path atomically
// and durably.
//
// Durability matters because the checkpoint is recorded *after* its COPY has
// already committed in PostgreSQL. If the file is left in the page cache and the
// machine loses power, the committed rows outlive the record of them, and the
// resumed run copies the same rows a second time. On a table without a primary
// key nothing detects that, so it silently doubles the data. The temp file and
// the directory entry are therefore both fsynced before this returns.
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp checkpoint: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename checkpoint: %w", err)
	}
	// Without syncing the directory, the rename itself can be lost on power failure
	// even though the file contents are durable.
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync checkpoint directory: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
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
func (cs *CheckpointState) recordChunk(tableName string, chunk Chunk, rowsCopied int64, chunkCount int) {
	tc, ok := cs.Tables[tableName]
	if !ok {
		tc = &TableCheckpoint{
			ChunkCount:      chunkCount,
			CompletedChunks: make(map[int]ChunkResult),
		}
		cs.Tables[tableName] = tc
	}
	tc.CompletedChunks[chunk.Index] = ChunkResult{
		CompletedAt: time.Now(),
		RowsCopied:  rowsCopied,
		LowerBound:  chunk.LowerBound,
		UpperBound:  chunk.UpperBound,
		IsLast:      chunk.IsLast,
	}
	tc.TotalRowsCopied += rowsCopied
}

// recordKeyRange stores the chunk key's MIN/MAX for a table so a later resume can
// detect that the source key range moved.
func (cs *CheckpointState) recordKeyRange(tableName string, min, max int64) {
	tc, ok := cs.Tables[tableName]
	if !ok {
		tc = &TableCheckpoint{CompletedChunks: make(map[int]ChunkResult)}
		cs.Tables[tableName] = tc
	}
	tc.KeyRange = &CheckpointKeyRange{Min: min, Max: max}
}

// recordFullTable records a completed full-table copy in the checkpoint state.
func (cs *CheckpointState) recordFullTable(tableName string, rowsCopied int64) {
	tc := cs.tableCheckpoint(tableName)
	tc.FullTableDone = true
	tc.InFlight = false
	tc.TotalRowsCopied = rowsCopied
}

func (cs *CheckpointState) tableCheckpoint(tableName string) *TableCheckpoint {
	tc, ok := cs.Tables[tableName]
	if !ok {
		tc = &TableCheckpoint{CompletedChunks: make(map[int]ChunkResult)}
		cs.Tables[tableName] = tc
	}
	if tc.CompletedChunks == nil {
		tc.CompletedChunks = make(map[int]ChunkResult)
	}
	return tc
}

// beginFullTable marks a full-table copy as started.
func (cs *CheckpointState) beginFullTable(tableName string) {
	cs.tableCheckpoint(tableName).InFlight = true
}

// clearInFlightTable drops one table's in-flight marker.
func (cs *CheckpointState) clearInFlightTable(tableName string) {
	if tc, ok := cs.Tables[tableName]; ok {
		tc.InFlight = false
	}
}

// checkpointManager abstracts checkpoint persistence during data migration.
// When resume is disabled, a noop implementation avoids all filesystem overhead.
type checkpointManager interface {
	// IsTableDone reports whether a table was fully completed in a previous run.
	IsTableDone(tableName string) bool
	// IsChunkCompleted reports whether a specific chunk was completed in a previous
	// run. The chunk is matched on its key bounds, not just its ordinal.
	IsChunkCompleted(tableName string, chunk Chunk) bool
	// PrepareTablePlan registers a freshly built chunk plan and validates it against
	// what the checkpoint recorded for the table, failing the resume if the source
	// key range moved. Chunk ordinals are only meaningful relative to the key range
	// they were planned over, so a moved range silently redefines them.
	PrepareTablePlan(tableName string, min, max int64, chunks []Chunk) error
	// BeginFullTable durably marks a full-table copy as started, and refuses the
	// resume if a previous run crashed while copying this table.
	BeginFullTable(tableName string) error
	// ClearInFlight drops in-flight markers on a clean exit, where any unfinished
	// COPY rolled back.
	ClearInFlight()
	// RecordFullTable records a completed full-table copy.
	RecordFullTable(tableName string, rowsCopied int64)
	// RecordChunk records a completed chunk.
	RecordChunk(tableName string, chunk Chunk, rowsCopied int64, chunkCount int)
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

func (n *noopCheckpointManager) IsTableDone(string) bool               { return false }
func (n *noopCheckpointManager) IsChunkCompleted(string, Chunk) bool   { return false }
func (n *noopCheckpointManager) RecordFullTable(string, int64)         {}
func (n *noopCheckpointManager) RecordChunk(string, Chunk, int64, int) {}
func (n *noopCheckpointManager) Flush() error                          { return nil }
func (n *noopCheckpointManager) Cleanup() error                        { return deleteCheckpoint(n.path) }

func (n *noopCheckpointManager) PrepareTablePlan(string, int64, int64, []Chunk) error { return nil }
func (n *noopCheckpointManager) BeginFullTable(string) error                          { return nil }
func (n *noopCheckpointManager) ClearInFlight()                                       {}

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
	skipTables      map[string]bool
	skipChunks      map[string]map[int]ChunkResult
	loadedKeyRanges map[string]CheckpointKeyRange
	// loadedInFlight names tables whose full-table copy was interrupted by a hard
	// crash in a previous run (read-only after init).
	loadedInFlight map[string]bool
	// ownInFlight names tables this run marked in flight. Only these may be cleared
	// on a clean exit: a marker inherited from a previous crashed run must survive,
	// or the next resume would re-copy an ambiguous table and duplicate it.
	ownInFlight map[string]bool

	// writeMu serializes actual checkpoint writes, so a caller that needs durability
	// can wait for its own write instead of being skipped by an in-progress flush.
	writeMu sync.Mutex
}

// newPersistentCheckpointManager creates a checkpoint manager that persists
// state to disk with batched writes. If a checkpoint file exists at path,
// it is loaded and skip sets are pre-computed for fast lookups.
func newPersistentCheckpointManager(path string, compat *checkpointCompatibility) (*persistentCheckpointManager, error) {
	loaded, err := loadCheckpoint(path)
	if err != nil {
		return nil, err
	}

	state := loaded
	if state == nil {
		state = newCheckpointStateWithCompatibility(compat)
	} else if compat != nil {
		if err := validateCheckpointCompatibility(path, state, *compat); err != nil {
			return nil, err
		}
	}

	m := &persistentCheckpointManager{
		state:           state,
		path:            path,
		lastFlush:       time.Now(),
		skipTables:      make(map[string]bool),
		skipChunks:      make(map[string]map[int]ChunkResult),
		loadedKeyRanges: make(map[string]CheckpointKeyRange),
		loadedInFlight:  make(map[string]bool),
		ownInFlight:     make(map[string]bool),
	}

	if loaded != nil {
		log.Printf("resuming from checkpoint (started %s)", loaded.StartedAt.Format(time.RFC3339))
		if loaded.Compatibility != nil && loaded.Compatibility.Fingerprint != "" {
			log.Printf("checkpoint compatibility fingerprint: %s", loaded.Compatibility.Fingerprint)
		}
		for name, tc := range loaded.Tables {
			if tc.FullTableDone {
				m.skipTables[name] = true
			}
			if len(tc.CompletedChunks) > 0 {
				s := make(map[int]ChunkResult, len(tc.CompletedChunks))
				for idx, res := range tc.CompletedChunks {
					s[idx] = res
				}
				m.skipChunks[name] = s
			}
			if tc.KeyRange != nil {
				m.loadedKeyRanges[name] = *tc.KeyRange
			}
			if tc.InFlight && !tc.FullTableDone {
				m.loadedInFlight[name] = true
			}
		}
	}

	return m, nil
}

func (m *persistentCheckpointManager) IsTableDone(tableName string) bool {
	return m.skipTables[tableName]
}

// IsChunkCompleted matches on the chunk's key bounds, not just its ordinal. The
// ordinal alone is not a stable identity: chunk i means [min + i*chunkSize, ...),
// so if the source key range moved between runs, ordinal i denotes a different
// slice of the table. PrepareTablePlan already rejects a moved range, and this is
// the second line of defence.
func (m *persistentCheckpointManager) IsChunkCompleted(tableName string, chunk Chunk) bool {
	s, ok := m.skipChunks[tableName]
	if !ok {
		return false
	}
	recorded, ok := s[chunk.Index]
	if !ok {
		return false
	}
	return recorded.LowerBound == chunk.LowerBound &&
		recorded.UpperBound == chunk.UpperBound &&
		recorded.IsLast == chunk.IsLast
}

// PrepareTablePlan fails the resume when the recorded key range or chunk bounds no
// longer match the freshly built plan, rather than silently skipping or re-copying
// rows.
func (m *persistentCheckpointManager) PrepareTablePlan(tableName string, min, max int64, chunks []Chunk) error {
	if recorded, ok := m.loadedKeyRanges[tableName]; ok {
		if recorded.Min != min || recorded.Max != max {
			return fmt.Errorf(
				"table %s: the source key range changed since the checkpoint was written "+
					"(was %d..%d, now %d..%d), so recorded chunk progress no longer refers to the same rows; "+
					"delete %s and rerun the migration, or resume against an unchanged source",
				tableName, recorded.Min, recorded.Max, min, max, m.path)
		}
	}

	planned := make(map[int]Chunk, len(chunks))
	for _, chunk := range chunks {
		planned[chunk.Index] = chunk
	}
	for index, recorded := range m.skipChunks[tableName] {
		chunk, ok := planned[index]
		if !ok {
			return fmt.Errorf(
				"table %s: chunk %d was completed in a previous run but no longer exists in the chunk plan; "+
					"delete %s and rerun the migration",
				tableName, index, m.path)
		}
		if recorded.LowerBound != chunk.LowerBound || recorded.UpperBound != chunk.UpperBound || recorded.IsLast != chunk.IsLast {
			return fmt.Errorf(
				"table %s: chunk %d covered keys %d..%d in the checkpointed run but covers %d..%d now, "+
					"so resuming would skip or duplicate rows; delete %s and rerun the migration",
				tableName, index, recorded.LowerBound, recorded.UpperBound,
				chunk.LowerBound, chunk.UpperBound, m.path)
		}
	}

	m.mu.Lock()
	m.state.recordKeyRange(tableName, min, max)
	m.dirty = true
	m.mu.Unlock()
	return nil
}

// BeginFullTable durably records that a full-table copy is starting.
//
// The COPY commits in PostgreSQL before the checkpoint records it. If the process
// is killed in that window, the rows are in the target but the checkpoint does not
// know, and a resume copies them a second time — silently, on a table with no
// primary key to reject the duplicates. There is no way to tell after the fact
// whether zero rows or every row was committed, so a resume that finds the marker
// refuses to guess.
//
// A clean exit (success, error, or interrupt) clears the marker, because a COPY
// that did not complete was rolled back. Only a hard crash leaves it set.
func (m *persistentCheckpointManager) BeginFullTable(tableName string) error {
	if m.loadedInFlight[tableName] {
		return fmt.Errorf(
			"table %s: a previous run was interrupted while copying this table and did not record the outcome, "+
				"so it may hold a partial or complete copy already; resuming could duplicate every row in it. "+
				"Delete %s and rerun the migration (or empty the target table first)",
			tableName, m.path)
	}

	m.mu.Lock()
	m.state.beginFullTable(tableName)
	m.ownInFlight[tableName] = true
	m.dirty = true
	m.mu.Unlock()

	// flushNow, not Flush: Flush skips when another flush is already running, and a
	// marker that is not durable before the COPY starts cannot detect the crash it
	// exists to detect.
	return m.flushNow()
}

// ClearInFlight drops only the in-flight markers this run set. Any COPY still
// unfinished at a clean exit was cancelled or failed, and therefore rolled back.
//
// A marker inherited from a previous crashed run is deliberately left alone: the
// table it names may already hold a full copy, and clearing it would let the next
// resume copy that table again — silently duplicating it, which is the very thing
// the marker exists to prevent.
func (m *persistentCheckpointManager) ClearInFlight() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ownInFlight) == 0 {
		return
	}
	for name := range m.ownInFlight {
		m.state.clearInFlightTable(name)
	}
	m.ownInFlight = make(map[string]bool)
	m.dirty = true
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

func (m *persistentCheckpointManager) RecordChunk(tableName string, chunk Chunk, rowsCopied int64, chunkCount int) {
	m.mu.Lock()
	m.state.recordChunk(tableName, chunk, rowsCopied, chunkCount)
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

	m.writeMu.Lock()
	writeErr := writeCheckpointFile(m.path, data)
	m.writeMu.Unlock()

	m.mu.Lock()
	m.flushing = false
	if writeErr == nil {
		// Clamp as flushNow does: the two snapshot m.unflushed independently, so an
		// interleave can subtract the same records twice and drive the counter
		// negative, which would delay the next count-triggered flush.
		m.unflushed -= flushedCount
		if m.unflushed < 0 {
			m.unflushed = 0
		}
		m.dirty = m.unflushed > 0
		m.lastFlush = time.Now()
	}
	m.mu.Unlock()
	return writeErr
}

// flushNow writes the current state and waits for it to be durable. Unlike Flush,
// it never skips because another flush is in progress: callers rely on it for the
// durability guarantee, not merely for eventual persistence.
func (m *persistentCheckpointManager) flushNow() error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	m.mu.Lock()
	flushedCount := m.unflushed
	data, err := json.Marshal(m.state)
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if writeErr := writeCheckpointFile(m.path, data); writeErr != nil {
		return writeErr
	}

	m.mu.Lock()
	m.unflushed -= flushedCount
	if m.unflushed < 0 {
		m.unflushed = 0
	}
	m.dirty = m.unflushed > 0
	m.lastFlush = time.Now()
	m.mu.Unlock()
	return nil
}

func (m *persistentCheckpointManager) Cleanup() error {
	return deleteCheckpoint(m.path)
}
