package project

import (
	"fmt"
	"path/filepath"
	"sync"
)

// pathLocks serializes mutations by canonical absolute path.
type pathLocks struct {
	// mutex protects locks.
	mutex sync.Mutex
	// locks contains active locks by canonical absolute path.
	locks map[string]*pathLock
}

// pathLock tracks one path's mutex and waiting users.
type pathLock struct {
	// mutex serializes mutations for one path.
	mutex sync.Mutex
	// users counts owners and waiters using this lock.
	users int
}

// canonicalPath resolves a clean path to an absolute lock key.
func canonicalPath(path string) (string, error) {
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve project file %q: %w", path, err)
	}
	return absolutePath, nil
}

// lock acquires one path lock and returns its release function.
func (l *pathLocks) lock(path string) (func(), error) {
	absolutePath, err := canonicalPath(path)
	if err != nil {
		return nil, err
	}
	l.mutex.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*pathLock)
	}
	entry := l.locks[absolutePath]
	if entry == nil {
		entry = &pathLock{mutex: sync.Mutex{}, users: 0}
		l.locks[absolutePath] = entry
	}
	entry.users++
	l.mutex.Unlock()
	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		l.mutex.Lock()
		entry.users--
		if entry.users == 0 {
			delete(l.locks, absolutePath)
		}
		l.mutex.Unlock()
	}, nil
}
