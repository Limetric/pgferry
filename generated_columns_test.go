package main

import "testing"

func TestIsGeneratedColumn(t *testing.T) {
	tests := []struct {
		name string
		col  Column
		want bool
	}{
		{
			name: "virtual generated",
			col:  Column{Extra: "VIRTUAL GENERATED"},
			want: true,
		},
		{
			name: "stored generated",
			col:  Column{Extra: "STORED GENERATED"},
			want: true,
		},
		{
			name: "default generated not flagged",
			col:  Column{Extra: "DEFAULT_GENERATED"},
			want: false,
		},
		{
			name: "regular column",
			col:  Column{Extra: "auto_increment"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGeneratedColumn(tt.col)
			if got != tt.want {
				t.Fatalf("isGeneratedColumn(%q) = %t, want %t", tt.col.Extra, got, tt.want)
			}
		})
	}
}

func TestCollectGeneratedColumnWarnings(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "orders",
				Columns: []Column{
					{SourceName: "id", Extra: "auto_increment"},
					{SourceName: "total", Extra: "VIRTUAL GENERATED"},
				},
			},
			{
				SourceName: "customers",
				Columns: []Column{
					{SourceName: "full_name", Extra: "STORED GENERATED"},
				},
			},
		},
	}

	warnings := collectGeneratedColumnWarnings(schema)
	if len(warnings) != 2 {
		t.Fatalf("warnings len = %d, want 2 (%v)", len(warnings), warnings)
	}
}
