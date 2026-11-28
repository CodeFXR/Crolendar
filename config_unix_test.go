//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestConfigUnixPermissionsSymlinkFIFOAndWorldWritableGuards(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "crolendar")
	cfgPath := filepath.Join(cfgDir, "config.json")

	// 1. Atomic save creates 0700 dir, 0600 file, and leaves no stray temp files.
	if err := SaveConfig(cfgPath, map[JobKey]JobOverride{
		{Source: "cron", ID: "j1"}: {Alias: "Safe", HasAlias: true, Color: "#0052D4"},
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	dirInfo, err := os.Stat(cfgDir)
	if err != nil {
		t.Fatalf("stat cfgDir failed: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected private 0700 config dir, got %04o", perm)
	}
	fileInfo, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat cfgPath failed: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 config file, got %04o", perm)
	}
	entries, err := os.ReadDir(cfgDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("expected only config.json with no leftover temp files, got %v", entries)
	}

	// 2. Reject world-writable config file on load.
	if err := os.Chmod(cfgPath, 0o666); err != nil {
		t.Fatalf("chmod 0666 failed: %v", err)
	}
	if _, _, err := LoadConfig(cfgPath); err == nil || !strings.Contains(err.Error(), "world-writable") {
		t.Fatalf("expected world-writable config file to be rejected, got %v", err)
	}

	// 3. Reject symlinked config file on both load and save without touching target.
	target := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(target, []byte("DO_NOT_TOUCH"), 0o600); err != nil {
		t.Fatalf("write target failed: %v", err)
	}
	symCfg := filepath.Join(dir, "sym_config.json")
	if err := os.Symlink(target, symCfg); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}
	if _, _, err := LoadConfig(symCfg); err == nil {
		t.Fatalf("expected LoadConfig to reject symlinked config file")
	}
	if err := SaveConfig(symCfg, map[JobKey]JobOverride{
		{Source: "cron", ID: "j1"}: {Alias: "X", HasAlias: true, Color: "#111111"},
	}); err == nil {
		t.Fatalf("expected SaveConfig to reject symlinked config file")
	}
	targetData, _ := os.ReadFile(target)
	if string(targetData) != "DO_NOT_TOUCH" {
		t.Fatalf("symlink target was modified: %q", string(targetData))
	}

	// 4. Reject symlinked parent config directory on both load and save.
	realDir := filepath.Join(dir, "real_dir")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatalf("mkdir realDir failed: %v", err)
	}
	symDir := filepath.Join(dir, "sym_dir")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Fatalf("symlink dir failed: %v", err)
	}
	symDirCfg := filepath.Join(symDir, "config.json")
	if err := os.WriteFile(filepath.Join(realDir, "config.json"), []byte(`{"version":1,"overrides":[]}`), 0o600); err != nil {
		t.Fatalf("write real config failed: %v", err)
	}
	if _, _, err := LoadConfig(symDirCfg); err == nil {
		t.Fatalf("expected LoadConfig to reject symlinked parent directory")
	}
	if err := SaveConfig(symDirCfg, map[JobKey]JobOverride{}); err == nil {
		t.Fatalf("expected SaveConfig to reject symlinked parent directory")
	}

	// 5. Reject FIFO (named pipe) at config path without blocking.
	fifoPath := filepath.Join(dir, "config.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo failed: %v", err)
	}
	if _, _, err := LoadConfig(fifoPath); err == nil {
		t.Fatalf("expected LoadConfig to reject FIFO without blocking")
	}
	if err := SaveConfig(fifoPath, map[JobKey]JobOverride{}); err == nil {
		t.Fatalf("expected SaveConfig to reject FIFO without blocking")
	}

	// 6. Reject directory at config path.
	dirAsFile := filepath.Join(dir, "dir_as_config")
	if err := os.Mkdir(dirAsFile, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if _, _, err := LoadConfig(dirAsFile); err == nil {
		t.Fatalf("expected LoadConfig to reject directory path")
	}
	if err := SaveConfig(dirAsFile, map[JobKey]JobOverride{}); err == nil {
		t.Fatalf("expected SaveConfig to reject directory path")
	}
}
