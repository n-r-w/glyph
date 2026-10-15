//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"os"

	"google.golang.org/protobuf/encoding/protojson"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// accountingUIReport contains public UI SDK projections after session restoration.
type accountingUIReport struct {
	// Restored contains the completed resume operation.
	Restored string
	// Tree contains the completed tree query.
	Tree string
}

// runAccountingUIFixture restores and exports public accounting projections.
func runAccountingUIFixture(ctx context.Context, host *uisdk.Host, sessionID string) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	const resumeID = "accounting-ui-resume"
	request := new(uipb.UIRequest)
	request.SetResumeSession(uipb.ResumeSessionCommand_builder{SessionId: new(sessionID)}.Build())
	started, err := host.Start(ctx, resumeID, request)
	if err != nil {
		return err
	}
	restored, err := waitUIOperation(ctx, host, resumeID, started, nil)
	if err != nil {
		return err
	}
	const treeID = "accounting-ui-tree"
	request = new(uipb.UIRequest)
	request.SetGetSessionTree(new(uipb.GetSessionTreeCommand))
	started, err = host.Start(ctx, treeID, request)
	if err != nil {
		return err
	}
	tree, err := waitUIOperation(ctx, host, treeID, started, nil)
	if err != nil {
		return err
	}
	restoredJSON, err := protojson.Marshal(restored)
	if err != nil {
		return err
	}
	treeJSON, err := protojson.Marshal(tree)
	if err != nil {
		return err
	}
	report, err := json.Marshal(accountingUIReport{Restored: string(restoredJSON), Tree: string(treeJSON)})
	if err != nil {
		return err
	}
	if err = os.WriteFile(os.Getenv(appUITraceEnvironment), report, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}
