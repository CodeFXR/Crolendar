//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openPrivateConfigDir(dir string) (int, error) {
	dirFD, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return -1, os.ErrNotExist
		}
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return -1, errors.New("refusing symlinked or non-directory config parent")
		}
		return -1, fmt.Errorf("cannot open config directory: %w", err)
	}

	var st unix.Stat_t
	if err := unix.Fstat(dirFD, &st); err != nil {
		_ = unix.Close(dirFD)
		return -1, fmt.Errorf("cannot stat config directory: %w", err)
	}
	mode := uint32(st.Mode)
	if mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(dirFD)
		return -1, errors.New("config parent is not a directory")
	}
	euid := uint32(os.Geteuid())
	if st.Uid != euid {
		_ = unix.Close(dirFD)
		return -1, errors.New("refusing config directory not owned by current user")
	}
	if mode&0o022 != 0 {
		_ = unix.Close(dirFD)
		return -1, errors.New("refusing group- or world-writable config directory")
	}
	return dirFD, nil
}

func openConfigFileReadOnly(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	dirFD, err := openPrivateConfigDir(dir)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dirFD)

	fd, err := unix.Openat(dirFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		if errors.Is(err, unix.ELOOP) {
			return nil, errors.New("refusing symlinked config file")
		}
		return nil, fmt.Errorf("cannot open config file: %w", err)
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("cannot stat config file: %w", err)
	}
	mode := uint32(st.Mode)
	if mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, errors.New("config path must be a regular file")
	}
	euid := uint32(os.Geteuid())
	if st.Uid != euid {
		_ = unix.Close(fd)
		return nil, errors.New("refusing config file not owned by current user")
	}
	if mode&0o022 != 0 {
		_ = unix.Close(fd)
		return nil, errors.New("refusing group- or world-writable config file")
	}
	if st.Size > maxConfigBytes {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("config file exceeds %d byte limit", maxConfigBytes)
	}

	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = unix.Close(fd)
		return nil, errors.New("cannot wrap config file descriptor")
	}
	return f, nil
}

func checkTargetRegularOrAbsentAt(dirFD int, base string, euid uint32) error {
	var st unix.Stat_t
	err := unix.Fstatat(dirFD, base, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("cannot inspect target config file: %w", err)
	}
	mode := uint32(st.Mode)
	if mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("refusing to overwrite symlink or non-regular config file")
	}
	if st.Uid != euid {
		return errors.New("refusing to overwrite config file not owned by current user")
	}
	return nil
}

func writeConfigFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	dirFD, err := openPrivateConfigDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return fmt.Errorf("cannot create config directory: %w", mkErr)
		}
		dirFD, err = openPrivateConfigDir(dir)
	}
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)

	euid := uint32(os.Geteuid())
	if err := checkTargetRegularOrAbsentAt(dirFD, base, euid); err != nil {
		return err
	}

	suffix, err := randomTempSuffix()
	if err != nil {
		return fmt.Errorf("cannot generate temp filename: %w", err)
	}
	tmpName := fmt.Sprintf(".%s.tmp.%s", base, suffix)

	tmpFD, err := unix.Openat(dirFD, tmpName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("cannot create temp config file: %w", err)
	}
	tmpFile := os.NewFile(uintptr(tmpFD), filepath.Join(dir, tmpName))
	if tmpFile == nil {
		_ = unix.Close(tmpFD)
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return errors.New("cannot wrap temp config file descriptor")
	}

	renamed := false
	defer func() {
		if !renamed {
			_ = unix.Unlinkat(dirFD, tmpName, 0)
		}
	}()

	if err := unix.Fchmod(tmpFD, 0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot set temp config permissions: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot write temp config file: %w", err)
	}
	if err := unix.Fsync(tmpFD); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot sync temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("cannot close temp config file: %w", err)
	}

	if err := checkTargetRegularOrAbsentAt(dirFD, base, euid); err != nil {
		return err
	}

	if err := unix.Renameat(dirFD, tmpName, dirFD, base); err != nil {
		return fmt.Errorf("cannot atomically replace config file: %w", err)
	}
	renamed = true

	if err := unix.Fsync(dirFD); err != nil && !isIgnorableDirSyncErr(err) {
		return fmt.Errorf("cannot sync config directory: %w", err)
	}
	return nil
}

func isIgnorableDirSyncErr(err error) bool {
	return errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.EROFS) ||
		errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.EBADF)
}
