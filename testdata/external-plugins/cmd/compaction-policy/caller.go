// Package main supplies a public-only reference compaction policy.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// callerReport contains the public Extension manual-operation outcome.
type callerReport struct {
	// Failed reports an SDK failure alongside any retained receipt.
	Failed bool `json:"failed"`
	// Committed reports a durable replacement.
	Committed bool `json:"committed"`
	// Summary contains the supplied replacement.
	Summary string `json:"summary"`
	// Error contains complete failure text.
	Error string `json:"error"`
	// Canceled reports explicit policy cancellation.
	Canceled bool `json:"canceled"`
}

// runCaller serves the ordinary public manual-compaction caller.
func runCaller(ctx context.Context, binding *extensionsdk.ExtensionContext, listener net.Listener) (returnErr error) {
	defer func() { returnErr = errors.Join(returnErr, listener.Close()) }()
	connection, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, connection.Close()) }()
	guidance, err := io.ReadAll(connection)
	report := callerReport{Failed: false, Committed: false, Summary: "", Error: "", Canceled: false}
	if err != nil {
		report.Error = err.Error()
	} else {
		report = compactFromCaller(ctx, binding, string(guidance))
	}
	return json.MarshalWrite(connection, report)
}

// compactFromCaller requests manual replacement through the ordinary public session binding.
func compactFromCaller(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	guidance string,
) (report callerReport) {
	report = callerReport{Failed: false, Committed: false, Summary: "", Error: "", Canceled: false}
	owner := ctx
	finish := func() error { return nil }
	const callerCancellationEnvironment = "GLYPH_MANUAL_CANCEL_SOCKET"
	path := os.Getenv(callerCancellationEnvironment)
	if path != "" {
		listener, listenErr := new(net.ListenConfig).Listen(ctx, fixtureSocketNetwork, path)
		if listenErr != nil {
			report.Error = listenErr.Error()
			return report
		}
		owner, finish = startCallerCancellation(ctx, listener)
	}
	defer report.finishCancellation(finish)
	operation, err := binding.StartCompaction(
		owner,
		extensionpb.CompactRequest_builder{Context: nil, Instructions: new(guidance)}.Build(),
	)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	outcome, err := operation.Wait(ctx)
	report.Committed = outcome.GetCommitted() != nil
	report.Summary = outcome.GetCommitted().GetCompaction().GetSummary()
	report.Error, report.Canceled = outcome.GetError(), outcome.GetCanceled()
	if err != nil {
		report.Failed = true
		report.Error = err.Error()
	}
	return report
}

// finishCancellation joins coordination before the caller report is returned.
func (report *callerReport) finishCancellation(finish func() error) {
	failure := finish()
	if failure == nil {
		return
	}
	if report.Error != "" {
		failure = errors.Join(errors.New(report.Error), failure)
	}
	report.Error = failure.Error()
}
