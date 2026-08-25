package headless

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// Renderer writes headless model, tool, startup, and terminal output.
type Renderer struct {
	stdout        io.Writer
	stderr        io.Writer
	modelLineOpen bool
}

var _ startup.Reporter = (*Renderer)(nil)

// NewRenderer creates the headless output recipient.
func NewRenderer(stdout, stderr io.Writer) *Renderer {
	return &Renderer{stdout: stdout, stderr: stderr, modelLineOpen: false}
}

// ReportRuntimeFailure renders one classified post-start extension failure.
func (r *Renderer) ReportRuntimeFailure(_ context.Context, failure tool.RuntimeFailure) error {
	message, err := failure.Message()
	if err != nil {
		return fmt.Errorf("format extension runtime failure: %w", err)
	}
	return writePrefixed(r.stderr, "[extension:error] ", message)
}

// DeliverAgent renders one Agent Core lifecycle event synchronously.
func (r *Renderer) DeliverAgent(_ context.Context, event run.Event) error {
	switch event.Type {
	case run.EventTextDelta:
		if event.Content.Kind != model.ContentText && event.Content.Kind != model.ContentRefusal {
			return nil
		}
		writeErr := writeText(r.stdout, event.Content.Text)
		if writeErr == nil && event.Content.Text != "" {
			r.modelLineOpen = true
		}
		return writeErr
	case run.EventMessageEnd:
		if !r.modelLineOpen {
			return nil
		}
		if err := writeText(r.stdout, "\n"); err != nil {
			return err
		}
		r.modelLineOpen = false
		return nil
	case run.EventToolExecutionStart:
		return writePrefixed(r.stderr, "[tool:start] ", event.ToolCall.Name)
	case run.EventToolExecutionUpdate:
		return r.writeProgress(event.Progress)
	case run.EventToolExecutionEnd:
		status := "ok"
		if event.ToolResult.IsError {
			status = "error"
		}
		return writePrefixed(r.stderr, "[tool:end] ", event.ToolCall.Name+": "+status)
	case run.EventToolResult:
		return r.writeToolResult(event.ToolResult.Contents)
	case run.EventAgentStart,
		run.EventTurnStart,
		run.EventMessageStart,
		run.EventContentStart,
		run.EventContentEnd,
		run.EventToolCallStart,
		run.EventToolCallDelta,
		run.EventToolCallEnd,
		run.EventTurnEnd,
		run.EventAgentEnd:
		return nil
	default:
		return fmt.Errorf("render unknown Agent Core event type %d", event.Type)
	}
}

// DeliverSettled receives the Host terminal settlement event without adding output.
func (r *Renderer) DeliverSettled(_ context.Context, _ string) error {
	return nil
}

// ReportIssue writes one isolated extension startup failure.
func (r *Renderer) ReportIssue(_ context.Context, issue toolservice.Issue) error {
	identity := strings.Join(issue.PluginIDs, ",")
	if identity == "" {
		identity = "unknown"
	}
	if issue.Path != "" {
		identity += " (" + issue.Path + ")"
	}
	return writePrefixed(r.stderr, "[extension:error] ", identity+": "+issue.Err.Error())
}

// ReportSummary writes one informational headless startup summary.
func (r *Renderer) ReportSummary(_ context.Context, report toolservice.LoadReport) error {
	if err := writePrefixed(r.stderr, "[info] ", "headless"); err != nil {
		return err
	}
	if len(report.Extensions) == 0 {
		return writePrefixed(r.stderr, "[info] ", "extensions: none")
	}
	for _, extension := range report.Extensions {
		names := lo.Map(extension.Tools, func(descriptor tool.Descriptor, _ int) string {
			return descriptor.Name
		})
		toolsText := "no tools"
		if len(names) > 0 {
			toolsText = strings.Join(names, ", ")
		}
		if err := writePrefixed(r.stderr, "[info] ", "extension "+extension.ID+": "+toolsText); err != nil {
			return err
		}
	}
	return nil
}

// WriteError renders one terminal command error.
func (r *Renderer) WriteError(err error) error {
	return writePrefixed(r.stderr, "[error] ", err.Error())
}

// writeToolResult renders typed terminal tool content without printing image bytes.
func (r *Renderer) writeToolResult(contents []tool.ResultContent) error {
	for _, content := range contents {
		switch content.Kind {
		case tool.ResultContentText:
			if err := writePrefixed(r.stderr, "[tool:result] ", content.Text.OrEmpty()); err != nil {
				return err
			}
		case tool.ResultContentImage:
			message := "image omitted: " + content.Image.OrEmpty().MediaType
			if err := writePrefixed(r.stderr, "[tool:result] ", message); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeProgress maps provider-neutral tool progress to one short headless prefix.
func (r *Renderer) writeProgress(progress tool.Progress) error {
	prefix := "[tool:status] "
	switch progress.Channel {
	case tool.ProgressChannelStdout:
		prefix = "[tool:stdout] "
	case tool.ProgressChannelStderr:
		prefix = "[tool:stderr] "
	case tool.ProgressChannelStatus:
	}
	return writePrefixed(r.stderr, prefix, progress.Content)
}

// writePrefixed writes one human-readable line while preserving existing trailing newlines.
func writePrefixed(writer io.Writer, prefix, content string) error {
	line := prefix + content
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	return writeText(writer, line)
}

// writeText checks both writer errors and incomplete writes.
func writeText(writer io.Writer, content string) error {
	written, err := io.WriteString(writer, content)
	if err != nil {
		return fmt.Errorf("write headless output: %w", err)
	}
	if written != len(content) {
		return fmt.Errorf("write headless output: %w", io.ErrShortWrite)
	}
	return nil
}
