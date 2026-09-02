//go:build integration

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const runtimeInterruptedSessionID = "runtime-interrupted"

// sessionInfoObservation records optional session information fields from process tests.
type sessionInfoObservation struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	WorkingDirectory        string `json:"working_directory"`
	StoragePath             string `json:"storage_path"`
	CreatedTime             string `json:"created_time"`
	UpdateTime              string `json:"update_time"`
	IDPresent               bool   `json:"id_present"`
	NamePresent             bool   `json:"name_present"`
	WorkingDirectoryPresent bool   `json:"working_directory_present"`
	StoragePathPresent      bool   `json:"storage_path_present"`
	CreatedTimePresent      bool   `json:"created_time_present"`
	UpdateTimePresent       bool   `json:"update_time_present"`
}

// runtimeFailureRoundTrip returns one deterministic response and applies its persistence fault.
func runtimeFailureRoundTrip(
	dataDirectory string,
	effectPath string,
	releasePath string,
	requestCount *atomic.Int32,
) (*http.Response, error) {
	requestNumber := requestCount.Add(1)
	storagePath, err := latestRuntimeSessionPath(dataDirectory)
	if err != nil {
		return nil, err
	}
	body := finalResponseSSE
	switch requestNumber {
	case 1:
		if err = os.Chmod(storagePath, 0o400); err != nil {
			return nil, err
		}
	case 2:
		command := "printf tool-effect > " + shellPath(effectPath) +
			"; chmod 0400 " + shellPath(storagePath)
		body = strings.ReplaceAll(toolResponseSSE, "printf tool-ok", command)
	case 3:
		if err = waitRuntimeRelease(releasePath); err != nil {
			return nil, err
		}
		if err = os.Chmod(storagePath, 0o400); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("runtime UI transport received a dependent provider request")
	}
	return runtimeFailureHTTPResponse(body), nil
}

func runtimeFailureHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0,
		TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}
}

func latestRuntimeSessionPath(dataDirectory string) (string, error) {
	var latestPath string
	var latestTime time.Time
	err := filepath.WalkDir(
		filepath.Join(dataDirectory, "sessions"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if latestPath == "" || info.ModTime().After(latestTime) {
				latestPath = path
				latestTime = info.ModTime()
			}
			return nil
		},
	)
	if err != nil {
		return "", err
	}
	if latestPath == "" {
		return "", errors.New("runtime UI transport found no session file")
	}
	return latestPath, nil
}

func shellPath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func waitRuntimeRelease(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var signal [1]byte
	_, err = io.ReadFull(file, signal[:])
	return err
}

// signalRuntimeRelease unblocks one waiting runtime fixture.
func signalRuntimeRelease(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err = file.Write([]byte{'r'}); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeRuntimeInterruptedSession(ctx context.Context, active sessionInfoObservation) error {
	path := filepath.Join(filepath.Dir(active.StoragePath), "runtime-interrupted.jsonl")
	header := fmt.Sprintf(
		`{"type":"session","version":2,"id":%q,"createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
		runtimeInterruptedSessionID,
		active.WorkingDirectory,
	)
	user := `{"type":"entry","entry":{"type":"user","id":"runtime-preceding",` +
		`"parentId":null,"createdAt":"2026-08-27T10:00:01Z",` +
		`"message":{"content":[{"kind":1,"text":"runtime preceding"}]}}}` + "\n"
	if err := os.WriteFile(path, []byte(header+user+`{"type":"entry","entry":{"type":"model"`), 0o600); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "/usr/bin/chflags", "uchg", path)
	return command.Run()
}

func clearImmutable(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "/usr/bin/chflags", "nouchg", path).Run()
}
