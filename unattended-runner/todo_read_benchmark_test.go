package main

import (
	"os"
	"testing"
)

// Explicit operator-only measurement; normal CI never opens a machine store.
func BenchmarkTodoPrivateSnapshotRead(b *testing.B) {
	path := os.Getenv("CC_TODO_BENCHMARK_SNAPSHOT")
	if path == "" {
		b.Skip("an explicit private snapshot is required")
	}
	r := &Runner{TodoPath: path}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.todo(2626); err != nil {
			b.Fatal("snapshot read failed")
		}
	}
}
