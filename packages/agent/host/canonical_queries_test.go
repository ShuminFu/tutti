package agenthost

import (
	"context"
	"errors"
	"reflect"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type canonicalQueryStore struct {
	CanonicalStore
	wantWorkspaceID string
	wantSessionID   string
	wantTurnID      string
	turn            storesqlite.Turn
	err             error
}

func (s canonicalQueryStore) GetTurn(_ context.Context, workspaceID, sessionID, turnID string) (storesqlite.Turn, bool, error) {
	if workspaceID != s.wantWorkspaceID || sessionID != s.wantSessionID || turnID != s.wantTurnID {
		return storesqlite.Turn{}, false, errors.New("unexpected canonical turn key")
	}
	return s.turn, true, s.err
}

func TestGetTurnDelegatesCanonicalQueryWithNormalizedIdentity(t *testing.T) {
	want := storesqlite.Turn{WorkspaceID: "workspace-1", AgentSessionID: "session-1", TurnID: "turn-1"}
	host := New(Config{CanonicalStore: canonicalQueryStore{
		wantWorkspaceID: want.WorkspaceID,
		wantSessionID:   want.AgentSessionID,
		wantTurnID:      want.TurnID,
		turn:            want,
	}})

	got, found, err := host.GetTurn(t.Context(), SessionRef{
		WorkspaceID: " workspace-1 ", AgentSessionID: " session-1 ",
	}, " turn-1 ")
	if err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetTurn() = (%#v, %v, %v), want (%#v, true, nil)", got, found, err, want)
	}
}

func TestGetTurnRejectsIncompleteIdentity(t *testing.T) {
	host := New(Config{CanonicalStore: canonicalQueryStore{}})
	for _, test := range []struct {
		name   string
		ref    SessionRef
		turnID string
	}{
		{name: "workspace", ref: SessionRef{AgentSessionID: "session-1"}, turnID: "turn-1"},
		{name: "session", ref: SessionRef{WorkspaceID: "workspace-1"}, turnID: "turn-1"},
		{name: "turn", ref: SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := host.GetTurn(t.Context(), test.ref, test.turnID); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("GetTurn() error = %v, want %v", err, ErrInvalidArgument)
			}
		})
	}
}
