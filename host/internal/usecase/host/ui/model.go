package ui

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// Availability identifies whether the Host can accept a user request.
type Availability uint8

const (
	// AvailabilityCheckingAuthentication blocks input during the startup credential check.
	AvailabilityCheckingAuthentication Availability = iota + 1
	// AvailabilityAuthenticating blocks input during browser OAuth.
	AvailabilityAuthenticating
	// AvailabilityAuthenticationFailed permits only explicit authentication retry.
	AvailabilityAuthenticationFailed
	// AvailabilityIdle permits one user request.
	AvailabilityIdle
	// AvailabilityRunning permits stop or quit but rejects another request.
	AvailabilityRunning
)

// Candidate identifies one executable UI plugin candidate.
type Candidate struct {
	// ID identifies the UI plugin.
	ID string
	// Path is the UI plugin executable path.
	Path string
}

// Directory identifies the effective UI catalog directory.
type Directory struct {
	// Path is the effective UI catalog directory path.
	Path string
}

// Discovery contains filesystem observations before UI acceptance.
type Discovery struct {
	// Candidates contains all executable observations in filesystem name order.
	Candidates []Candidate
	// Failures contains complete per-entry filesystem failures.
	Failures []CandidateFailure
	// DirectoryError contains the complete directory-read failure, when present.
	DirectoryError error
}

// CandidateFailure identifies one executable inspection failure.
type CandidateFailure struct {
	// Name is the filesystem entry name used in the inspection diagnostic.
	Name string
	// Err retains the complete filesystem cause.
	Err error
}

// Initialization contains authoritative Host state needed by UI startup output.
type Initialization struct {
	// Availability identifies which user actions the Host accepts.
	Availability Availability
	// Models lists selectable configured models.
	Models []model.Descriptor
	// ModelSelection contains the active model selection.
	ModelSelection mo.Option[model.Selection]
	// SessionInfo identifies the active session initialized before provider work.
	SessionInfo session.Info
}
