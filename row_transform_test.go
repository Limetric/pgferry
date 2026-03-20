package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fallbackTransformSource struct {
	SourceDB
	calls int
}

func (s *fallbackTransformSource) TransformValue(val any, col Column, _ TypeMappingConfig) (any, error) {
	s.calls++
	text, ok := val.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", val)
	}
	return strings.ToUpper(text) + ":" + col.SourceName, nil
}

func TestNewRowSourcePrecomputesTransformers(t *testing.T) {
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "email"},
			{SourceName: "created_at"},
		},
	}

	rs := newRowSource(nil, table, &sqliteSourceDB{}, defaultTypeMappingConfig())

	if len(rs.transformers) != len(table.Columns) {
		t.Fatalf("transformers len = %d, want %d", len(rs.transformers), len(table.Columns))
	}
	for i, fn := range rs.transformers {
		if fn == nil {
			t.Fatalf("transformers[%d] is nil", i)
		}
	}
}

func TestBuildRowValueTransformers_FallbackMatchesSourceTransformValue(t *testing.T) {
	src := &fallbackTransformSource{}
	table := Table{
		Columns: []Column{
			{SourceName: "first"},
			{SourceName: "second"},
		},
	}
	inputs := []any{"hello", "world"}

	transformers := buildRowValueTransformers(table, src, defaultTypeMappingConfig())
	if len(transformers) != len(inputs) {
		t.Fatalf("len(transformers) = %d, want %d", len(transformers), len(inputs))
	}

	for i, input := range inputs {
		got, err := transformers[i](input)
		if err != nil {
			t.Fatalf("transformers[%d](%v) error: %v", i, input, err)
		}
		want, err := src.TransformValue(input, table.Columns[i], defaultTypeMappingConfig())
		if err != nil {
			t.Fatalf("TransformValue(%v) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("transformers[%d](%v) = %v, want %v", i, input, got, want)
		}
	}

	if src.calls != len(inputs)*2 {
		t.Fatalf("fallback TransformValue calls = %d, want %d", src.calls, len(inputs)*2)
	}
}

func TestBuildRowValueTransformers_MySQLMatchesTransformValue(t *testing.T) {
	src := &mysqlSourceDB{}
	typeMap := defaultTypeMappingConfig()
	typeMap.Binary16AsUUID = true
	typeMap.StringUUIDAsUUID = true
	typeMap.TinyInt1AsBoolean = true
	typeMap.SetMode = "text_array"
	typeMap.BitMode = "bit"
	typeMap.TimeMode = "interval"
	typeMap.UsePostGIS = true

	table := Table{
		Columns: []Column{
			{SourceName: "uuid_bin", DataType: "binary", ColumnType: "binary(16)"},
			{SourceName: "doc", DataType: "json", ColumnType: "json"},
			{SourceName: "external_id", DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36},
			{SourceName: "is_active", DataType: "tinyint", ColumnType: "tinyint(1)"},
			{SourceName: "flags", DataType: "set", ColumnType: "set('a','b','c')"},
			{SourceName: "mask", DataType: "bit", ColumnType: "bit(8)", MySQLBitWidth: 8},
			{SourceName: "duration", DataType: "time", ColumnType: "time"},
			{SourceName: "created_at", DataType: "datetime", ColumnType: "datetime"},
			{SourceName: "title", DataType: "varchar", ColumnType: "varchar(255)", CharMaxLen: 255},
			{SourceName: "shape", DataType: "point", ColumnType: "point"},
		},
	}
	inputs := []any{
		[]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		[]byte("hello\x00world"),
		" 550E8400-E29B-41D4-A716-446655440000 ",
		[]byte("1"),
		[]byte("a,c"),
		[]byte{0b10110011},
		"12:30:15",
		time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC),
		[]byte("title\x00value"),
		[]byte{
			0xe6, 0x10, 0x00, 0x00,
			0x00,
			0x00, 0x00, 0x00, 0x01,
			0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
	}

	transformers := buildRowValueTransformers(table, src, typeMap)
	if len(transformers) != len(inputs) {
		t.Fatalf("len(transformers) = %d, want %d", len(transformers), len(inputs))
	}

	for i, col := range table.Columns {
		got, err := transformers[i](inputs[i])
		if err != nil {
			t.Fatalf("transformers[%d](%s) error: %v", i, col.SourceName, err)
		}
		want, err := src.TransformValue(inputs[i], col, typeMap)
		if err != nil {
			t.Fatalf("TransformValue(%s) error: %v", col.SourceName, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("transformers[%d](%s) = %#v, want %#v", i, col.SourceName, got, want)
		}
	}
}

func TestBuildRowValueTransformers_MSSQLMatchesTransformValue(t *testing.T) {
	src := &mssqlSourceDB{}
	typeMap := defaultTypeMappingConfig()

	table := Table{
		Columns: []Column{
			{SourceName: "id", DataType: "uniqueidentifier"},
			{SourceName: "price", DataType: "money"},
			{SourceName: "summary", DataType: "nvarchar"},
			{SourceName: "payload", DataType: "json"},
			{SourceName: "flag", DataType: "bit"},
		},
	}
	inputs := []any{
		[]byte{0x04, 0x03, 0x02, 0x01, 0x06, 0x05, 0x08, 0x07, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		float64(19.99),
		[]byte("hello\x00world"),
		"json\x00payload",
		true,
	}

	transformers := buildRowValueTransformers(table, src, typeMap)
	if len(transformers) != len(inputs) {
		t.Fatalf("len(transformers) = %d, want %d", len(transformers), len(inputs))
	}

	for i, col := range table.Columns {
		got, err := transformers[i](inputs[i])
		if err != nil {
			t.Fatalf("transformers[%d](%s) error: %v", i, col.SourceName, err)
		}
		want, err := src.TransformValue(inputs[i], col, typeMap)
		if err != nil {
			t.Fatalf("TransformValue(%s) error: %v", col.SourceName, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("transformers[%d](%s) = %#v, want %#v", i, col.SourceName, got, want)
		}
	}
}

func BenchmarkRowTransformLoopMySQLDispatch(b *testing.B) {
	src := &mysqlSourceDB{}
	table, values, typeMap := benchmarkMySQLTransformFixture()
	benchmarkTransformLoopDispatch(b, src, table, values, typeMap)
}

func BenchmarkRowTransformLoopMySQLPlanned(b *testing.B) {
	src := &mysqlSourceDB{}
	table, values, typeMap := benchmarkMySQLTransformFixture()
	benchmarkTransformLoopPlanned(b, src, table, values, typeMap)
}

func BenchmarkRowTransformLoopMSSQLDispatch(b *testing.B) {
	src := &mssqlSourceDB{}
	table, values, typeMap := benchmarkMSSQLTransformFixture()
	benchmarkTransformLoopDispatch(b, src, table, values, typeMap)
}

func BenchmarkRowTransformLoopMSSQLPlanned(b *testing.B) {
	src := &mssqlSourceDB{}
	table, values, typeMap := benchmarkMSSQLTransformFixture()
	benchmarkTransformLoopPlanned(b, src, table, values, typeMap)
}

func benchmarkTransformLoopDispatch(b *testing.B, src SourceDB, table Table, values []any, typeMap TypeMappingConfig) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for colIdx, col := range table.Columns {
			got, err := src.TransformValue(values[colIdx], col, typeMap)
			if err != nil {
				b.Fatalf("TransformValue(%s) error: %v", col.SourceName, err)
			}
			consumeBenchmarkValue(got)
		}
	}
}

func benchmarkTransformLoopPlanned(b *testing.B, src SourceDB, table Table, values []any, typeMap TypeMappingConfig) {
	b.Helper()
	transformers := buildRowValueTransformers(table, src, typeMap)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for colIdx, transform := range transformers {
			got, err := transform(values[colIdx])
			if err != nil {
				b.Fatalf("transformers[%d] error: %v", colIdx, err)
			}
			consumeBenchmarkValue(got)
		}
	}
}

func benchmarkMySQLTransformFixture() (Table, []any, TypeMappingConfig) {
	typeMap := defaultTypeMappingConfig()
	typeMap.Binary16AsUUID = true
	typeMap.StringUUIDAsUUID = true
	typeMap.TinyInt1AsBoolean = true
	typeMap.SetMode = "text_array"
	typeMap.BitMode = "bit"
	typeMap.TimeMode = "interval"
	typeMap.UsePostGIS = true

	patternCols := []Column{
		{SourceName: "uuid_bin", DataType: "binary", ColumnType: "binary(16)"},
		{SourceName: "doc", DataType: "json", ColumnType: "json"},
		{SourceName: "external_id", DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36},
		{SourceName: "is_active", DataType: "tinyint", ColumnType: "tinyint(1)"},
		{SourceName: "flags", DataType: "set", ColumnType: "set('a','b','c')"},
		{SourceName: "mask", DataType: "bit", ColumnType: "bit(8)", MySQLBitWidth: 8},
		{SourceName: "duration", DataType: "time", ColumnType: "time"},
		{SourceName: "created_at", DataType: "datetime", ColumnType: "datetime"},
		{SourceName: "title", DataType: "varchar", ColumnType: "varchar(255)", CharMaxLen: 255},
		{SourceName: "shape", DataType: "point", ColumnType: "point"},
	}
	patternVals := []any{
		[]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		[]byte("hello\x00world"),
		"550E8400-E29B-41D4-A716-446655440000",
		[]byte("1"),
		[]byte("a,c"),
		[]byte{0b10110011},
		"12:30:15",
		time.Date(2026, time.March, 20, 10, 0, 0, 0, time.UTC),
		[]byte("title\x00value"),
		[]byte{
			0xe6, 0x10, 0x00, 0x00,
			0x00,
			0x00, 0x00, 0x00, 0x01,
			0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		},
	}

	columns := make([]Column, 0, 50)
	values := make([]any, 0, 50)
	for group := 0; group < 5; group++ {
		for i := range patternCols {
			col := patternCols[i]
			col.SourceName = fmt.Sprintf("%s_%d", col.SourceName, group)
			columns = append(columns, col)
			values = append(values, cloneBenchmarkValue(patternVals[i]))
		}
	}

	return Table{SourceName: "bench_mysql", Columns: columns}, values, typeMap
}

func benchmarkMSSQLTransformFixture() (Table, []any, TypeMappingConfig) {
	typeMap := defaultTypeMappingConfig()

	patternCols := []Column{
		{SourceName: "id", DataType: "uniqueidentifier"},
		{SourceName: "price", DataType: "money"},
		{SourceName: "summary", DataType: "nvarchar"},
		{SourceName: "payload", DataType: "json"},
		{SourceName: "flag", DataType: "bit"},
	}
	patternVals := []any{
		[]byte{0x04, 0x03, 0x02, 0x01, 0x06, 0x05, 0x08, 0x07, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		float64(19.99),
		[]byte("hello\x00world"),
		"json\x00payload",
		true,
	}

	columns := make([]Column, 0, 50)
	values := make([]any, 0, 50)
	for group := 0; group < 10; group++ {
		for i := range patternCols {
			col := patternCols[i]
			col.SourceName = fmt.Sprintf("%s_%d", col.SourceName, group)
			columns = append(columns, col)
			values = append(values, cloneBenchmarkValue(patternVals[i]))
		}
	}

	return Table{SourceName: "bench_mssql", Columns: columns}, values, typeMap
}

func cloneBenchmarkValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return bytes.Clone(t)
	default:
		return t
	}
}

func consumeBenchmarkValue(v any) {
	benchmarkTransformSink = v
}

var benchmarkTransformSink any
