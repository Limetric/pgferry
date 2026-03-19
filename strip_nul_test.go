package main

import "testing"

func TestStripNULString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"", ""},
		{"\x00", ""},
		{"a\x00b", "ab"},
		{"\x00\x00", ""},
	}
	for _, tt := range tests {
		if got := stripNULString(tt.in); got != tt.want {
			t.Errorf("stripNULString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStripNULBytesToString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"no NUL", []byte("hello"), "hello"},
		{"nil", nil, ""},
		{"empty", []byte{}, ""},
		{"only NUL", []byte{0}, ""},
		{"with NUL", []byte("a\x00b"), "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripNULBytesToString(tt.in); got != tt.want {
				t.Fatalf("stripNULBytesToString() = %q, want %q", got, tt.want)
			}
		})
	}
}
