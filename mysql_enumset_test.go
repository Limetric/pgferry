package main

import (
	"reflect"
	"testing"
)

func TestParseMySQLEnumSetValues(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		err  bool
	}{
		{"enum('new','used')", []string{"new", "used"}, false},
		{"set('a','b','c')", []string{"a", "b", "c"}, false},
		{"enum('it''s','ok')", []string{"it's", "ok"}, false},
		{"enum('a\\'b','c')", []string{"a'b", "c"}, false},
		{"enum(bad)", nil, true},
	}

	for _, tt := range tests {
		got, err := parseMySQLEnumSetValues(tt.in)
		if tt.err {
			if err == nil {
				t.Fatalf("parseMySQLEnumSetValues(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseMySQLEnumSetValues(%q) error: %v", tt.in, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("parseMySQLEnumSetValues(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestParseMySQLSetDefault(t *testing.T) {
	got := parseMySQLSetDefault("a,b,c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseMySQLSetDefault() = %#v, want %#v", got, want)
	}

	got = parseMySQLSetDefault("")
	if len(got) != 0 {
		t.Fatalf("parseMySQLSetDefault(empty) = %#v, want empty", got)
	}
}
