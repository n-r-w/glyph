//go:build integration

package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

// TestAppCollectsUndeliveredRuntimeCompletion crosses the real SDK, runtime, Host owner and app cleanup.
func TestAppCollectsUndeliveredRuntimeCompletion(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{
		"host-rejection-completion", "host-rejection-delivered", "host-rejection-registration",
	} {
		delivered := mode == "host-rejection-delivered"
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange the direct protocol fixture with confirmed output or transport backpressure.
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			directory := t.TempDir()
			binary := filepath.Join(t.TempDir(), "runtime-fixture")
			build := exec.CommandContext(ctx, "go", "test", "-c", "-race", "-tags=integration", "-o", binary,
				"../infra/plugins/extension/runtime")
			buildOutput, err := build.CombinedOutput()
			require.NoError(t, err, "%s", buildOutput)
			script := fmt.Sprintf(
				"#!/bin/sh\nGLYPH_EXTENSION_RUNTIME_HELPER=%s exec %q -test.run=^TestRuntimeHelperProcess$\n",
				mode, binary,
			)
			require.NoError(t, os.WriteFile(filepath.Join(directory, "completion"), []byte(script), 0o755))
			reported := make(chan struct{})
			reportOnce := sync.OnceFunc(func() { close(reported) })
			stderr := NewMockCleanupWriter(gomock.NewController(t))
			stderr.EXPECT().Write(gomock.Any()).DoAndReturn(func(data []byte) (int, error) {
				if bytes.Contains(data, []byte("[extension:error]")) {
					reportOnce()
				}
				return len(data), nil
			}).AnyTimes()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				select {
				case <-reported:
				case <-request.Context().Done():
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(
					writer,
					"data: {\"id\":\"done\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"complete\"},"+
						"\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
				)
			}))
			t.Cleanup(server.Close)
			paths := testPaths(
				t,
				strings.Replace(restartSelectionSettings(), "http://localhost:11434/v1", server.URL, 1),
			)
			var stdout bytes.Buffer

			// Act through the actual app, after its mode-specific runtime notification succeeds.
			err = runWithPaths(ctx, paths, cli.Command{
				Mode:               cli.ModeHeadless,
				Headless:           headless.Command{UserText: "complete request", ExtensionDirectory: directory},
				ExtensionDirectory: directory,
				UIDirectory:        "",
				UIID:               "",
				SocketPath:         "",
			}, &stdout, stderr)

			// Assert ordinary work completes and only undelivered Host sources reach app cleanup.
			require.Contains(t, stdout.String(), "complete")
			if delivered {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "complete extension context reference is required")
			}
		})
	}
}
