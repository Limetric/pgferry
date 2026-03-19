package main

import (
	"bytes"
	"strings"
)

// stripNULString returns s with NUL bytes removed. If s contains no NUL, s is returned unchanged.
func stripNULString(s string) string {
	if strings.IndexByte(s, 0) < 0 {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}

// stripNULBytesToString converts b to a string with NUL bytes removed. If b contains no NUL,
// this is equivalent to string(b) without an extra copy for stripping.
func stripNULBytesToString(b []byte) string {
	if bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	return string(bytes.ReplaceAll(b, []byte{0}, nil))
}
