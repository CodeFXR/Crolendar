//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openRegularFileReadOnly opens path without blocking on FIFOs/devices (O_NONBLOCK)
// and verifies on the opened file descriptor that it is a regular file.
func openRegularFileReadOnly(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open crontab file: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("open crontab file: invalid descriptor")
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat crontab file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("crontab path must be a regular file")
	}
	return f, nil
}
