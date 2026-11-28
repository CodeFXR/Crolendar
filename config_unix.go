//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func openConfigFileReadOnly(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	dirFD, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, os.ErrNotExist
		}
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return nil, errors.New("refusing symlinked or non-directory config parent")
		}
		return nil, fmt.Errorf("cannot open config directory: %w", err)
	}
	_ = syscall.Close(dirFD)

	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, os.ErrNotExist
		}
		if errors.Is(err, syscall.ELOOP) {
			return nil, errors.New("refusing symlinked config file")
		}
		return nil, fmt.Errorf("cannot open config file: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("cannot wrap config file descriptor")
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
	if info.Mode().Perm()&0o002 != 0 {
		_ = f.Close()
		return nil, errors.New("refusing world-writable config file")
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

	dirFD, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return errors.New("refusing symlinked or non-directory config parent")
		}
		return fmt.Errorf("cannot open config directory: %w", err)
	}
	dirFile := os.NewFile(uintptr(dirFD), dir)
	if dirFile == nil {
		_ = syscall.Close(dirFD)
		return errors.New("cannot wrap config directory descriptor")
	}
	defer dirFile.Close()

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

	fd, err := syscall.Open(tmpPath, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create temp config file: %w", err)
	}
	tmpFile := os.NewFile(uintptr(fd), tmpPath)
	if tmpFile == nil {
		_ = syscall.Close(fd)
		_ = os.Remove(tmpPath)
		return errors.New("cannot wrap temp config file descriptor")
	}

	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot set temp config permissions: %w", err)
	}
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

	// Re-check target immediately before rename to avoid replacing a symlink/directory.
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing to overwrite symlink or non-regular config file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot inspect target config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("cannot atomically replace config file: %w", err)
	}
	renamed = true

	// Best-effort directory fsync for crash durability.
	_ = dirFile.Sync()
	return nil
}
