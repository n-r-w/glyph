// Package tracefile appends trace lines of reference extension processes to a file that the test names.
package tracefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// permission is the file mode for owner-only reading and writing.
const permission = 0o600

// Log appends one line at a time to the file that an environment variable names.
type Log struct {
	// environment names the variable that holds the path of the trace file.
	environment string
	// mutex serializes appends.
	mutex sync.Mutex
}

// New creates a log whose file is named by the environment variable.
func New(environment string) *Log {
	return &Log{environment: environment, mutex: sync.Mutex{}}
}

// Append adds one line to the trace file; it does nothing when the environment variable is empty.
func (l *Log) Append(line string) (returnErr error) {
	path := os.Getenv(l.environment)
	if path == "" {
		return nil
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	file, err := root.OpenFile(filepath.Base(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, permission)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	_, err = fmt.Fprintln(file, line)
	return err
}
