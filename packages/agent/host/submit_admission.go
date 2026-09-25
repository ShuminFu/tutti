package agenthost

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
)

// SubmitKindQueued marks a send the Host accepted but has not dispatched yet:
// the session's one canonical turn slot was occupied, so the prompt waits in
// the admission queue and is replayed when the runtime frees the slot.
const SubmitKindQueued = "queued"

// submitAdmissionMode says whether this send may park when the runtime turn
// slot is busy. Only a caller-originated send parks; a replay out of the queue
// reports "still busy" so the drain loop can keep its own ordering.
type submitAdmissionMode int

const (
	admissionPark submitAdmissionMode = iota
	admissionReplay
	// admissionCodexReplay delivers a prompt already stored in the desktop
	// hold queue. A still-held resume must not append a second copy.
	admissionCodexReplay
)

// submitAdmissionQueueLimit bounds the parked prompts per session. A session
// that keeps collecting prompts it never runs is a bug somewhere else; refuse
// loudly at the boundary instead of growing without limit.
const submitAdmissionQueueLimit = 8

// parkedSubmit is a send that was prepared, rejected by the runtime because
// another turn owned the slot, and kept verbatim for replay. Replaying the
// whole SendInput (rather than a half-finished continuation) is deliberate:
// claim idempotency, provenance, turn recording and observation all live in
// that one function, so the queued path cannot drift from the direct one.
type parkedSubmit struct {
	ref      SessionRef
	input    SendInput
	attempts int
}

// submitAdmissionMaxAttempts bounds how often one parked prompt may fail to be
// admitted before it is given up on. A transient failure (store contention, a
// provider still coming back) must not lose the user's message, and a
// permanently broken one must not wedge the queue behind it.
const submitAdmissionMaxAttempts = 3

type submitAdmissionQueue struct {
	mu        sync.Mutex
	bySession map[string][]parkedSubmit
	// draining/resignalled serialize the drain per session. Two release
	// signals racing on one session would otherwise both read the same head
	// and replay it twice.
	draining    map[string]bool
	resignalled map[string]bool
}

// beginDrain reports whether the caller owns the drain loop for this session.
// A caller that does not own it has recorded its signal, and the owner will
// take another lap.
func (q *submitAdmissionQueue) beginDrain(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.draining[key] {
		if q.resignalled == nil {
			q.resignalled = make(map[string]bool)
		}
		q.resignalled[key] = true
		return false
	}
	if q.draining == nil {
		q.draining = make(map[string]bool)
	}
	q.draining[key] = true
	return true
}

// endDrain releases ownership and reports whether a signal arrived meanwhile.
func (q *submitAdmissionQueue) endDrain(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	again := q.resignalled[key]
	delete(q.resignalled, key)
	if !again {
		delete(q.draining, key)
	}
	return again
}

func (q *submitAdmissionQueue) park(ref SessionRef, input SendInput) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bySession == nil {
		q.bySession = make(map[string][]parkedSubmit)
	}
	key := submitAdmissionKey(ref)
	if len(q.bySession[key]) >= submitAdmissionQueueLimit {
		return ErrSubmitAdmissionQueueFull
	}
	q.bySession[key] = append(q.bySession[key], parkedSubmit{ref: ref, input: input})
	return nil
}

// head returns the oldest parked submit without removing it. The item stays
// enqueued until it has actually been dispatched, so a replay that finds the
// slot busy again simply leaves the queue order untouched.
func (q *submitAdmissionQueue) head(workspaceID, agentSessionID string) (parkedSubmit, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	parked := q.bySession[submitAdmissionKey(SessionRef{WorkspaceID: workspaceID, AgentSessionID: agentSessionID})]
	if len(parked) == 0 {
		return parkedSubmit{}, false
	}
	return parked[0], true
}

// failHead records one failed admission attempt on the head and reports
// whether the prompt has been given up on (and removed).
func (q *submitAdmissionQueue) failHead(ref SessionRef, clientSubmitID string) bool {
	q.mu.Lock()
	key := submitAdmissionKey(ref)
	parked := q.bySession[key]
	if len(parked) == 0 || parked[0].input.ClientSubmitID != clientSubmitID {
		q.mu.Unlock()
		return true
	}
	parked[0].attempts++
	exhausted := parked[0].attempts >= submitAdmissionMaxAttempts
	q.bySession[key] = parked
	q.mu.Unlock()
	if exhausted {
		q.drop(ref, clientSubmitID)
	}
	return exhausted
}

// drop removes the head when it is still the same submit.
func (q *submitAdmissionQueue) drop(ref SessionRef, clientSubmitID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := submitAdmissionKey(ref)
	parked := q.bySession[key]
	if len(parked) == 0 || parked[0].input.ClientSubmitID != clientSubmitID {
		return
	}
	if len(parked) == 1 {
		delete(q.bySession, key)
		return
	}
	q.bySession[key] = parked[1:]
}

func submitAdmissionKey(ref SessionRef) string {
	return strings.TrimSpace(ref.WorkspaceID) + "/" + strings.TrimSpace(ref.AgentSessionID)
}

// ObserveTurnSlotReleased implements the runtime's turn-slot observer. The
// runtime calls it on its own goroutine once a session's canonical turn slot
// is free.
func (h *Host) ObserveTurnSlotReleased(workspaceID string, agentSessionID string) {
	if h == nil {
		return
	}
	h.drainParkedSubmits(context.Background(), workspaceID, agentSessionID)
	// The item we just finished stays at the head until its turn settles.
	// Continue in order now; the lock probe still refuses while Codex desktop holds it.
	_ = h.deliverCodexDesktopHold(context.Background(), SessionRef{
		WorkspaceID: workspaceID, AgentSessionID: agentSessionID,
	}, false)
}

// drainParkedSubmits replays parked prompts one at a time, in arrival order.
//
// It stops as soon as a replay is parked again: that means another turn took
// the slot first, and that turn will produce its own release signal. Nothing
// polls.
func (h *Host) drainParkedSubmits(ctx context.Context, workspaceID string, agentSessionID string) {
	if h == nil {
		return
	}
	key := submitAdmissionKey(SessionRef{WorkspaceID: workspaceID, AgentSessionID: agentSessionID})
	if !h.submitAdmission.beginDrain(key) {
		return
	}
	defer func() {
		for h.submitAdmission.endDrain(key) {
			h.drainParkedSession(ctx, workspaceID, agentSessionID)
		}
	}()
	h.drainParkedSession(ctx, workspaceID, agentSessionID)
}

func (h *Host) drainParkedSession(ctx context.Context, workspaceID string, agentSessionID string) {
	for {
		parked, ok := h.submitAdmission.head(workspaceID, agentSessionID)
		if !ok {
			return
		}
		result, err := h.sendInputAdmitted(ctx, parked.ref, parked.input)
		if err != nil {
			// A session that has gone away (closed, deleted, never existed)
			// can never run this prompt; anything else may be transient, so
			// keep the prompt and let the next release signal retry it. Either
			// way the queue must not wedge behind it forever.
			gone := errors.Is(err, ErrSessionNotFound) ||
				errors.Is(err, ErrDeletedSessionNotFound) ||
				errors.Is(err, ErrInvalidArgument)
			if gone {
				h.submitAdmission.drop(parked.ref, parked.input.ClientSubmitID)
			}
			givenUp := gone || h.submitAdmission.failHead(parked.ref, parked.input.ClientSubmitID)
			slog.WarnContext(ctx, "parked agent prompt could not be admitted",
				"event", "agent_session.submit_admission.failed",
				"workspaceId", workspaceID,
				"agentSessionId", agentSessionID,
				"clientSubmitId", parked.input.ClientSubmitID,
				"givenUp", givenUp,
				"error", err,
			)
			if givenUp {
				continue
			}
			return
		}
		if result.Kind == SubmitKindQueued {
			// Someone else won the slot in between. Leave this prompt at the
			// head and wait for that turn's release signal.
			return
		}
		h.submitAdmission.drop(parked.ref, parked.input.ClientSubmitID)
	}
}

// admitLater is the single place an ordinary prompt turns from "rejected
// because the session is busy" into "accepted, waiting for the slot". Nothing
// durable was written before the runtime refused admission, so parking the
// verbatim input and abandoning the submit claim leaves no half state behind.
func (h *Host) admitLater(
	ref SessionRef,
	input SendInput,
	session ProviderRuntimeSession,
	admission submitAdmissionMode,
) (SendInputResult, error) {
	queued := SendInputResult{
		Session: session,
		Kind:    SubmitKindQueued,
		TurnID:  strings.TrimSpace(input.TurnID),
	}
	if admission == admissionReplay {
		return queued, nil
	}
	if err := h.submitAdmission.park(ref, input); err != nil {
		return SendInputResult{}, err
	}
	// Closes the lost-wakeup window: the active turn can settle between the
	// runtime's refusal and this park, and that release signal would then find
	// an empty queue. One immediate drain attempt costs nothing and the drain
	// is a no-op while the slot is still taken.
	go h.drainParkedSubmits(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
	return queued, nil
}
