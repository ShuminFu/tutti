package agentgateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	connectormcpserver "github.com/tutti-os/tutti/packages/connector/runtime/mcpserver"
)

type backendStub struct {
	server *httptest.Server
	token  string

	mu         sync.Mutex
	revokeAlls int
}

func newBackendStub(t *testing.T, token, response string) *backendStub {
	t.Helper()
	backend := &backendStub{token: token}
	backend.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != connectorMCPPath {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(writer, "invalid backend authorization", http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(response))
	}))
	t.Cleanup(backend.server.Close)
	return backend
}

func (backend *backendStub) Binding(string, string) (connectormcpserver.Binding, error) {
	return connectormcpserver.Binding{Name: "connector", Type: "http",
		URL:     backend.server.URL + connectorMCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + backend.token}}, nil
}
func (*backendStub) Revoke(string, string) {}
func (backend *backendStub) RevokeAll() {
	backend.mu.Lock()
	backend.revokeAlls++
	backend.mu.Unlock()
}

func TestGatewayKeepsAgentBindingAcrossBackendReplacement(t *testing.T) {
	gateway, err := Start(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gateway.Close(context.Background()) })
	first := newBackendStub(t, "backend-one", "one")
	if err := gateway.SetBackend("runtime-1", first); err != nil {
		t.Fatal(err)
	}
	binding, err := gateway.Binding("workspace-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := callGateway(t, binding); got != "one" {
		t.Fatalf("first response = %q", got)
	}
	second := newBackendStub(t, "backend-two", "two")
	if err := gateway.SetBackend("runtime-2", second); err != nil {
		t.Fatal(err)
	}
	if got := callGateway(t, binding); got != "two" {
		t.Fatalf("replacement response = %q", got)
	}
	first.mu.Lock()
	revoked := first.revokeAlls
	first.mu.Unlock()
	if revoked != 1 {
		t.Fatalf("previous backend revoke-all count = %d", revoked)
	}
}

func callGateway(t *testing.T, binding connectormcpserver.Binding) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, binding.URL, strings.NewReader(`{"jsonrpc":"2.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", binding.Headers["Authorization"])
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d body=%q", response.StatusCode, body)
	}
	return string(body)
}
