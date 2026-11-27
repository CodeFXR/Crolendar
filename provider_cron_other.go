//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os"
)

// openRegularFileReadOnly verifies path is a regular file before and after opening.
func openRegularFileReadOnly(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read crontab file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("crontab path must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open crontab file: %w", err)
	}
	fdInfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat crontab file: %w", err)
	}
	if !fdInfo.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("crontab path must be a regular file")
	}
	return f, nil
}
