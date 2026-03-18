package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestNewRowSourcePreallocatesBuffers(t *testing.T) {
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "email"},
			{SourceName: "created_at"},
		},
	}

	rs := newRowSource(nil, table, &sqliteSourceDB{}, defaultTypeMappingConfig())

	if len(rs.scanDest) != len(table.Columns) {
		t.Fatalf("scanDest len = %d, want %d", len(rs.scanDest), len(table.Columns))
	}
	if len(rs.scanPtrs) != len(table.Columns) {
		t.Fatalf("scanPtrs len = %d, want %d", len(rs.scanPtrs), len(table.Columns))
	}
	if len(rs.values) != len(table.Columns) {
		t.Fatalf("values len = %d, want %d", len(rs.values), len(table.Columns))
	}

	for i := range rs.scanDest {
		ptr, ok := rs.scanPtrs[i].(*any)
		if !ok {
			t.Fatalf("scanPtrs[%d] type = %T, want *any", i, rs.scanPtrs[i])
		}
		if ptr != &rs.scanDest[i] {
			t.Fatalf("scanPtrs[%d] does not point at scanDest[%d]", i, i)
		}
	}
}

func TestBuildSourceSelectQuery_UsesExplicitQuotedColumnsInOrder(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "Order"},
			{SourceName: "created-at"},
		},
	}

	got := buildSourceSelectQuery(src, table, defaultTypeMappingConfig())
	want := "SELECT `id`, `Order`, `created-at` FROM `users`"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_IncludesGeneratedColumns(t *testing.T) {
	src := &sqliteSourceDB{}
	table := Table{
		SourceName: "metrics",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "computed_value", Extra: "VIRTUAL GENERATED"},
			{SourceName: "stored_total", Extra: "STORED GENERATED"},
		},
	}

	got := buildSourceSelectQuery(src, table, defaultTypeMappingConfig())
	want := `SELECT "id", "computed_value", "stored_total" FROM "metrics"`
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_MSSQLWithSourceSchema(t *testing.T) {
	src := &mssqlSourceDB{sourceSchema: "sales"}
	table := Table{
		SourceName: "orders",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "customer_id"},
		},
	}

	got := buildSourceSelectQuery(src, table, defaultTypeMappingConfig())
	want := "SELECT [id], [customer_id] FROM [sales].[orders]"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_MySQLPostGISUsesWKBExport(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "places",
		Columns: []Column{
			{SourceName: "id", DataType: "int"},
			{SourceName: "shape", DataType: "point"},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.UsePostGIS = true

	got := buildSourceSelectQuery(src, table, tm)
	want := "SELECT `id`, CONCAT(CHAR((ST_SRID(`shape`)) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 8) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 16) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 24) & 255 USING binary), ST_AsWKB(`shape`, 'axis-order=long-lat')) AS `shape` FROM `places`"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_MySQLPostGISLegacyExport(t *testing.T) {
	src := &mysqlSourceDB{
		axisOrderOptionKnown:  true,
		supportsAxisOrderExpr: false,
	}
	table := Table{
		SourceName: "places",
		Columns: []Column{
			{SourceName: "id", DataType: "int"},
			{SourceName: "shape", DataType: "point"},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.UsePostGIS = true

	got := buildSourceSelectQuery(src, table, tm)
	want := "SELECT `id`, CONCAT(CHAR((ST_SRID(`shape`)) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 8) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 16) & 255 USING binary), CHAR(((ST_SRID(`shape`)) >> 24) & 255 USING binary), ST_AsWKB(`shape`)) AS `shape` FROM `places`"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_MariaDBSpatialWKTUsesMySQLFamilyPath(t *testing.T) {
	src := &mariadbSourceDB{}
	table := Table{
		SourceName: "places",
		Columns: []Column{
			{SourceName: "id", DataType: "int"},
			{SourceName: "shape", DataType: "point"},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.SpatialMode = "wkt_text"

	got := buildSourceSelectQuery(src, table, tm)
	want := "SELECT `id`, ST_AsText(`shape`) AS `shape` FROM `places`"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

type fakeMigrationCheckpointManager struct {
	doneTables map[string]bool
	doneChunks map[string]map[int]bool

	mu            sync.Mutex
	recordedFull  []string
	recordedChunk []string
}

func (m *fakeMigrationCheckpointManager) IsTableDone(tableName string) bool {
	return m.doneTables[tableName]
}

func (m *fakeMigrationCheckpointManager) IsChunkCompleted(tableName string, chunkIndex int) bool {
	if chunks, ok := m.doneChunks[tableName]; ok {
		return chunks[chunkIndex]
	}
	return false
}

func (m *fakeMigrationCheckpointManager) RecordFullTable(tableName string, _ int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordedFull = append(m.recordedFull, tableName)
}

func (m *fakeMigrationCheckpointManager) RecordChunk(tableName string, chunkIndex int, _ int64, _ int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordedChunk = append(m.recordedChunk, tableName+":"+fmt.Sprint(chunkIndex))
}

func (m *fakeMigrationCheckpointManager) Flush() error   { return nil }
func (m *fakeMigrationCheckpointManager) Cleanup() error { return nil }

type fakeMigrationWorkerSource struct {
	id          int
	closeMu     *sync.Mutex
	closeCounts map[int]int
}

func (s *fakeMigrationWorkerSource) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (s *fakeMigrationWorkerSource) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	s.closeCounts[s.id]++
	return nil
}

func TestBuildParallelMigrationWorkItems_SkipsCompletedResumeEntries(t *testing.T) {
	chunkKey := &ChunkKey{SourceColumn: "id", PGColumn: "id"}
	plans := []ChunkPlan{
		{Table: Table{SourceName: "users"}},
		{Table: Table{SourceName: "profiles"}},
		{
			Table:    Table{SourceName: "orders"},
			ChunkKey: chunkKey,
			Chunks: []Chunk{
				{Index: 0},
				{Index: 1},
				{Index: 2},
			},
		},
		{
			Table:    Table{SourceName: "items"},
			ChunkKey: chunkKey,
			Chunks: []Chunk{
				{Index: 0},
				{Index: 1},
			},
		},
	}
	mgr := &fakeMigrationCheckpointManager{
		doneTables: map[string]bool{"users": true},
		doneChunks: map[string]map[int]bool{"orders": {1: true}},
	}

	got := buildParallelMigrationWorkItems(plans, mgr)
	if len(got) != 5 {
		t.Fatalf("work item count = %d, want 5", len(got))
	}

	summaries := []string{
		got[1].Table.SourceName + ":" + fmt.Sprint(got[1].Chunk.Index),
		got[2].Table.SourceName + ":" + fmt.Sprint(got[2].Chunk.Index),
		got[3].Table.SourceName + ":" + fmt.Sprint(got[3].Chunk.Index),
		got[4].Table.SourceName + ":" + fmt.Sprint(got[4].Chunk.Index),
	}
	want := []string{"orders:0", "orders:2", "items:0", "items:1"}
	if !slices.Equal(summaries, want) {
		t.Fatalf("work items = %v, want %v", summaries, want)
	}
	if got[0].Table.SourceName != "profiles" || got[0].ChunkKey != nil {
		t.Fatalf("first work item = %+v, want full-table profiles item", got[0])
	}
	for _, item := range got[1:] {
		if item.ChunkKey == nil {
			t.Fatalf("chunked item %+v missing chunk key", item)
		}
		if item.ChunkCount != len(findPlanByTable(plans, item.Table.SourceName).Chunks) {
			t.Fatalf("chunk count for %s = %d, want %d", item.Table.SourceName, item.ChunkCount, len(findPlanByTable(plans, item.Table.SourceName).Chunks))
		}
	}
}

func TestRunParallelMigrationWorkers_ReusesSourceAcrossItems(t *testing.T) {
	workItems := []migrationWorkItem{
		{Table: Table{SourceName: "a"}},
		{Table: Table{SourceName: "b"}},
		{Table: Table{SourceName: "c"}},
		{Table: Table{SourceName: "d"}},
	}
	mgr := &fakeMigrationCheckpointManager{}

	var mu sync.Mutex
	openCalls := 0
	closeCounts := map[int]int{}
	seenSources := map[int]int{}
	execCalls := 0
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- runParallelMigrationWorkers(
			context.Background(),
			2,
			func() (migrationWorkerSource, error) {
				mu.Lock()
				defer mu.Unlock()
				openCalls++
				return &fakeMigrationWorkerSource{id: openCalls, closeMu: &mu, closeCounts: closeCounts}, nil
			},
			workItems,
			mgr,
			func(ctx context.Context, source dbQuerier, item migrationWorkItem) (int64, error) {
				src, ok := source.(*fakeMigrationWorkerSource)
				if !ok {
					return 0, fmt.Errorf("source type = %T, want *fakeMigrationWorkerSource", source)
				}

				mu.Lock()
				execCalls++
				callNumber := execCalls
				seenSources[src.id]++
				mu.Unlock()

				if callNumber <= 2 {
					started <- struct{}{}
					<-release
				}
				return 1, nil
			},
		)
	}()

	<-started
	<-started
	close(release)

	err := <-errCh
	if err != nil {
		t.Fatalf("runParallelMigrationWorkers() error: %v", err)
	}

	if openCalls != 2 {
		t.Fatalf("openSource calls = %d, want 2", openCalls)
	}
	if execCalls != 4 {
		t.Fatalf("execute calls = %d, want 4", execCalls)
	}
	if len(seenSources) != 2 {
		t.Fatalf("distinct worker sources = %d, want 2", len(seenSources))
	}
	if closeCounts[1] != 1 || closeCounts[2] != 1 {
		t.Fatalf("close counts = %v, want each worker source closed once", closeCounts)
	}
	recorded := slices.Clone(mgr.recordedFull)
	slices.Sort(recorded)
	if !slices.Equal(recorded, []string{"a", "b", "c", "d"}) {
		t.Fatalf("recorded full tables = %v, want %v", recorded, []string{"a", "b", "c", "d"})
	}
}

func TestRunParallelMigrationWorkers_CancelsRemainingWorkOnFailure(t *testing.T) {
	workItems := []migrationWorkItem{
		{Table: Table{SourceName: "fail"}},
		{Table: Table{SourceName: "other-1"}},
		{Table: Table{SourceName: "other-2"}},
		{Table: Table{SourceName: "other-3"}},
		{Table: Table{SourceName: "other-4"}},
	}

	var mu sync.Mutex
	processed := []string{}
	err := runParallelMigrationWorkers(
		context.Background(),
		2,
		func() (migrationWorkerSource, error) {
			return &fakeMigrationWorkerSource{id: 1, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, item migrationWorkItem) (int64, error) {
			mu.Lock()
			processed = append(processed, item.Table.SourceName)
			mu.Unlock()

			if item.Table.SourceName == "fail" {
				return 0, errors.New("boom")
			}
			<-ctx.Done()
			return 0, ctx.Err()
		},
	)
	if err == nil {
		t.Fatal("expected worker pool error")
	}
	if got := err.Error(); !strings.Contains(got, "table fail: boom") {
		t.Fatalf("error = %q, want substring %q", got, "table fail: boom")
	}
	if len(processed) >= len(workItems) {
		t.Fatalf("processed = %v, want cancellation before all work items were executed", processed)
	}
}

func TestRunParallelMigrationWorkers_PropagatesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runParallelMigrationWorkers(
		ctx,
		2,
		func() (migrationWorkerSource, error) {
			return &fakeMigrationWorkerSource{id: 1, closeMu: &sync.Mutex{}, closeCounts: map[int]int{}}, nil
		},
		[]migrationWorkItem{{Table: Table{SourceName: "users"}}},
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			return 0, ctx.Err()
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want %v", err, context.Canceled)
	}
}

func findPlanByTable(plans []ChunkPlan, tableName string) ChunkPlan {
	for _, plan := range plans {
		if plan.Table.SourceName == tableName {
			return plan
		}
	}
	return ChunkPlan{}
}
