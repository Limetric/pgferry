package main

import "testing"

func TestStripNULString(t *testing.T) {
	t.Parallel()
	if s := stripNULString("hello"); s != "hello" {
		t.Fatalf("no NUL: got %q", s)
	}
	if s := stripNULString("a\x00b"); s != "ab" {
		t.Fatalf("with NUL: got %q", s)
	}
}

func TestStripNULBytesToString(t *testing.T) {
	t.Parallel()
	if s := stripNULBytesToString([]byte("hello")); s != "hello" {
		t.Fatalf("no NUL: got %q", s)
	}
	if s := stripNULBytesToString([]byte("a\x00b")); s != "ab" {
		t.Fatalf("with NUL: got %q", s)
	}
}
