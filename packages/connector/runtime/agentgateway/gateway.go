// Package agentgateway provides a VM-boot-scoped Connector MCP endpoint whose
// session bindings survive replacement of the bundle-owned MCP backend.
package agentgateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	connectormcpserver "github.com/tutti-os/tutti/packages/connector/runtime/mcpserver"
)

const connectorMCPPath = "/mcp/connector"

type Backend interface {
	Binding(workspaceID, agentSessionID string) (connectormcpserver.Binding, error)
	Revoke(workspaceID, agentSessionID string)
	RevokeAll()
}

type Config struct {
	Address string
}

type sessionAuthority struct {
	workspaceID string
	sessionID   string
}

type cachedBackendBinding struct {
	generation uint64
	binding    connectormcpserver.Binding
}

// Gateway owns the stable listener and Agent-facing bearer authority. The
// replaceable backend owns MCP routing and can restart independently.
type Gateway struct {
	listener net.Listener
	http     *http.Server
	baseURL  string

	mu              sync.RWMutex
	backend         Backend
	backendEpoch    string
	generation      uint64
	authorizations  map[string]sessionAuthority
	backendBindings map[string]cachedBackendBinding
}

func Start(config Config) (*Gateway, error) {
	address := strings.TrimSpace(config.Address)
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("connector Agent gateway address must be loopback")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for connector Agent gateway: %w", err)
	}
	gateway := &Gateway{
		listener:        listener,
		baseURL:         "http://" + listener.Addr().String() + connectorMCPPath,
		authorizations:  make(map[string]sessionAuthority),
		backendBindings: make(map[string]cachedBackendBinding),
	}
	gateway.http = &http.Server{Handler: gateway, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = gateway.http.Serve(listener) }()
	return gateway, nil
}

func (gateway *Gateway) SetBackend(epoch string, backend Backend) error {
	if gateway == nil || backend == nil || strings.TrimSpace(epoch) == "" {
		return errors.New("connector Agent gateway backend and epoch are required")
	}
	gateway.mu.Lock()
	previous := gateway.backend
	gateway.backend = backend
	gateway.backendEpoch = strings.TrimSpace(epoch)
	gateway.generation++
	gateway.backendBindings = make(map[string]cachedBackendBinding)
	gateway.mu.Unlock()
	if previous != nil {
		previous.RevokeAll()
	}
	return nil
}

func (gateway *Gateway) ClearBackend(epoch string) {
	if gateway == nil {
		return
	}
	gateway.mu.Lock()
	if strings.TrimSpace(epoch) != "" && gateway.backendEpoch != strings.TrimSpace(epoch) {
		gateway.mu.Unlock()
		return
	}
	previous := gateway.backend
	gateway.backend = nil
	gateway.backendEpoch = ""
	gateway.generation++
	gateway.backendBindings = make(map[string]cachedBackendBinding)
	gateway.mu.Unlock()
	if previous != nil {
		previous.RevokeAll()
	}
}

func (gateway *Gateway) Binding(workspaceID, agentSessionID string) (connectormcpserver.Binding, error) {
	if gateway == nil || gateway.listener == nil {
		return connectormcpserver.Binding{}, errors.New("connector Agent gateway is unavailable")
	}
	workspaceID, agentSessionID = strings.TrimSpace(workspaceID), strings.TrimSpace(agentSessionID)
	if workspaceID == "" || agentSessionID == "" {
		return connectormcpserver.Binding{}, errors.New("connector Agent gateway binding identity is required")
	}
	token, err := randomToken(32)
	if err != nil {
		return connectormcpserver.Binding{}, err
	}
	gateway.mu.Lock()
	gateway.revokeLocked(workspaceID, agentSessionID)
	gateway.authorizations[token] = sessionAuthority{workspaceID: workspaceID, sessionID: agentSessionID}
	gateway.mu.Unlock()
	return connectormcpserver.Binding{Name: "connector", Type: "http", URL: gateway.baseURL,
		Headers: map[string]string{"Authorization": "Bearer " + token}}, nil
}

func (gateway *Gateway) Revoke(workspaceID, agentSessionID string) {
	if gateway == nil {
		return
	}
	workspaceID, agentSessionID = strings.TrimSpace(workspaceID), strings.TrimSpace(agentSessionID)
	gateway.mu.Lock()
	backend := gateway.backend
	gateway.revokeLocked(workspaceID, agentSessionID)
	gateway.mu.Unlock()
	if backend != nil {
		backend.Revoke(workspaceID, agentSessionID)
	}
}

func (gateway *Gateway) RevokeAll() {
	if gateway == nil {
		return
	}
	gateway.mu.Lock()
	backend := gateway.backend
	gateway.authorizations = make(map[string]sessionAuthority)
	gateway.backendBindings = make(map[string]cachedBackendBinding)
	gateway.mu.Unlock()
	if backend != nil {
		backend.RevokeAll()
	}
}

func (gateway *Gateway) Close(ctx context.Context) error {
	if gateway == nil || gateway.http == nil {
		return nil
	}
	gateway.RevokeAll()
	return gateway.http.Shutdown(ctx)
}

func (gateway *Gateway) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != connectorMCPPath || !loopbackHost(request.Host) {
		http.NotFound(writer, request)
		return
	}
	authority, ok := gateway.authorize(request.Header.Get("Authorization"))
	if !ok {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(writer, "connector Agent gateway authentication is required", http.StatusUnauthorized)
		return
	}
	binding, err := gateway.backendBinding(authority)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	target, err := url.Parse(binding.URL)
	if err != nil || target.Scheme != "http" || !loopbackHostname(target.Hostname()) || target.Path != connectorMCPPath {
		http.Error(writer, "connector MCP backend binding is invalid", http.StatusServiceUnavailable)
		return
	}
	proxyTarget := *target
	proxyTarget.Path = ""
	proxyTarget.RawPath = ""
	proxy := httputil.NewSingleHostReverseProxy(&proxyTarget)
	originalDirector := proxy.Director
	proxy.Director = func(outbound *http.Request) {
		originalDirector(outbound)
		outbound.Host = target.Host
		outbound.Header.Del("Authorization")
		if authorization := strings.TrimSpace(binding.Headers["Authorization"]); authorization != "" {
			outbound.Header.Set("Authorization", authorization)
		}
	}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(response, "connector MCP backend is unavailable", http.StatusServiceUnavailable)
	}
	proxy.ServeHTTP(writer, request)
}

func (gateway *Gateway) backendBinding(authority sessionAuthority) (connectormcpserver.Binding, error) {
	key := authority.workspaceID + "\x00" + authority.sessionID
	gateway.mu.RLock()
	backend, generation := gateway.backend, gateway.generation
	if cached, ok := gateway.backendBindings[key]; ok && cached.generation == generation {
		gateway.mu.RUnlock()
		return cached.binding, nil
	}
	gateway.mu.RUnlock()
	if backend == nil {
		return connectormcpserver.Binding{}, errors.New("connector MCP backend is starting")
	}
	binding, err := backend.Binding(authority.workspaceID, authority.sessionID)
	if err != nil {
		return connectormcpserver.Binding{}, err
	}
	gateway.mu.Lock()
	if gateway.backend != backend || gateway.generation != generation {
		gateway.mu.Unlock()
		backend.Revoke(authority.workspaceID, authority.sessionID)
		return connectormcpserver.Binding{}, errors.New("connector MCP backend changed while binding")
	}
	gateway.backendBindings[key] = cachedBackendBinding{generation: generation, binding: binding}
	gateway.mu.Unlock()
	return binding, nil
}

func (gateway *Gateway) authorize(header string) (sessionAuthority, bool) {
	token, ok := strings.CutPrefix(strings.TrimSpace(header), "Bearer ")
	if !ok || token == "" {
		return sessionAuthority{}, false
	}
	gateway.mu.RLock()
	authority, exists := gateway.authorizations[token]
	gateway.mu.RUnlock()
	return authority, exists
}

func (gateway *Gateway) revokeLocked(workspaceID, agentSessionID string) {
	for token, authority := range gateway.authorizations {
		if authority.workspaceID == workspaceID && authority.sessionID == agentSessionID {
			delete(gateway.authorizations, token)
		}
	}
	delete(gateway.backendBindings, workspaceID+"\x00"+agentSessionID)
}

func loopbackHost(value string) bool {
	host, _, err := net.SplitHostPort(value)
	return err == nil && loopbackHostname(host)
}

func loopbackHostname(value string) bool {
	ip := net.ParseIP(strings.Trim(value, "[]"))
	return ip != nil && ip.IsLoopback()
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
