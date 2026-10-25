// Package main supplies a public-only reference compaction policy.
package main

import (
	"context"
	"encoding/json/v2"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// nestedCompactionReportEnvironment selects compaction from a live selection observer.
	nestedCompactionReportEnvironment = "GLYPH_NESTED_COMPACTION_REPORT"
	// nestedCompactionObserverID identifies the consuming selection observer.
	nestedCompactionObserverID = "nested-compaction-first"
	// nestedCompactionLaterObserverID identifies work that must stop after child failure.
	nestedCompactionLaterObserverID = "nested-compaction-later"
	// nestedCompactionLaterSuffix identifies an incorrectly reached observer.
	nestedCompactionLaterSuffix = ".later"
	// nestedCompactionFailureGuidance selects an independent postcommit policy failure.
	nestedCompactionFailureGuidance = "fail-after-commit"
)

// nestedCompactionObserver consumes a manual child failure from a live selection invocation
// while the operation gate is idle.
type nestedCompactionObserver struct {
	// binding retains ordinary invocation identity without compaction mutation authority.
	binding *extensionsdk.ExtensionContext
	// handlerID identifies the selected observer.
	handlerID string
}

// The interface assertion checks the ordinary observer operation contract.
var _ extensionsdk.HandleOperation = (*nestedCompactionObserver)(nil)

// Run records the typed child outcome and deliberately returns lifecycle success.
func (o *nestedCompactionObserver) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	path := os.Getenv(nestedCompactionReportEnvironment)
	if o.handlerID == nestedCompactionLaterObserverID {
		if err := writePolicyArtifactBytes(path+nestedCompactionLaterSuffix, nil); err != nil {
			return nil, err
		}
	} else {
		// Host cancels the owning invocation on child failure; await its terminal through an independent caller context.
		report := compactFromCaller(context.WithoutCancel(ctx), o.binding, nestedCompactionFailureGuidance)
		encoded, err := json.Marshal(report)
		if err != nil {
			return nil, err
		}
		if writeErr := writePolicyArtifactBytes(path, encoded); writeErr != nil {
			return nil, writeErr
		}
	}
	response := new(extensionpb.HandleResponse)
	response.SetLifecycle(new(extensionpb.LifecycleAction))
	return response, nil
}

// Release satisfies the observer contract because the fixture owns no resources.
func (*nestedCompactionObserver) Release() {}
