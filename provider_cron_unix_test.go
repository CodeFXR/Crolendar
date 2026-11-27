//go:build unix

package main

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCronProviderRejectsFIFOWithoutBlocking(t *testing.T) {
	fifoPath := filepath.Join(t.TempDir(), "blocked.fifo")
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		t.Fatalf("mkfifo failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := NewCronProvider(fifoPath).Load(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected FIFO path to be rejected")
		}
	case <-time.After(time.Second):
		t.Fatalf("CronProvider blocked on FIFO path")
	}
}
