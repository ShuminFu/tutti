package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

// 补丁 0125：批量口的三态。每个问到的 id 都必须出现在结果里 —— 少一个 key 与
// 「查到了但不活」在调用方那边分不开。
func TestDaemonAPIGeneratedRoutesAgentSessionLivenessReportsEveryRequestedID(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{
		AgentSessionService: stubAgentSessionService{
			sessionLivenessFn: func(_ context.Context, workspaceID string, ids []string) (map[string]agentservice.SessionLivenessEntry, error) {
				if workspaceID != "ws-1" {
					t.Fatalf("workspace = %q, want ws-1", workspaceID)
				}
				if strings.Join(ids, ",") != "sess-live,sess-idle,sess-missing" {
					t.Fatalf("ids = %#v", ids)
				}
				return map[string]agentservice.SessionLivenessEntry{
					"sess-live": {Found: true, RuntimeLive: true, ActiveTurnID: "turn-9"},
					"sess-idle": {Found: true, RuntimeLive: false},
				}, nil
			},
		},
	}))

	recorder := performGeneratedRouteRequest(
		t,
		mux,
		http.MethodGet,
		"/v1/workspaces/ws-1/agent-sessions/liveness?ids=sess-live,sess-idle&ids=sess-missing",
		nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response tuttigenerated.WorkspaceAgentSessionLivenessResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if len(response.Sessions) != 3 {
		t.Fatalf("sessions = %#v, want 3 entries", response.Sessions)
	}
	live := response.Sessions["sess-live"]
	if !live.Found || !live.RuntimeLive || live.ActiveTurnId != "turn-9" {
		t.Fatalf("sess-live = %#v", live)
	}
	idle := response.Sessions["sess-idle"]
	if !idle.Found || idle.RuntimeLive || idle.ActiveTurnId != "" {
		t.Fatalf("sess-idle = %#v", idle)
	}
	// 服务层压根没回这个 id，接口仍要给出 found=false 而不是把 key 丢掉。
	missing, present := response.Sessions["sess-missing"]
	if !present || missing.Found || missing.RuntimeLive {
		t.Fatalf("sess-missing present=%v entry=%#v", present, missing)
	}
}

// 重复 ids 归一成一条：既不重复问服务层，也不在结果里出现两次。
func TestDaemonAPIGeneratedRoutesAgentSessionLivenessDeduplicatesIDs(t *testing.T) {
	mux := http.NewServeMux()
	var observed []string
	RegisterRoutes(mux, NewRoutes(DaemonAPI{
		AgentSessionService: stubAgentSessionService{
			sessionLivenessFn: func(_ context.Context, _ string, ids []string) (map[string]agentservice.SessionLivenessEntry, error) {
				observed = append([]string{}, ids...)
				return map[string]agentservice.SessionLivenessEntry{
					"sess-a": {Found: true, RuntimeLive: true},
					"sess-b": {Found: true},
				}, nil
			},
		},
	}))

	recorder := performGeneratedRouteRequest(
		t,
		mux,
		http.MethodGet,
		"/v1/workspaces/ws-1/agent-sessions/liveness?ids=sess-a,sess-b,sess-a&ids=sess-b&ids=%20sess-a%20",
		nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if strings.Join(observed, ",") != "sess-a,sess-b" {
		t.Fatalf("service saw ids = %#v, want [sess-a sess-b]", observed)
	}
	var response tuttigenerated.WorkspaceAgentSessionLivenessResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if len(response.Sessions) != 2 {
		t.Fatalf("sessions = %#v, want 2 entries", response.Sessions)
	}
}

// 空 ids 与超过上限都必须 400，且一次服务调用都不要发生。
func TestDaemonAPIGeneratedRoutesAgentSessionLivenessRejectsEmptyAndOversizedIDs(t *testing.T) {
	oversized := make([]string, 0, agentservice.SessionLivenessMaxIDs+1)
	for index := 0; index <= agentservice.SessionLivenessMaxIDs; index++ {
		oversized = append(oversized, fmt.Sprintf("sess-%d", index))
	}

	for name, query := range map[string]string{
		"blank ids":     "ids=" + url.QueryEscape("   ,  ,"),
		"over the caps": "ids=" + url.QueryEscape(strings.Join(oversized, ",")),
	} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterRoutes(mux, NewRoutes(DaemonAPI{
				AgentSessionService: stubAgentSessionService{
					sessionLivenessFn: func(_ context.Context, _ string, _ []string) (map[string]agentservice.SessionLivenessEntry, error) {
						t.Fatal("service must not be called for a rejected request")
						return nil, nil
					},
				},
			}))
			recorder := performGeneratedRouteRequest(
				t,
				mux,
				http.MethodGet,
				"/v1/workspaces/ws-1/agent-sessions/liveness?"+query,
				nil,
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

// 恰好等于上限的一批必须放行，证明 400 是「超了」而不是「多了就拒」。
func TestDaemonAPIGeneratedRoutesAgentSessionLivenessAcceptsExactlyMaxIDs(t *testing.T) {
	ids := make([]string, 0, agentservice.SessionLivenessMaxIDs)
	for index := 0; index < agentservice.SessionLivenessMaxIDs; index++ {
		ids = append(ids, fmt.Sprintf("sess-%d", index))
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{
		AgentSessionService: stubAgentSessionService{
			sessionLivenessFn: func(_ context.Context, _ string, requested []string) (map[string]agentservice.SessionLivenessEntry, error) {
				if len(requested) != agentservice.SessionLivenessMaxIDs {
					t.Fatalf("requested %d ids, want %d", len(requested), agentservice.SessionLivenessMaxIDs)
				}
				return map[string]agentservice.SessionLivenessEntry{}, nil
			},
		},
	}))
	recorder := performGeneratedRouteRequest(
		t,
		mux,
		http.MethodGet,
		"/v1/workspaces/ws-1/agent-sessions/liveness?ids="+url.QueryEscape(strings.Join(ids, ",")),
		nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response tuttigenerated.WorkspaceAgentSessionLivenessResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if len(response.Sessions) != agentservice.SessionLivenessMaxIDs {
		t.Fatalf("sessions = %d entries, want %d", len(response.Sessions), agentservice.SessionLivenessMaxIDs)
	}
}

// 单条 GET 的会话对象带上 runtimeLive；顺带证明字面量路由 /liveness 没有被
// /{agentSessionID} 那条通配路由吃掉（两条都命中各自的处理器）。
func TestDaemonAPIGeneratedRoutesAgentSessionDetailCarriesRuntimeLive(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{
		AgentSessionService: stubAgentSessionService{
			getDetailWithProjectionFn: func(
				_ context.Context,
				workspaceID string,
				agentSessionID string,
				_ agentservice.SessionDetailProjection,
			) (agentservice.SessionDetail, error) {
				if workspaceID != "ws-1" || agentSessionID != "sess-1" {
					t.Fatalf("workspace/session = %q/%q", workspaceID, agentSessionID)
				}
				return agentservice.SessionDetail{
					Session: agentservice.Session{
						ID:             "sess-1",
						Kind:           "root",
						Provider:       "claude",
						RailSectionKey: "conversations",
						CreatedAt:      time.UnixMilli(1000),
						RuntimeLive:    true,
					},
					ChildSessions: []agentservice.Session{},
				}, nil
			},
		},
	}))

	recorder := performGeneratedRouteRequest(
		t,
		mux,
		http.MethodGet,
		"/v1/workspaces/ws-1/agent-sessions/sess-1",
		nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	// 先留一份原文：decodeGeneratedRouteResponse 会把 recorder 的 body 读空。
	payload := recorder.Body.String()
	var response tuttigenerated.WorkspaceAgentSessionDetailResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if !response.Session.RuntimeLive {
		t.Fatalf("session runtimeLive = false, want true; body: %s", payload)
	}
	if !strings.Contains(payload, `"runtimeLive":true`) {
		t.Fatalf("runtimeLive missing from wire payload: %s", payload)
	}
}
