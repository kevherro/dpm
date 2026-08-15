// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

// Package operationlock serializes dpm operations within one managed root.
package operationlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const FileName = ".operation.lock"

// Mode controls whether an operation excludes readers or may coexist with them.
type Mode int

const (
	Shared Mode = iota
	Exclusive
)

// Lock is a held root operation lock.
type Lock struct {
	file *os.File
}

// Acquire blocks until the requested root-wide operation lock is held.
func Acquire(root string, mode Mode) (*Lock, error) {
	if root == "" {
		return nil, fmt.Errorf("operation lock root is empty")
	}
	path := filepath.Join(root, FileName)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("operation lock %s is a symlink", path)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("operation lock %s is not a regular file", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect operation lock %s: %w", path, err)
	}

	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	how := syscall.LOCK_SH
	if mode == Exclusive {
		how = syscall.LOCK_EX
	} else if mode != Shared {
		file.Close()
		return nil, fmt.Errorf("invalid operation lock mode %d", mode)
	}
	for {
		err = syscall.Flock(fd, how)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("acquire operation lock %s: %w", path, err)
	}

	return &Lock{file: file}, nil
}

// Close releases the operation lock.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	path := l.file.Name()
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("release operation lock %s: %w", path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close operation lock %s: %w", path, closeErr)
	}

	return nil
}
