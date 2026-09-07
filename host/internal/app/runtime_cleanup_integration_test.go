//go:build integration

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/internal/testsupport/pluginmock"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// runtimeCleanupPIDEnvironment identifies the isolated extension fixture's process receipt.
const runtimeCleanupPIDEnvironment = "GLYPH_RUNTIME_CLEANUP_PID"

// TestRuntimeCleanupExtensionProcess serves a registered extension whose process the parent test controls.
func TestRuntimeCleanupExtensionProcess(t *testing.T) {
	t.Parallel()
	// Arrange the child only when its executable wrapper supplies a receipt path.
	path := os.Getenv(runtimeCleanupPIDEnvironment)
	if path == "" {
		return
	}
	controller := gomock.NewController(t)
	service := pluginmock.NewMockExtensionService(controller)
	registration := pluginmock.NewMockExtensionRegisterOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).DoAndReturn(func(context.Context) (*extensionpb.RegisterResponse, error) {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return nil, err
		}
		return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: nil}.Build(), nil
	})
	registration.EXPECT().Release()
	// Act through the real SDK child-process protocol.
	// Assert registration and process-exit behavior in the parent application test.
	extensionsdk.Serve(service)
}

// TestHeadlessAppReturnsRuntimeReportCleanupFailure verifies actual app cleanup consumes retained runtime errors.
func TestHeadlessAppReturnsRuntimeReportCleanupFailure(t *testing.T) {
	t.Parallel()
	// Arrange a real idle extension, a normal provider response, and failed runtime diagnostic output.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	directory := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "extension.pid")
	script := fmt.Sprintf("#!/bin/sh\n%s=%q exec %q -test.run=^TestRuntimeCleanupExtensionProcess$\n",
		runtimeCleanupPIDEnvironment, pidPath, os.Args[0])
	require.NoError(t, os.WriteFile(filepath.Join(directory, "retention"), []byte(script), 0o755))
	failure := extension.RuntimeFailure{PluginID: "retention", Condition: extension.RuntimeUnavailableProcessExited}
	message, err := failure.Message()
	require.NoError(t, err)
	writeErr := errors.New("headless runtime diagnostic writer failed")
	reported := make(chan struct{})
	recordReport := sync.OnceFunc(func() { close(reported) })
	stderr := NewMockCleanupWriter(gomock.NewController(t))
	stderr.EXPECT().Write(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
		if bytes.Contains(data, []byte(message)) {
			recordReport()
			return 0, writeErr
		}
		return len(data), nil
	}).AnyTimes()
	var requests atomic.Int32
	providerErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		pidData, readErr := os.ReadFile(pidPath)
		if readErr != nil {
			providerErrors <- readErr
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		pid, parseErr := strconv.Atoi(string(pidData))
		if parseErr != nil {
			providerErrors <- parseErr
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		process, findErr := os.FindProcess(pid)
		if findErr != nil {
			providerErrors <- findErr
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		if killErr := process.Kill(); killErr != nil {
			providerErrors <- killErr
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		select {
		case <-reported:
		case <-request.Context().Done():
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"result\",\"choices\":[{\"index\":0,"+
			"\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	paths := testPaths(t, strings.Replace(restartSelectionSettings(), "http://localhost:11434/v1", server.URL, 1))
	var stdout bytes.Buffer

	// Act through the complete headless application and its deferred runtime Close.
	err = runWithPaths(ctx, paths, cli.Command{
		Mode: cli.ModeHeadless, Headless: headless.Command{UserText: "complete request", ExtensionDirectory: directory},
		ExtensionDirectory: directory, UIDirectory: "", UIID: "", SocketPath: "",
	}, &stdout, stderr)

	// Assert fixture work succeeded and the otherwise successful app returns both retained reporting causes.
	select {
	case setupErr := <-providerErrors:
		require.NoError(t, setupErr)
	default:
	}
	require.Equal(t, int32(1), requests.Load())
	require.Contains(t, stdout.String(), "complete")
	require.ErrorIs(t, err, writeErr)
	require.ErrorContains(t, err, message)
}
