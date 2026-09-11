package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// 补丁 0125：批量存活口的三态。Found 与 RuntimeLive 是两件事 ——
// 「这个工作区不认识这个 id」和「认识但进程已经没了」必须能分开。
func TestServiceSessionLivenessReportsEveryRequestedID(t *testing.T) {
	service := &Service{
		SessionReader: fakeSessionReader{sessions: map[string]PersistedSession{
			"ws-1:sess-known":  {ID: "sess-known", WorkspaceID: "ws-1", ActiveTurnID: "turn-7"},
			"ws-1:sess-closed": {ID: "sess-closed", WorkspaceID: "ws-1"},
		}},
	}

	entries, err := service.SessionLiveness(
		context.Background(),
		"ws-1",
		[]string{"sess-known", "sess-closed", "sess-unknown"},
	)
	if err != nil {
		t.Fatalf("SessionLiveness() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v, want 3", entries)
	}
	if known := entries["sess-known"]; !known.Found || known.ActiveTurnID != "turn-7" {
		t.Fatalf("sess-known = %#v", known)
	}
	if closed := entries["sess-closed"]; !closed.Found || closed.ActiveTurnID != "" {
		t.Fatalf("sess-closed = %#v", closed)
	}
	unknown, present := entries["sess-unknown"]
	if !present || unknown.Found || unknown.RuntimeLive {
		t.Fatalf("sess-unknown present=%v entry=%#v", present, unknown)
	}
	// 没装 Host 时存活判据退回「不活」而不是 panic：会话响应的投影尾巴上挂着
	// 同一条判据，在这条路上炸掉会拖垮所有只造 Service 的调用。
	if known := entries["sess-known"]; known.RuntimeLive {
		t.Fatalf("sess-known runtimeLive = true without an application host: %#v", known)
	}
}

// 逗号写法与重复 ids= 归一成同一份去重清单。
func TestServiceSessionLivenessNormalizesIDs(t *testing.T) {
	got := NormalizeSessionLivenessIDs([]string{"a, b ,a", "", "  ", "b,c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("ids = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ids = %#v, want %#v", got, want)
		}
	}
}

// 空清单与超过上限都是无效参数，不是空结果。
func TestServiceSessionLivenessRejectsEmptyAndOversizedIDs(t *testing.T) {
	service := &Service{SessionReader: fakeSessionReader{sessions: map[string]PersistedSession{}}}

	if _, err := service.SessionLiveness(context.Background(), "ws-1", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty ids error = %v, want ErrInvalidArgument", err)
	}

	oversized := make([]string, 0, SessionLivenessMaxIDs+1)
	for index := 0; index <= SessionLivenessMaxIDs; index++ {
		oversized = append(oversized, fmt.Sprintf("sess-%d", index))
	}
	if _, err := service.SessionLiveness(context.Background(), "ws-1", oversized); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("oversized ids error = %v, want ErrInvalidArgument", err)
	}

	entries, err := service.SessionLiveness(context.Background(), "ws-1", oversized[:SessionLivenessMaxIDs])
	if err != nil {
		t.Fatalf("exactly max ids error = %v", err)
	}
	if len(entries) != SessionLivenessMaxIDs {
		t.Fatalf("entries = %d, want %d", len(entries), SessionLivenessMaxIDs)
	}
}
