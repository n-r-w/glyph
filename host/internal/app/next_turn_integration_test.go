//go:build integration

package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicNextTurnAfterSettlement checks idle acceptance and branch or incarnation changes through public commands.
func TestPublicNextTurnAfterSettlement(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"same", "navigation", "create", "resume", "replace-resume", "fork", "clone"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			// Arrange a real extension that sends nextTurn from its settled observer.
			provider := startQueueProvider(
				t,
				0,
				strings.ReplaceAll(extensionQueueSSE, "queue-messages", "queue-on-settled"),
				finalResponseSSE,
				finalResponseSSE,
			)
			fixture := startQueueFixture(t, provider, buildPublicExtensionFixture(t))
			defer fixture.closeOwner(t)
			require.NoError(t, fixture.stream.Send(userRequest("arm", "first request")))
			require.True(t, waitForTerminalEvent(t, fixture, "arm").HasCompleted())
			entries := sendProgrammaticOperation(t, fixture, "before", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(request).SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
			}).GetSessionEntries().GetEntries()
			require.NotEmpty(t, entries)
			for _, entry := range entries {
				require.Nil(t, entry.GetExtensionMessage(), "idle acceptance committed an attachment")
			}
			require.Len(t, provider.snapshot(), 2, "idle acceptance started a run")
			info := sendProgrammaticOperation(t, fixture, "identity", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(request).SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
			}).GetSessionInfo().GetInfo()
			var userID string
			for _, entry := range entries {
				if entry.GetUser() != nil {
					userID = entry.GetId()
					break
				}
			}
			require.NotEmpty(t, userID)

			// Act through public navigation or replacement and then the next user message.
			if change == "replace-resume" {
				sendProgrammaticOperation(t, fixture, "replace", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetCreateSession(new(programmaticv1.CreateSession))
				})
			}
			if change != "same" {
				sendProgrammaticOperation(t, fixture, "change", func(request *programmaticv1.OpenRequest) {
					payload := programmaticRequest(request)
					switch change {
					case "navigation":
						payload.SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
							TargetEntryId: new(
								userID,
							),
							SummaryMode: new(programmaticv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
							CustomFocus: nil,
						}.Build())
					case "create":
						payload.SetCreateSession(new(programmaticv1.CreateSession))
					case "resume", "replace-resume":
						payload.SetResumeSession(
							programmaticv1.ResumeSession_builder{SessionId: new(info.GetId())}.Build(),
						)
					case "fork":
						payload.SetForkSession(programmaticv1.ForkSession_builder{TargetEntryId: new(userID)}.Build())
					case "clone":
						payload.SetCloneSession(new(programmaticv1.CloneSession))
					}
				})
			}
			require.NoError(t, fixture.stream.Send(userRequest("next-user", "second request")))
			require.True(t, waitForTerminalEvent(t, fixture, "next-user").HasCompleted())

			// Assert only the same incarnation delivers the exact attachment before the new input.
			bodies := provider.snapshot()
			require.Len(t, bodies, 3)
			if change == "same" || change == "navigation" {
				require.Contains(t, bodies[2], `extension queued nextTurn exact\ntext`)
				require.Less(
					t,
					strings.Index(bodies[2], "extension queued nextTurn"),
					strings.Index(bodies[2], "second request"),
				)
				stored := sendProgrammaticOperation(t, fixture, "after", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
				}).GetSessionEntries().GetEntries()
				var messages []*programmaticv1.ExtensionMessage
				for _, entry := range stored {
					if message := entry.GetExtensionMessage(); message != nil {
						messages = append(messages, message)
					}
				}
				require.Len(t, messages, 1)
				require.Equal(t, extensionNextTurnText, messages[0].GetText())
				require.Equal(t, "external", messages[0].GetExtensionId())
				require.Equal(t, "queued-message", messages[0].GetEntryType())
				require.Equal(t, programmaticv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE, messages[0].GetVisibility())
			} else {
				require.NotContains(t, bodies[2], "extension queued nextTurn")
			}
		})
	}
}
