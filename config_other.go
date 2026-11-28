//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func openConfigFileReadOnly(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("cannot inspect config directory: %w", err)
	}
	if !dirInfo.Mode().IsDir() {
		return nil, errors.New("refusing symlinked or non-directory config parent")
	}

	linfo, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("cannot inspect config file: %w", err)
	}
	if !linfo.Mode().IsRegular() {
		return nil, errors.New("config path must be a regular file")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open config file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot stat config file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("config path must be a regular file")
	}
	if info.Size() > maxConfigBytes {
		_ = f.Close()
		return nil, fmt.Errorf("config file exceeds %d byte limit", maxConfigBytes)
	}
	return f, nil
}

func writeConfigFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if info, err := os.Lstat(dir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot inspect config directory: %w", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("cannot create config directory: %w", err)
		}
	} else if !info.Mode().IsDir() {
		return errors.New("refusing symlinked or non-directory config parent")
	}

	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing to overwrite symlink or non-regular config file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot inspect target config file: %w", err)
	}

	suffix, err := randomTempSuffix()
	if err != nil {
		return fmt.Errorf("cannot generate temp filename: %w", err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%s", filepath.Base(path), suffix))

	tmpFile, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create temp config file: %w", err)
	}

	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot write temp config file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot sync temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("cannot close temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("cannot atomically replace config file: %w", err)
	}
	renamed = true
	return nil
}
