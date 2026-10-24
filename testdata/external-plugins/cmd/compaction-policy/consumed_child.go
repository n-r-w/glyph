package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// consumedChildEnvironment enables the consumed-failure tool and supplies its report path.
	consumedChildEnvironment = "GLYPH_REFERENCE_CONSUMED_CHILD"
	// consumedInvalidOutputEnvironment selects policy consumption of participant rejections.
	consumedInvalidOutputEnvironment = "GLYPH_REFERENCE_CONSUME_PARTICIPANT_FAILURE"
	// consumedChildGuidanceEnvironment selects the ordinary tool's nested compaction scenario.
	consumedChildGuidanceEnvironment = "GLYPH_REFERENCE_CONSUMED_GUIDANCE"
	// consumedChildToolName identifies the ordinary nested-compaction caller.
	consumedChildToolName = "reference_consumed_child"
	// consumedChildToolDescription describes the public fixture capability.
	consumedChildToolDescription = "Consumes a nested compaction handler failure"
	// consumedChildToolSchema declares the fixture's explicit failure switch.
	consumedChildToolSchema = `{"type":"object","properties":{"fail":{"type":"boolean"}},` +
		`"required":["fail"],"additionalProperties":false}`
	// consumedChildToolText is the plugin's attempted successful parent result.
	consumedChildToolText = "parent returned success after consuming child failure"
)

// consumedChildOperation retains the selected tool behavior.
type consumedChildOperation struct {
	// fail selects the nested failure rather than an independent successful invocation.
	fail bool
}

var _ extensionsdk.ExecuteOperation = (*consumedChildOperation)(nil)

// consumedChildReport retains both child outcomes visible through the public SDK.
type consumedChildReport struct {
	// Failure contains the first child's complete returned cause.
	Failure string `json:"failure"`
	// Later contains the attempted subsequent admission outcome.
	Later string `json:"later"`
}

// prepareConsumedChild validates the ordinary tool's arguments.
func prepareConsumedChild(request *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
	var input struct {
		Fail bool `json:"fail"`
	}
	if err := json.Unmarshal(request.GetArgumentsJson(), &input); err != nil {
		return nil, err
	}
	return &consumedChildOperation{fail: input.Fail}, nil
}

// Run consumes a nested compaction failure and then attempts further Host work with another context.
func (o *consumedChildOperation) Run(
	ctx context.Context,
	_ *extensionsdk.ProgressReporter,
) (*extensionpb.ToolResult, error) {
	if o.fail {
		if err := consumeCompactionFailure(ctx); err != nil {
			return nil, err
		}
	}
	content := new(extensionpb.ToolResultContent)
	content.SetText(consumedChildToolText)
	return extensionpb.ToolResult_builder{
		IsError:  new(false),
		Contents: []*extensionpb.ToolResultContent{content},
	}.Build(), nil
}

// consumeCompactionFailure records the returned handler failure and attempted later admission.
func consumeCompactionFailure(ctx context.Context) error {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return err
	}
	caller := context.WithoutCancel(ctx)
	guidance := os.Getenv(consumedChildGuidanceEnvironment)
	if guidance == "" {
		guidance = referenceRequestFail
	}
	child, err := binding.StartCompaction(caller, extensionpb.CompactRequest_builder{
		Context: nil, Instructions: new(guidance),
	}.Build())
	if err != nil {
		return err
	}
	_, childErr := child.Wait(caller)
	if childErr == nil && os.Getenv(consumedInvalidOutputEnvironment) == "" {
		return errors.New("nested compaction unexpectedly succeeded")
	}
	later, laterErr := binding.StartGetModels(caller)
	if laterErr == nil {
		_, laterErr = later.Wait(caller)
	}
	report := consumedChildReport{Failure: "", Later: ""}
	if childErr != nil {
		report.Failure = childErr.Error()
	}
	if laterErr != nil {
		report.Later = laterErr.Error()
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return writePolicyArtifactBytes(os.Getenv(consumedChildEnvironment), encoded)
}

// Release requires no cleanup because the operation owns no releasable resources.
func (o *consumedChildOperation) Release() {}

// newConsumedChildTools supplies the ordinary tool only when requested by its integration scenario.
func newConsumedChildTools() []*extensionpb.ToolDescriptor {
	if os.Getenv(consumedChildEnvironment) == "" {
		return nil
	}
	return []*extensionpb.ToolDescriptor{extensionpb.ToolDescriptor_builder{
		Name: new(consumedChildToolName), Description: new(consumedChildToolDescription),
		InputSchemaJson: []byte(consumedChildToolSchema), ConstrainedSampling: nil,
	}.Build()}
}
