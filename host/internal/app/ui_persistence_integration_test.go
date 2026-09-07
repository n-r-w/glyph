//go:build integration

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// persistenceUIBehavior selects source-backed public run failure assertions.
	persistenceUIBehavior = "run-persistence"
	// persistenceProviderCause identifies the complete independent provider diagnostic.
	persistenceProviderCause = "provider request failed: session persistence failed upstream; diagnostic suffix"
)

// UIRunFailureSuite exercises admitted run failures through the real UI client process.
type UIRunFailureSuite struct {
	// Suite supplies assertions and the sequential test lifecycle.
	suite.Suite
}

var _ suite.TestingSuite = (*UIRunFailureSuite)(nil)

// TestUIPersistenceFailureCategories verifies first append, joined later append, and unrelated public failures.
func (testSuite *UIRunFailureSuite) TestUIPersistenceFailureCategories() {
	t := testSuite.T()
	// Arrange a real UI process and a provider failure that also blocks one later history append.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	trace := filepath.Join(t.TempDir(), "storage-path")
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(*http.Request) (*http.Response, error) {
		storage, err := os.ReadFile(trace)
		if err != nil {
			return nil, err
		}
		if len(storage) != 0 {
			if err := os.Chmod(string(storage), 0o400); err != nil {
				return nil, err
			}
		}
		return nil, errors.New(persistenceProviderCause)
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through production UI admission, Core history writes, and public SDK terminal delivery.
	runSummaryControlUI(t, paths, t.TempDir(), trace, persistenceUIBehavior)

	// Assert the child reached all terminal assertions and completed the retained connection.
	receipt, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Equal(t, "verified", string(receipt))
}

// runPersistenceUIFixture verifies complete terminal failures without reconstructing categories from text.
func runPersistenceUIFixture(t *testing.T, ctx context.Context, host *uisdk.Host) error {
	t.Helper()
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	trace := os.Getenv(appUITraceEnvironment)
	for _, scenario := range []string{"first", "joined", "unrelated"} {
		// Arrange a fresh writable session and a durable name entry before the run.
		create := new(uiv1.UIRequest)
		create.SetCreateSession(new(uiv1.CreateSessionCommand))
		created, err := host.Start(ctx, "create-"+scenario, create)
		if err != nil {
			return err
		}
		if _, err := waitUIOperation(ctx, host, "create-"+scenario, created, nil); err != nil {
			return err
		}
		name := new(uiv1.UIRequest)
		name.SetSetSessionName(uiv1.SetSessionNameCommand_builder{Name: new("persistence check")}.Build())
		named, err := host.Start(ctx, "name-"+scenario, name)
		if err != nil {
			return err
		}
		result, err := waitUIOperation(ctx, host, "name-"+scenario, named, nil)
		if err != nil {
			return err
		}
		path := result.GetSessionInformation().GetInfo().GetStoragePath()
		if path == "" {
			return errors.New("named session has no storage path")
		}
		if scenario == "first" {
			if err := os.Chmod(path, 0o400); err != nil {
				return err
			}
		}
		storage := ""
		if scenario == "joined" {
			storage = path
		}
		if err := os.WriteFile(trace, []byte(storage), 0o600); err != nil {
			return err
		}

		// Act with an admitted run and retain its public operation error.
		request := new(uiv1.UIRequest)
		request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("hello")}.Build())
		operation, err := host.Start(ctx, "run-"+scenario, request)
		if err != nil {
			return err
		}
		_, runErr := waitUIOperation(ctx, host, "run-"+scenario, operation, nil)
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}

		// Assert source identity selects the code and every independent source remains readable.
		if runErr == nil {
			return fmt.Errorf("%s completed instead of failing", scenario)
		}
		failure, classified := errors.AsType[*uisdk.FailureError](runErr)
		if !classified {
			return fmt.Errorf("%s expected accepted-run failure: %w", scenario, runErr)
		}
		expectedCode := "PERSISTENCE_UNAVAILABLE"
		if scenario == "unrelated" {
			expectedCode = "INTERNAL"
		}
		if failure.Code() != expectedCode {
			return fmt.Errorf("%s expected %s, got %s: %w", scenario, expectedCode, failure.Code(), failure)
		}
		if scenario != "unrelated" && !strings.Contains(strings.ToLower(failure.Error()), "permission") {
			return fmt.Errorf("%s lost persistence detail: %w", scenario, failure)
		}
		if scenario != "first" && !strings.Contains(failure.Error(), persistenceProviderCause) {
			return fmt.Errorf("%s lost provider detail: %w", scenario, failure)
		}
	}
	if err := os.WriteFile(trace, []byte("verified"), 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}
