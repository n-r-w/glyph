package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// sessionListFrame copies the ordered list so later service changes cannot mutate an in-flight frame.
func sessionListFrame(listed []session.Summary) domainui.Frame {
	return domainui.Frame{
		Kind:                domainui.FrameSessionList,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            append([]session.Summary(nil), listed...),
	}
}

// sessionChangedFrame confirms that active replacement has committed.
func sessionChangedFrame(info session.Info) domainui.Frame {
	return sessionInfoFrame(domainui.FrameSessionChanged, info)
}

// sessionInformationFrame reports active identity without replacing TUI transcript state.
func sessionInformationFrame(info session.Info) domainui.Frame {
	return sessionInfoFrame(domainui.FrameSessionInformation, info)
}

// sessionInfoFrame initializes exactly one information-bearing lifecycle variant.
func sessionInfoFrame(kind domainui.FrameKind, info session.Info) domainui.Frame {
	return domainui.Frame{
		Kind:                kind,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.None[string](),
		RetryAuthentication: mo.None[bool](),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.Some(info),
		Sessions:            nil,
	}
}
