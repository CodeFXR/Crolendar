//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
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

func TestCronProviderFIFOSwapTOCTOUNeverBlocks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "swap.cron")
	reg := filepath.Join(dir, "reg.cron")
	fifo := filepath.Join(dir, "pipe.fifo")

	if err := os.WriteFile(reg, []byte("0 2 * * * /bin/true\n"), 0600); err != nil {
		t.Fatalf("write reg failed: %v", err)
	}
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatalf("mkfifo failed: %v", err)
	}
	if err := os.Symlink(reg, target); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}

	var stop atomic.Bool
	swapDone := make(chan struct{})
	go func() {
		defer close(swapDone)
		tmpLink := filepath.Join(dir, "tmp.link")
		for i := 0; !stop.Load(); i++ {
			dst := reg
			if i%2 == 1 {
				dst = fifo
			}
			_ = os.Remove(tmpLink)
			if os.Symlink(dst, tmpLink) == nil {
				_ = os.Rename(tmpLink, target)
			}
		}
	}()

	for i := 0; i < 400; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		done := make(chan struct{})
		go func() {
			_, _ = NewCronProvider(target).Load(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			stop.Store(true)
			<-swapDone
			cancel()
			t.Fatalf("CronProvider blocked during FIFO swap TOCTOU at attempt %d", i)
		}
		cancel()
	}
	stop.Store(true)
	<-swapDone
}
