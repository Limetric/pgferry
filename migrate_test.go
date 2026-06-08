package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestShouldSampleProgressLogTime(t *testing.T) {
	tests := []struct {
		copied int64
		want   bool
	}{
		{1, true},
		{2, false},
		{1023, false},
		{1024, true},
		{1025, false},
		{2048, true},
		{2049, false},
	}
	for _, tt := range tests {
		if got := shouldSampleProgressLogTime(tt.copied); got != tt.want {
			t.Errorf("shouldSampleProgressLogTime(%d) = %v, want %v", tt.copied, got, tt.want)
		}
	}
}

func TestLogChunkProgressHonorsLogLevel(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(orig)
	})

	logChunkProgress(migrateLogLevelVerbose, "users", 7, "starting", 0)
	logChunkProgress(migrateLogLevelTable, "users", 8, "starting", 0)
	logChunkProgress(migrateLogLevelSchema, "users", 9, "done", 12)

	got := buf.String()
	if !strings.Contains(got, "[users] chunk 7 starting") {
		t.Fatalf("verbose chunk log missing from %q", got)
	}
	if strings.Contains(got, "chunk 8") || strings.Contains(got, "chunk 9") {
		t.Fatalf("non-verbose chunk log was emitted: %q", got)
	}
}

func TestShouldLogRowCopyProgressOnlyInVerboseMode(t *testing.T) {
	tests := []struct {
		logLevel string
		want     bool
	}{
		{"", true},
		{migrateLogLevelVerbose, true},
		{migrateLogLevelTable, false},
		{migrateLogLevelSchema, false},
	}
	for _, tt := range tests {
		if got := shouldLogRowCopyProgress(tt.logLevel); got != tt.want {
			t.Fatalf("shouldLogRowCopyProgress(%q) = %v, want %v", tt.logLevel, got, tt.want)
		}
	}
}

func TestNewRowSourcePreallocatesBuffers(t *testing.T) {
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "email"},
			{SourceName: "created_at"},
		},
	}

	rs := newRowSource(nil, table, &sqliteSourceDB{}, defaultTypeMappingConfig(), migrateLogLevelVerbose)

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

type stubChunkPlanningSourceDB struct {
	SourceDB
	maxWorkers int
}

func (s *stubChunkPlanningSourceDB) Name() string { return "mysql" }

func (s *stubChunkPlanningSourceDB) QuoteIdentifier(name string) string {
	return "`" + name + "`"
}

func (s *stubChunkPlanningSourceDB) SourceTableRef(table Table) string {
	return "`" + table.SourceName + "`"
}

func (s *stubChunkPlanningSourceDB) MaxWorkers() int { return s.maxWorkers }

func chunkablePlanningTable(name string) Table {
	return Table{
		SourceName: name,
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int", ColumnType: "int"},
			{SourceName: "payload", PGName: "payload", DataType: "varchar", ColumnType: "varchar(255)"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
}

func nonChunkablePlanningTable(name string) Table {
	return Table{
		SourceName: name,
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "varchar", ColumnType: "varchar(255)"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
}

func TestChunkPlanningWorkers(t *testing.T) {
	tests := []struct {
		name       string
		workers    int
		maxWorkers int
		want       int
	}{
		{name: "uses workers when uncapped", workers: 8, maxWorkers: 0, want: 8},
		{name: "caps by source max workers", workers: 8, maxWorkers: 1, want: 1},
		{name: "caps by planning maximum", workers: 32, maxWorkers: 0, want: 16},
		{name: "defaults to one worker", workers: 0, maxWorkers: 0, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkPlanningWorkers(tt.workers, &stubChunkPlanningSourceDB{maxWorkers: tt.maxWorkers})
			if got != tt.want {
				t.Fatalf("chunkPlanningWorkers(%d, maxWorkers=%d) = %d, want %d", tt.workers, tt.maxWorkers, got, tt.want)
			}
		})
	}
}

func TestBuildChunkPlansWithDeps_UsesBoundedConcurrencyAndPreservesTableOrder(t *testing.T) {
	src := &stubChunkPlanningSourceDB{}
	schema := &Schema{
		Tables: []Table{
			chunkablePlanningTable("alpha"),
			nonChunkablePlanningTable("audit_log"),
			chunkablePlanningTable("beta"),
			chunkablePlanningTable("gamma"),
		},
	}

	var mu sync.Mutex
	openCalls := 0
	closeCounts := map[int]int{}
	inFlight := 0
	maxInFlight := 0
	started := make(chan string, 2)
	release := make(chan struct{})

	rangeByTable := map[string]struct {
		min int64
		max int64
	}{
		"alpha": {min: 1, max: 250},
		"beta":  {min: 11, max: 11},
		"gamma": {min: 1000, max: 1099},
	}

	deps := chunkPlanningDeps{
		openSource: func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			openCalls++
			return &fakeMigrationWorkerSource{id: openCalls, closeMu: &mu, closeCounts: closeCounts}, nil
		},
		queryMinMax: func(ctx context.Context, source dbQuerier, _ SourceDB, table Table, _ ChunkKey) (int64, int64, bool, error) {
			if _, ok := source.(*fakeMigrationWorkerSource); !ok {
				return 0, 0, false, fmt.Errorf("source type = %T, want *fakeMigrationWorkerSource", source)
			}

			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()

			started <- table.SourceName
			<-release

			mu.Lock()
			inFlight--
			mu.Unlock()

			r := rangeByTable[table.SourceName]
			return r.min, r.max, true, nil
		},
	}

	var (
		plans []ChunkPlan
		err   error
	)
	done := make(chan struct{})
	go func() {
		plans, err = buildChunkPlansWithDeps(context.Background(), src, schema, 100, defaultTypeMappingConfig(), 2, migrateLogLevelVerbose, deps)
		close(done)
	}()

	<-started
	<-started
	close(release)
	<-done

	if err != nil {
		t.Fatalf("buildChunkPlansWithDeps() error: %v", err)
	}
	if maxInFlight != 2 {
		t.Fatalf("max concurrent queryMinMax calls = %d, want 2", maxInFlight)
	}
	if openCalls != 2 {
		t.Fatalf("openSource calls = %d, want 2", openCalls)
	}
	if closeCounts[1] != 1 || closeCounts[2] != 1 {
		t.Fatalf("close counts = %v, want each worker source closed once", closeCounts)
	}
	if len(plans) != len(schema.Tables) {
		t.Fatalf("plan count = %d, want %d", len(plans), len(schema.Tables))
	}

	if plans[0].Table.SourceName != "alpha" || plans[0].ChunkKey == nil || len(plans[0].Chunks) != 3 {
		t.Fatalf("alpha plan = %+v, want chunked alpha with 3 chunks", plans[0])
	}
	if plans[1].Table.SourceName != "audit_log" || plans[1].ChunkKey != nil {
		t.Fatalf("audit_log plan = %+v, want non-chunkable plan in original position", plans[1])
	}
	if plans[2].Table.SourceName != "beta" || plans[2].ChunkKey == nil || len(plans[2].Chunks) != 1 {
		t.Fatalf("beta plan = %+v, want single chunk in original position", plans[2])
	}
	if plans[3].Table.SourceName != "gamma" || plans[3].ChunkKey == nil || len(plans[3].Chunks) != 1 {
		t.Fatalf("gamma plan = %+v, want single chunk in original position", plans[3])
	}
}

func TestBuildChunkPlansWithDeps_CancelsSiblingQueriesAfterFirstError(t *testing.T) {
	src := &stubChunkPlanningSourceDB{}
	schema := &Schema{
		Tables: []Table{
			chunkablePlanningTable("fail"),
			chunkablePlanningTable("slow"),
			chunkablePlanningTable("later"),
		},
	}

	slowStarted := make(chan struct{})
	var slowCanceled bool

	_, err := buildChunkPlansWithDeps(
		context.Background(),
		src,
		schema,
		100,
		defaultTypeMappingConfig(),
		2,
		migrateLogLevelVerbose,
		chunkPlanningDeps{
			openSource: func() (migrationWorkerSource, error) {
				return &fakeMigrationWorkerSource{id: 1, closeMu: &sync.Mutex{}, closeCounts: map[int]int{}}, nil
			},
			queryMinMax: func(ctx context.Context, _ dbQuerier, _ SourceDB, table Table, _ ChunkKey) (int64, int64, bool, error) {
				switch table.SourceName {
				case "fail":
					<-slowStarted
					return 0, 0, false, errors.New("boom")
				case "slow":
					close(slowStarted)
					<-ctx.Done()
					slowCanceled = true
					return 0, 0, false, ctx.Err()
				default:
					<-ctx.Done()
					return 0, 0, false, ctx.Err()
				}
			},
		},
	)
	if err == nil {
		t.Fatal("expected chunk planning error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "boom")
	}
	if !slowCanceled {
		t.Fatal("expected sibling query to observe context cancellation")
	}
}

func TestBuildChunkPlansWithDeps_OpenSourceErrorIncludesTableName(t *testing.T) {
	src := &stubChunkPlanningSourceDB{}
	schema := &Schema{
		Tables: []Table{
			chunkablePlanningTable("orders"),
		},
	}

	_, err := buildChunkPlansWithDeps(
		context.Background(),
		src,
		schema,
		100,
		defaultTypeMappingConfig(),
		1,
		migrateLogLevelVerbose,
		chunkPlanningDeps{
			openSource: func() (migrationWorkerSource, error) {
				return nil, errors.New("dial tcp timeout")
			},
			queryMinMax: func(context.Context, dbQuerier, SourceDB, Table, ChunkKey) (int64, int64, bool, error) {
				t.Fatal("queryMinMax should not be called when openSource fails")
				return 0, 0, false, nil
			},
		},
	)
	if err == nil {
		t.Fatal("expected chunk planning error")
	}
	if !strings.Contains(err.Error(), "orders") {
		t.Fatalf("error = %q, want table name context", err.Error())
	}
	if !strings.Contains(err.Error(), "dial tcp timeout") {
		t.Fatalf("error = %q, want original openSource failure", err.Error())
	}
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

func TestTablePGCopyColumns_OrderMatchesColumns(t *testing.T) {
	table := Table{
		Columns: []Column{
			{PGName: "user_id"},
			{PGName: "created_at"},
			{PGName: "meta"},
		},
	}
	got := tablePGCopyColumns(table)
	want := []string{"user_id", "created_at", "meta"}
	if !slices.Equal(got, want) {
		t.Fatalf("tablePGCopyColumns() = %v, want %v", got, want)
	}
}

func TestBuildParallelMigrationWorkItems_ChunksSharePGCopyColumnsSlice(t *testing.T) {
	chunkKey := &ChunkKey{SourceColumn: "id", PGColumn: "id"}
	table := Table{
		SourceName: "orders",
		Columns: []Column{
			{SourceName: "id", PGName: "id"},
			{SourceName: "total", PGName: "total"},
		},
	}
	pgCols := tablePGCopyColumns(table)
	plans := []ChunkPlan{{
		Table:         table,
		ChunkKey:      chunkKey,
		Chunks:        []Chunk{{Index: 0}, {Index: 1}},
		PGCopyColumns: pgCols,
	}}
	items := buildParallelMigrationWorkItems(plans, &fakeMigrationCheckpointManager{})
	if len(items) != 2 {
		t.Fatalf("work items = %d, want 2", len(items))
	}
	a, b := items[0].PGCopyColumns, items[1].PGCopyColumns
	if reflect.ValueOf(a).Pointer() != reflect.ValueOf(b).Pointer() {
		t.Fatal("expected same PGCopyColumns backing slice for all chunks of one table plan")
	}
}

func TestBuildParallelMigrationWorkItems_FullTableCarriesPlanPGCopyColumns(t *testing.T) {
	table := Table{
		SourceName: "profiles",
		Columns: []Column{
			{SourceName: "id", PGName: "id"},
			{SourceName: "bio", PGName: "bio"},
		},
	}
	pgCols := tablePGCopyColumns(table)
	plan := ChunkPlan{Table: table, ChunkSize: 100_000, PGCopyColumns: pgCols}
	items := buildParallelMigrationWorkItems([]ChunkPlan{plan}, &fakeMigrationCheckpointManager{})
	if len(items) != 1 {
		t.Fatalf("work items = %d, want 1", len(items))
	}
	if items[0].ChunkKey != nil {
		t.Fatal("expected full-table work item")
	}
	got := items[0].PGCopyColumns
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(plan.PGCopyColumns).Pointer() {
		t.Fatal("full-table item should reuse plan.PGCopyColumns backing slice")
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
