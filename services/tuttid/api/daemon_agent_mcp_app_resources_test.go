package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
)

type fakeMCPAppResourceReader map[string]storesqlite.MCPAppResourceSnapshot

func (reader fakeMCPAppResourceReader) GetMCPAppResourceSnapshot(_ context.Context, sha string) (storesqlite.MCPAppResourceSnapshot, bool, error) {
	snapshot, ok := reader[sha]
	return snapshot, ok, nil
}

func TestGetWorkspaceAgentMcpAppResourceRoute(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{AgentMCPAppResources: fakeMCPAppResourceReader{
		sha: {
			SHA256:   sha,
			URI:      "ui://workflow_report/widget",
			MimeType: "text/html;profile=mcp-app",
			HTML:     "<!doctype html><html></html>",
			MetaJSON: `{"csp":{"connectDomains":[],"resourceDomains":["https://registry.npmmirror.com"]},"permissions":{"camera":{}},"prefersBorder":false}`,
		},
	}}))

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws/agent-mcp-app-resources/"+strings.ToUpper(sha), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	var response tuttigenerated.WorkspaceAgentMcpAppResourceResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if response.ResourceSha256 != sha || response.Uri != "ui://workflow_report/widget" || response.Html != "<!doctype html><html></html>" || response.MimeType != "text/html;profile=mcp-app" {
		t.Fatalf("response = %#v", response)
	}
	if response.Meta.PrefersBorder == nil || *response.Meta.PrefersBorder ||
		response.Meta.Csp == nil || response.Meta.Csp.ResourceDomains == nil ||
		strings.Join(*response.Meta.Csp.ResourceDomains, ",") != "https://registry.npmmirror.com" ||
		response.Meta.Csp.ConnectDomains == nil || len(*response.Meta.Csp.ConnectDomains) != 0 {
		t.Fatalf("meta = %s", body)
	}
	if strings.Contains(body, "permissions") {
		t.Fatalf("display-only host leaked permissions: %s", body)
	}

	missing := httptest.NewRecorder()
	mux.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws/agent-mcp-app-resources/"+strings.Repeat("0", 64), nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d body = %s", missing.Code, missing.Body.String())
	}
	assertGeneratedRouteError(t, missing, tuttigenerated.WorkspaceNotFound, apierrors.ReasonWorkspaceAgentMCPAppResourceNotFound, "MCP App resource snapshot was not found")

	invalid := httptest.NewRecorder()
	mux.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws/agent-mcp-app-resources/not-a-sha", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body = %s", invalid.Code, invalid.Body.String())
	}

	method := httptest.NewRecorder()
	mux.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws/agent-mcp-app-resources/"+sha, nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", method.Code)
	}
}

func TestGetWorkspaceAgentMcpAppResourceWithoutReaderIsUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{}))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws/agent-mcp-app-resources/"+strings.Repeat("a", 64), nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
