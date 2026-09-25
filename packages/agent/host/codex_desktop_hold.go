package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// SubmitKindCodexDesktopHeld is a send Dock accepted but did not deliver
	// because the Codex desktop app still owns the thread writer.
	SubmitKindCodexDesktopHeld = "codex_desktop_held"
	// CodexThreadHeldExternallyReason is the stable API and CLI reason.
	CodexThreadHeldExternallyReason = "codex_thread_held_externally"

	codexConfigOverridesEnv = "TUTTI_CODEX_CONFIG_OVERRIDES"
	codexHomeEnv            = "CODEX_HOME"
	codexHoldQueueLimit     = 64
	codexHoldProbeInterval  = 15 * time.Second
)

// CodexDesktopHoldState is the read model for a thread the desktop app owns.
type CodexDesktopHoldState struct {
	Held              bool
	ReasonCode        string
	QueuedCount       int
	Kinds             []string
	ProviderSessionID string
	CodexHome         string
	OpenURL           string
}

type codexHoldRecord struct {
	ProviderSessionID string          `json:"providerSessionId"`
	CodexHome         string          `json:"codexHome"`
	ReasonCode        string          `json:"reasonCode"`
	AutoBlocked       bool            `json:"autoBlocked"`
	Items             []codexHoldItem `json:"items"`
}

type codexHoldItem struct {
	Kind  string    `json:"kind"`
	Input SendInput `json:"input"`
}

type codexDesktopHoldQueue struct {
	mu      sync.Mutex
	dir     string
	loaded  bool
	records map[string]*codexHoldRecord
	started atomic.Bool
}

func (h *Host) codexHoldQueue() *codexDesktopHoldQueue {
	if h == nil {
		return nil
	}
	if h.codexHold == nil {
		h.codexHold = &codexDesktopHoldQueue{records: map[string]*codexHoldRecord{}}
	}
	return h.codexHold
}

func codexHoldKey(workspaceID, agentSessionID string) string {
	return strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(agentSessionID)
}

func sessionUsesUserCodexHome(env []string) bool {
	_, found := environmentValue(env, codexConfigOverridesEnv)
	return found
}

func environmentValue(env []string, key string) (string, bool) {
	prefix := key + "="
	value := ""
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

func codexOutboundKind(metadata map[string]any) string {
	raw, _ := metadata["codexOutboundKind"].(string)
	switch strings.TrimSpace(raw) {
	case "handoff", "review":
		return strings.TrimSpace(raw)
	default:
		return "send"
	}
}

func (h *Host) codexDesktopHoldBlocks(session ProviderRuntimeSession, admission submitAdmissionMode) bool {
	if h == nil || admission == admissionCodexReplay || !sessionUsesUserCodexHome(session.Env) {
		return false
	}
	home, found := environmentValue(session.Env, codexHomeEnv)
	if !found {
		return false
	}
	held, known := codexWriterLockHeld(home, session.ProviderSessionID)
	return known && held
}

func (h *Host) parkCodexDesktopHold(
	ref SessionRef,
	input SendInput,
	session ProviderRuntimeSession,
	admission submitAdmissionMode,
) (SendInputResult, error) {
	queued := SendInputResult{Session: session, Kind: SubmitKindCodexDesktopHeld}
	if admission == admissionCodexReplay {
		return queued, nil
	}
	if err := h.rememberCodexDesktopHold(ref, input, session); err != nil {
		return SendInputResult{}, err
	}
	return queued, nil
}

func (h *Host) rememberCodexDesktopHold(ref SessionRef, input SendInput, session ProviderRuntimeSession) error {
	queue := h.codexHoldQueue()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if err := queue.loadLocked(); err != nil {
		return err
	}
	key := codexHoldKey(ref.WorkspaceID, ref.AgentSessionID)
	record := queue.records[key]
	if record == nil {
		record = &codexHoldRecord{ReasonCode: CodexThreadHeldExternallyReason}
		queue.records[key] = record
	}
	home, _ := environmentValue(session.Env, codexHomeEnv)
	if id := strings.TrimSpace(session.ProviderSessionID); id != "" {
		record.ProviderSessionID = id
	}
	if strings.TrimSpace(home) != "" {
		record.CodexHome = strings.TrimSpace(home)
	}
	record.ReasonCode = CodexThreadHeldExternallyReason
	clientID := strings.TrimSpace(input.ClientSubmitID)
	if clientID != "" {
		for _, item := range record.Items {
			if strings.TrimSpace(item.Input.ClientSubmitID) == clientID {
				return queue.saveLocked(ref.WorkspaceID, ref.AgentSessionID, record)
			}
		}
	}
	if len(record.Items) >= codexHoldQueueLimit {
		return errors.New("codex desktop hold queue is full")
	}
	record.Items = append(record.Items, codexHoldItem{
		Kind:  codexOutboundKind(input.Metadata),
		Input: input,
	})
	return queue.saveLocked(ref.WorkspaceID, ref.AgentSessionID, record)
}

func (h *Host) CodexDesktopHold(workspaceID, agentSessionID string) CodexDesktopHoldState {
	queue := h.codexHoldQueue()
	if queue == nil {
		return CodexDesktopHoldState{}
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	_ = queue.loadLocked()
	record := queue.records[codexHoldKey(workspaceID, agentSessionID)]
	if record == nil || len(record.Items) == 0 {
		return CodexDesktopHoldState{}
	}
	kinds := make([]string, 0, len(record.Items))
	for _, item := range record.Items {
		kinds = append(kinds, item.Kind)
	}
	providerSessionID := strings.TrimSpace(record.ProviderSessionID)
	return CodexDesktopHoldState{
		Held:              true,
		ReasonCode:        firstNonEmpty(record.ReasonCode, CodexThreadHeldExternallyReason),
		QueuedCount:       len(record.Items),
		Kinds:             kinds,
		ProviderSessionID: providerSessionID,
		CodexHome:         strings.TrimSpace(record.CodexHome),
		OpenURL:           codexThreadOpenURL(providerSessionID),
	}
}

func codexThreadOpenURL(providerSessionID string) string {
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" {
		return ""
	}
	return "codex://threads/" + providerSessionID
}

// RetryCodexDesktopHold delivers the parked queue once. The resume result,
// not the lock-file probe, decides whether the desktop app still owns it.
func (h *Host) RetryCodexDesktopHold(ctx context.Context, ref SessionRef) error {
	return h.deliverCodexDesktopHold(ctx, ref, true)
}

// StartCodexDesktopHoldWatcher rechecks held threads infrequently. It probes
// the lock file and only resumes after that probe says the lock is gone.
func (h *Host) StartCodexDesktopHoldWatcher() {
	queue := h.codexHoldQueue()
	if queue == nil || strings.TrimSpace(queue.dir) == "" || queue.started.Swap(true) {
		return
	}
	go queue.watch(h)
}

func (q *codexDesktopHoldQueue) watch(h *Host) {
	ticker := time.NewTicker(codexHoldProbeInterval)
	defer ticker.Stop()
	for range ticker.C {
		h.DeliverReadyCodexDesktopHolds(context.Background())
	}
}

// DeliverReadyCodexDesktopHolds probes parked threads and resumes only
// those whose writer lock is gone.
func (h *Host) DeliverReadyCodexDesktopHolds(ctx context.Context) {
	h.probeAndDeliverCodexHolds(ctx)
}

func (h *Host) probeAndDeliverCodexHolds(ctx context.Context) {
	queue := h.codexHoldQueue()
	if queue == nil {
		return
	}
	queue.mu.Lock()
	_ = queue.loadLocked()
	refs := make([]SessionRef, 0, len(queue.records))
	for key, record := range queue.records {
		if record == nil || len(record.Items) == 0 {
			continue
		}
		workspaceID, agentSessionID, _ := strings.Cut(key, "\x00")
		held, known := codexWriterLockHeld(record.CodexHome, record.ProviderSessionID)
		if record.AutoBlocked {
			if known && held {
				record.AutoBlocked = false
				_ = queue.saveLocked(workspaceID, agentSessionID, record)
			}
			continue
		}
		if !known || held {
			continue
		}
		refs = append(refs, SessionRef{WorkspaceID: workspaceID, AgentSessionID: agentSessionID})
	}
	queue.mu.Unlock()
	for _, ref := range refs {
		_ = h.deliverCodexDesktopHold(ctx, ref, false)
	}
}

// CodexWriterLockHeld reports whether an existing Codex writer lock is taken.
func CodexWriterLockHeld(codexHome, threadID string) (bool, bool) {
	return codexWriterLockHeld(codexHome, threadID)
}

func (h *Host) deliverCodexDesktopHold(ctx context.Context, ref SessionRef, force bool) error {
	queue := h.codexHoldQueue()
	if queue == nil {
		return nil
	}
	for {
		item, ok := h.codexHoldHead(ref)
		if !ok {
			return nil
		}
		if !force {
			queue.mu.Lock()
			record := queue.records[codexHoldKey(ref.WorkspaceID, ref.AgentSessionID)]
			blocked := record != nil && record.AutoBlocked
			home, threadID := "", ""
			if record != nil {
				home, threadID = record.CodexHome, record.ProviderSessionID
			}
			queue.mu.Unlock()
			if blocked {
				return nil
			}
			held, known := codexWriterLockHeld(home, threadID)
			if !known || held {
				return nil
			}
		}
		result, err := h.sendInputWithAdmission(ctx, ref, item.Input, admissionCodexReplay)
		if result.Kind == SubmitKindQueued || codexHoldRetryLater(err) {
			// The previous queued message still owns the turn slot. Leave this
			// one at the head and try again after that turn settles.
			return nil
		}
		if err != nil || result.Kind == SubmitKindCodexDesktopHeld {
			h.blockCodexHoldAutoDelivery(ref)
			if err != nil {
				return err
			}
			return nil
		}
		h.dropCodexHoldHead(ref, item.Input.ClientSubmitID)
		force = false
	}
}

func codexHoldRetryLater(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSessionTurnSlotBusy) {
		return true
	}
	// The canonical store rejects a second turn while the one we just
	// delivered is still live. The queue stays put until that turn settles.
	return strings.Contains(err.Error(), "already has live turn")
}

func (h *Host) codexHoldHead(ref SessionRef) (codexHoldItem, bool) {
	queue := h.codexHoldQueue()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	_ = queue.loadLocked()
	record := queue.records[codexHoldKey(ref.WorkspaceID, ref.AgentSessionID)]
	if record == nil || len(record.Items) == 0 {
		return codexHoldItem{}, false
	}
	return record.Items[0], true
}

func (h *Host) dropCodexHoldHead(ref SessionRef, clientSubmitID string) {
	queue := h.codexHoldQueue()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	record := queue.records[codexHoldKey(ref.WorkspaceID, ref.AgentSessionID)]
	if record == nil || len(record.Items) == 0 {
		return
	}
	head := record.Items[0]
	if clientID := strings.TrimSpace(clientSubmitID); clientID != "" &&
		strings.TrimSpace(head.Input.ClientSubmitID) != clientID {
		return
	}
	record.Items = append([]codexHoldItem(nil), record.Items[1:]...)
	record.AutoBlocked = false
	if len(record.Items) == 0 {
		delete(queue.records, codexHoldKey(ref.WorkspaceID, ref.AgentSessionID))
		_ = queue.removeLocked(ref.WorkspaceID, ref.AgentSessionID)
		return
	}
	_ = queue.saveLocked(ref.WorkspaceID, ref.AgentSessionID, record)
}

func (h *Host) blockCodexHoldAutoDelivery(ref SessionRef) {
	queue := h.codexHoldQueue()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	record := queue.records[codexHoldKey(ref.WorkspaceID, ref.AgentSessionID)]
	if record == nil {
		return
	}
	record.AutoBlocked = true
	_ = queue.saveLocked(ref.WorkspaceID, ref.AgentSessionID, record)
}

func (q *codexDesktopHoldQueue) loadLocked() error {
	if q.records == nil {
		q.records = map[string]*codexHoldRecord{}
	}
	if q.dir == "" || q.loaded {
		return nil
	}
	q.loaded = true
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, workspace := range entries {
		if !workspace.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(q.dir, workspace.Name()))
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			path := filepath.Join(q.dir, workspace.Name(), file.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var stored struct {
				WorkspaceID    string `json:"workspaceId"`
				AgentSessionID string `json:"agentSessionId"`
				codexHoldRecord
			}
			if err := json.Unmarshal(raw, &stored); err != nil {
				return err
			}
			workspaceID := strings.TrimSpace(stored.WorkspaceID)
			agentSessionID := strings.TrimSpace(stored.AgentSessionID)
			if workspaceID == "" || agentSessionID == "" || len(stored.Items) == 0 {
				continue
			}
			record := stored.codexHoldRecord
			q.records[codexHoldKey(workspaceID, agentSessionID)] = &record
		}
	}
	return nil
}

func (q *codexDesktopHoldQueue) saveLocked(workspaceID, agentSessionID string, record *codexHoldRecord) error {
	if q.dir == "" {
		return nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	agentSessionID = strings.TrimSpace(agentSessionID)
	path := codexHoldPath(q.dir, workspaceID, agentSessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		WorkspaceID    string `json:"workspaceId"`
		AgentSessionID string `json:"agentSessionId"`
		codexHoldRecord
	}{
		WorkspaceID:     workspaceID,
		AgentSessionID:  agentSessionID,
		codexHoldRecord: *record,
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (q *codexDesktopHoldQueue) removeLocked(workspaceID, agentSessionID string) error {
	if q.dir == "" {
		return nil
	}
	err := os.Remove(codexHoldPath(q.dir, workspaceID, agentSessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func codexHoldPath(dir, workspaceID, agentSessionID string) string {
	return filepath.Join(dir, sanitizeHoldID(workspaceID), sanitizeHoldID(agentSessionID)+".json")
}

func sanitizeHoldID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
