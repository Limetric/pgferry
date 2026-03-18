package main

import (
	"strings"
	"testing"
)

func TestNormalizeClockString_NoFraction(t *testing.T) {
	got, err := normalizeClockString("12:30:45")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12:30:45.000000" {
		t.Fatalf("got %q, want '12:30:45.000000'", got)
	}
}

func TestNormalizeClockString_ShortFraction(t *testing.T) {
	got, err := normalizeClockString("12:30:45.12")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12:30:45.120000" {
		t.Fatalf("got %q, want '12:30:45.120000'", got)
	}
}

func TestNormalizeClockString_LongFraction(t *testing.T) {
	got, err := normalizeClockString("12:30:45.1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12:30:45.123456" {
		t.Fatalf("got %q, want '12:30:45.123456' (truncated)", got)
	}
}

func TestNormalizeClockString_ExactSixFraction(t *testing.T) {
	got, err := normalizeClockString("12:30:45.123456")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12:30:45.123456" {
		t.Fatalf("got %q, want '12:30:45.123456'", got)
	}
}

func TestNormalizeClockString_EmptyString(t *testing.T) {
	_, err := normalizeClockString("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
	if !strings.Contains(err.Error(), "empty time") {
		t.Fatalf("expected 'empty time' error, got: %v", err)
	}
}

func TestNormalizeClockString_MalformedBase(t *testing.T) {
	_, err := normalizeClockString("25:61:00:00")
	if err == nil {
		t.Fatal("expected error for malformed time")
	}
	if !strings.Contains(err.Error(), "cannot normalize") {
		t.Fatalf("expected normalize error, got: %v", err)
	}
}

func TestNormalizeClockString_ShortBase(t *testing.T) {
	_, err := normalizeClockString("1:2:3")
	if err == nil {
		t.Fatal("expected error for short base")
	}
}

func TestNormalizeClockString_Whitespace(t *testing.T) {
	got, err := normalizeClockString("  12:30:45  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "12:30:45.000000" {
		t.Fatalf("got %q, want '12:30:45.000000'", got)
	}
}

func TestNormalizeTimestampString_BasicFormats(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2024-01-15 10:30:00", "2024-01-15T10:30:00.000000"},
		{"2024-01-15T10:30:00", "2024-01-15T10:30:00.000000"},
		{"2024-01-15 10:30:00.123456", "2024-01-15T10:30:00.123456"},
	}
	for _, tt := range tests {
		got, err := normalizeTimestampString(tt.input)
		if err != nil {
			t.Fatalf("normalizeTimestampString(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("normalizeTimestampString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeTimestampString_Invalid(t *testing.T) {
	_, err := normalizeTimestampString("not a timestamp")
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

func TestNormalizeTimestamptzString_BasicFormats(t *testing.T) {
	got, err := normalizeTimestamptzString("2024-01-15T10:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2024-01-15T10:30:00.000000Z" {
		t.Fatalf("got %q, want UTC format", got)
	}
}

func TestNormalizeTimestamptzString_Invalid(t *testing.T) {
	_, err := normalizeTimestamptzString("not a timestamp")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCanonicalizeJSONFragment_Basic(t *testing.T) {
	got, err := canonicalizeJSONFragment(`{"b":2,"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	// Go JSON marshal uses sorted keys
	if got != `{"a":1,"b":2}` {
		t.Fatalf("got %q, want sorted JSON", got)
	}
}

func TestCanonicalizeJSONFragment_ByteInput(t *testing.T) {
	got, err := canonicalizeJSONFragment([]byte(`[1,2,3]`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "[1,2,3]" {
		t.Fatalf("got %q", got)
	}
}

func TestCanonicalizeJSONFragment_InvalidJSON(t *testing.T) {
	_, err := canonicalizeJSONFragment("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidationSampleOffsets_SmallTable(t *testing.T) {
	// Table with fewer rows than sample size
	offsets := validationSampleOffsets(5, 16)
	if len(offsets) != 5 {
		t.Fatalf("expected 5 offsets for 5-row table, got %d", len(offsets))
	}
	// All offsets should be unique
	seen := make(map[int64]bool)
	for _, o := range offsets {
		if seen[o] {
			t.Fatalf("duplicate offset %d", o)
		}
		seen[o] = true
	}
}

func TestValidationSampleOffsets_SingleRow(t *testing.T) {
	offsets := validationSampleOffsets(1, 16)
	if len(offsets) != 1 {
		t.Fatalf("expected 1 offset for 1-row table, got %d", len(offsets))
	}
	if offsets[0] != 0 {
		t.Fatalf("expected offset 0, got %d", offsets[0])
	}
}

func TestValidationSampleOffsets_ExactSampleSize(t *testing.T) {
	offsets := validationSampleOffsets(16, 16)
	if len(offsets) != 16 {
		t.Fatalf("expected 16 offsets, got %d", len(offsets))
	}
}

func TestCanonicalizeValidationFragment_NumericText(t *testing.T) {
	got, err := canonicalizeValidationFragment("42", validationKindNumericText)
	if err != nil {
		t.Fatal(err)
	}
	// renderValidationText returns "42", then marshalJSONString wraps it
	if got != `"42"` {
		t.Fatalf("got %q, want %q", got, `"42"`)
	}
}

func TestCanonicalizeValidationFragment_Bytea(t *testing.T) {
	got, err := canonicalizeValidationFragment([]byte{0xDE, 0xAD}, validationKindBytea)
	if err != nil {
		t.Fatal(err)
	}
	// hex-encoded then JSON-string-wrapped
	if got != `"dead"` {
		t.Fatalf("got %q, want %q", got, `"dead"`)
	}
}

func TestCanonicalizeValidationFragment_ByteaEmpty(t *testing.T) {
	got, err := canonicalizeValidationFragment([]byte{}, validationKindBytea)
	if err != nil {
		t.Fatal(err)
	}
	// Empty bytes → empty hex → JSON string ""
	if got != `""` {
		t.Fatalf("got %q, want %q for empty bytea", got, `""`)
	}
}

func TestCanonicalizeValidationFragment_NilValue(t *testing.T) {
	got, err := canonicalizeValidationFragment(nil, validationKindText)
	if err != nil {
		t.Fatal(err)
	}
	if got != "null" {
		t.Fatalf("got %q, want 'null' for nil", got)
	}
}
