package main

import "testing"

func TestPlanChunks_SingleChunkWhenSmall(t *testing.T) {
	chunks := planChunks(1, 100, 1000)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.Index != 0 || c.LowerBound != 1 || c.UpperBound != 100 || !c.IsLast {
		t.Errorf("chunk = %+v", c)
	}
}

func TestPlanChunks_ExactDivision(t *testing.T) {
	// Range 1..300 with chunk size 100 → 3 chunks
	chunks := planChunks(1, 300, 100)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Chunk 0: [1, 101)
	if chunks[0].LowerBound != 1 || chunks[0].UpperBound != 101 || chunks[0].IsLast {
		t.Errorf("chunk[0] = %+v", chunks[0])
	}
	// Chunk 1: [101, 201)
	if chunks[1].LowerBound != 101 || chunks[1].UpperBound != 201 || chunks[1].IsLast {
		t.Errorf("chunk[1] = %+v", chunks[1])
	}
	// Chunk 2: [201, 300]
	if chunks[2].LowerBound != 201 || chunks[2].UpperBound != 300 || !chunks[2].IsLast {
		t.Errorf("chunk[2] = %+v", chunks[2])
	}
}

func TestPlanChunks_UnevenDivision(t *testing.T) {
	// Range 1..250 with chunk size 100 → 3 chunks
	chunks := planChunks(1, 250, 100)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if !chunks[2].IsLast {
		t.Error("last chunk should have IsLast=true")
	}
	if chunks[2].UpperBound != 250 {
		t.Errorf("last chunk upper bound = %d, want 250", chunks[2].UpperBound)
	}
}

func TestPlanChunks_SingleRow(t *testing.T) {
	chunks := planChunks(42, 42, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.LowerBound != 42 || c.UpperBound != 42 || !c.IsLast {
		t.Errorf("chunk = %+v", c)
	}
}

func TestPlanChunks_LargeRange(t *testing.T) {
	chunks := planChunks(0, 999999, 100000)
	if len(chunks) != 10 {
		t.Fatalf("expected 10 chunks, got %d", len(chunks))
	}
	if !chunks[9].IsLast {
		t.Error("last chunk should have IsLast=true")
	}
	// Verify no gaps
	for i := 1; i < len(chunks); i++ {
		if chunks[i].LowerBound != chunks[i-1].UpperBound {
			t.Errorf("gap between chunks %d and %d: %d != %d",
				i-1, i, chunks[i-1].UpperBound, chunks[i].LowerBound)
		}
	}
}

func TestPlanChunks_NegativeRange(t *testing.T) {
	// Range [-50..50] has 101 values, so with chunk size 100 → 2 chunks
	chunks := planChunks(-50, 50, 100)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for range [-50..50] with size 100 (range=101), got %d", len(chunks))
	}
	if chunks[0].LowerBound != -50 {
		t.Errorf("chunk[0].LowerBound = %d, want -50", chunks[0].LowerBound)
	}
	if !chunks[1].IsLast {
		t.Error("chunk[1] should be IsLast")
	}
}

func TestPlanChunks_DefaultChunkSize(t *testing.T) {
	// chunkSize <= 0 should use default of 100000
	chunks := planChunks(1, 10, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with default size, got %d", len(chunks))
	}
}

func TestBuildChunkedSelectQuery_MySQL(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "name"},
		},
	}
	key := ChunkKey{SourceColumn: "id", PGColumn: "id"}

	// Middle chunk (not last)
	chunk := Chunk{Index: 0, LowerBound: 1, UpperBound: 100, IsLast: false}
	got := buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want := "SELECT `id`, `name` FROM `users` WHERE `id` >= 1 AND `id` < 100 ORDER BY `id`"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// Last chunk
	chunk = Chunk{Index: 1, LowerBound: 100, UpperBound: 150, IsLast: true}
	got = buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want = "SELECT `id`, `name` FROM `users` WHERE `id` >= 100 AND `id` <= 150 ORDER BY `id`"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildChunkedSelectQuery_SQLite(t *testing.T) {
	src := &sqliteSourceDB{}
	table := Table{
		SourceName: "items",
		Columns: []Column{
			{SourceName: "rowid"},
			{SourceName: "value"},
		},
	}
	key := ChunkKey{SourceColumn: "rowid", PGColumn: "rowid"}

	chunk := Chunk{Index: 0, LowerBound: 1, UpperBound: 50, IsLast: true}
	got := buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want := `SELECT "rowid", "value" FROM "items" WHERE "rowid" >= 1 AND "rowid" <= 50 ORDER BY "rowid"`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildChunkedSelectQuery_MSSQLWithSourceSchema(t *testing.T) {
	src := &mssqlSourceDB{sourceSchema: "sales"}
	table := Table{
		SourceName: "orders",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "customer_id"},
		},
	}
	key := ChunkKey{SourceColumn: "id", PGColumn: "id"}
	chunk := Chunk{Index: 0, LowerBound: 1, UpperBound: 100, IsLast: false}

	got := buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want := "SELECT [id], [customer_id] FROM [sales].[orders] WHERE [id] >= 1 AND [id] < 100 ORDER BY [id]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBuildMinMaxQuery_MSSQLWithSourceSchema(t *testing.T) {
	src := &mssqlSourceDB{sourceSchema: "sales"}
	table := Table{SourceName: "orders"}
	key := ChunkKey{SourceColumn: "id", PGColumn: "id"}

	got := buildMinMaxQuery(src, table, key)
	want := "SELECT MIN([id]), MAX([id]) FROM [sales].[orders]"
	if got != want {
		t.Fatalf("buildMinMaxQuery() = %q, want %q", got, want)
	}
}

func TestChunkKeyForTable_SingleNumericPK(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "users",
		PGName:     "users",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int"},
			{SourceName: "name", PGName: "name", DataType: "varchar"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key == nil {
		t.Fatal("expected non-nil ChunkKey for single int PK")
	}
	if key.SourceColumn != "id" || key.PGColumn != "id" {
		t.Errorf("key = %+v", key)
	}
}

func TestChunkKeyForTable_CompositePK(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "tags",
		PGName:     "tags",
		Columns: []Column{
			{SourceName: "tag_id", PGName: "tag_id", DataType: "int"},
			{SourceName: "item_id", PGName: "item_id", DataType: "int"},
		},
		PrimaryKey: &Index{
			Columns: []string{"tag_id", "item_id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key != nil {
		t.Fatal("expected nil ChunkKey for composite PK")
	}
}

func TestChunkKeyForTable_NoPK(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "logs",
		PGName:     "logs",
		Columns: []Column{
			{SourceName: "message", PGName: "message", DataType: "text"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key != nil {
		t.Fatal("expected nil ChunkKey for table with no PK")
	}
}

func TestChunkKeyForTable_NonNumericPK(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "slugs",
		PGName:     "slugs",
		Columns: []Column{
			{SourceName: "slug", PGName: "slug", DataType: "varchar"},
		},
		PrimaryKey: &Index{
			Columns: []string{"slug"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key != nil {
		t.Fatal("expected nil ChunkKey for non-numeric PK")
	}
}

func TestChunkKeyForTable_SQLiteInteger(t *testing.T) {
	src := &sqliteSourceDB{}
	table := Table{
		SourceName: "items",
		PGName:     "items",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "integer", ColumnType: "INTEGER"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key == nil {
		t.Fatal("expected non-nil ChunkKey for SQLite INTEGER PK")
	}
}

func TestChunkKeyForTable_UnsignedBigintExcluded(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "big_table",
		PGName:     "big_table",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "bigint unsigned"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key != nil {
		t.Fatal("expected nil ChunkKey for unsigned bigint PK (exceeds int64 range)")
	}
}

func TestChunkKeyForTable_SignedBigintAllowed(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "big_table",
		PGName:     "big_table",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "bigint"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key == nil {
		t.Fatal("expected non-nil ChunkKey for signed bigint PK")
	}
}

func TestPlanChunks_IndexesAreSequential(t *testing.T) {
	chunks := planChunks(1, 500, 100)
	for i, c := range chunks {
		if c.Index != i {
			t.Errorf("chunk[%d].Index = %d", i, c.Index)
		}
	}
}

func TestPlanChunks_OnlyLastIsLast(t *testing.T) {
	chunks := planChunks(1, 500, 100)
	for i, c := range chunks {
		if i < len(chunks)-1 && c.IsLast {
			t.Errorf("chunk[%d] should not be IsLast", i)
		}
	}
	if !chunks[len(chunks)-1].IsLast {
		t.Error("last chunk should be IsLast")
	}
}
