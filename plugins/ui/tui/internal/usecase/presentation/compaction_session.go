package presentation

import (
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"

	"github.com/samber/mo"
)

// compactionSessionStatus records whether a replacement established a new transcript owner.
type compactionSessionStatus uint8

const (
	// compactionSessionPending awaits the replacement outcome.
	compactionSessionPending compactionSessionStatus = iota
	// compactionSessionConfirmed owns a confirmed displayed transcript lifetime.
	compactionSessionConfirmed
	// compactionSessionRejected retains the preceding transcript owner.
	compactionSessionRejected
)

// compactionSession associates compaction with preceding replacement requests, including unresolved requests.
type compactionSession struct {
	// previous owns work when this replacement is rejected.
	previous *compactionSession
	// status determines whether this request changed transcript ownership.
	status compactionSessionStatus
}

// owner resolves rejected replacements without treating an unconfirmed request as a session change.
func (session *compactionSession) owner() *compactionSession {
	for session != nil && session.status == compactionSessionRejected {
		session = session.previous
	}
	if session == nil || session.status == compactionSessionPending {
		return nil
	}
	return session
}

// deferredCompaction retains a receipt and its diagnostics until its preceding replacements settle.
type deferredCompaction struct {
	// session determines which confirmed transcript may receive the receipt.
	session *compactionSession
	// displayed records where the diagnostic was already shown before replacement resolution.
	displayed *compactionSession
	// input preserves the complete terminal payload and independent failure.
	input plugininput.Notification
}

// replacesSession identifies commands that can establish a new displayed transcript lifetime.
func (command Command) replacesSession() bool {
	return command.Kind == CommandCreateSession || command.Kind == CommandResumeSession ||
		command.Kind == CommandForkSession || command.Kind == CommandCloneSession
}

// payloadReplacesSession recognizes confirmation independently from the terminal's success or failure status.
func payloadReplacesSession(payload plugininput.Payload) bool {
	return (payload.Kind == plugininput.PayloadSession && payload.Session.Kind == plugininput.SessionChanged) ||
		(payload.Kind == plugininput.PayloadTree &&
			(payload.Tree.Kind == plugininput.TreeForked || payload.Tree.Kind == plugininput.TreeCloned))
}

// resolveCompactionSession records replacement outcomes without treating delivery acknowledgement as confirmation.
func (service *Service) resolveCompactionSession(pending *pendingCommand, input plugininput.Notification) {
	if input.Kind == plugininput.NotificationProgress || !pending.command.replacesSession() {
		return
	}
	pending.session.status = compactionSessionRejected
	if payload, present := input.Payload.Get(); present && payloadReplacesSession(payload) {
		pending.session.status = compactionSessionConfirmed
		pending.session.previous = nil
		service.displayedSession = pending.session
	}
}

// applyCompactionInput applies diagnostics immediately while restricting receipts to their confirmed owner.
func (service *Service) applyCompactionInput(input plugininput.Notification, session *compactionSession) error {
	payload, present := input.Payload.Get()
	if !present || payload.CommittedCompaction.IsNone() {
		return service.applyInput(input.Payload)
	}
	owner := session.owner()
	if owner == nil {
		service.deferredCompactions = append(service.deferredCompactions, deferredCompaction{
			session: session, displayed: service.displayedSession, input: input,
		})
	}
	if owner != service.displayedSession {
		payload.CommittedCompaction = mo.None[plugininput.TreeEntry]()
	}
	return service.applyInput(mo.Some(payload))
}

// applyDeferredCompactions projects resolved receipts without repeating acknowledgement or settlement.
func (service *Service) applyDeferredCompactions() error {
	waiting := service.deferredCompactions
	service.deferredCompactions = nil
	for index := range waiting {
		delivery := &waiting[index]
		owner := delivery.session.owner()
		if owner == nil {
			service.deferredCompactions = append(service.deferredCompactions, *delivery)
			continue
		}
		payload, _ := delivery.input.Payload.Get()
		if owner == service.displayedSession {
			entry, _ := payload.CommittedCompaction.Get()
			service.model.applyCompactionEntry(decodeTreeEntry(entry))
		}
		if delivery.displayed != service.displayedSession {
			// Replacement cleared this terminal's diagnostics. Receipt replay must not settle newer work.
			if payload.Kind == plugininput.PayloadText {
				payload.CommittedCompaction = mo.None[plugininput.TreeEntry]()
				if err := service.applyInput(mo.Some(payload)); err != nil {
					return err
				}
			}
			service.applyOperationFailure(emptyCommand(CommandCompact), delivery.input)
		}
	}
	return nil
}
