package agentruntime

import (
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
)

// The Codex app-server takes the context window as thread config, not as a turn
// parameter, so a thread started without it can never be widened and one started
// with it would keep sizing compaction against 1M after the user dropped back to
// a plain model. Changing the window request therefore has to rebuild the
// session; a plain model swap still switches in place.
func TestCodexAppServerRequiresANewSessionWhenTheContextWindowChanges(t *testing.T) {
	t.Parallel()

	adapter, _, session := startedAppServerAdapter(t)
	bare := "gpt-5.1-codex-mini"
	marked := contextwindow.WithMarker(bare)

	session.Settings = &SessionSettings{Model: bare}
	if !adapter.RequiresNewSessionForSettings(session, SessionSettingsPatch{Model: &marked}) {
		t.Fatal("bare -> 1M did not require a new session")
	}
	// Both directions: leaving the 1M thread behind would keep the wide window.
	session.Settings = &SessionSettings{Model: marked}
	if !adapter.RequiresNewSessionForSettings(session, SessionSettingsPatch{Model: &bare}) {
		t.Fatal("1M -> bare did not require a new session")
	}
	if adapter.RequiresNewSessionForSettings(session, SessionSettingsPatch{Model: &marked}) {
		t.Fatal("re-selecting the same 1M request required a new session")
	}

	// Unrelated settings keep the per-turn override behaviour.
	session.Settings = &SessionSettings{Model: bare}
	other := "gpt-5.2"
	if adapter.RequiresNewSessionForSettings(session, SessionSettingsPatch{Model: &other}) {
		t.Fatal("plain model swap required a new session")
	}
	if adapter.RequiresNewSessionForSettings(session, SessionSettingsPatch{}) {
		t.Fatal("empty patch required a new session")
	}
}
