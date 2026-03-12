package main

import (
	"fmt"
	"sync/atomic"
)

var introspectionStubDriverSeq atomic.Int64

func nextIntrospectionStubDriverName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, introspectionStubDriverSeq.Add(1))
}
