package claudesidecar

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// TestDriverEnv enables the deterministic test driver; the daemon forwards it
// when spawning the sidecar.
const TestDriverEnv = "TUTTI_CLAUDE_SDK_SIDECAR_TEST_DRIVER"

// Server owns the sidecar's request loop and session registry, the way
// main.ts owns the stdio server and request routing.
type Server struct {
	sink     *EventSink
	sessions map[string]*SessionRuntime
	// testDriver forces the deterministic driver regardless of environment.
	testDriver bool
	// queryFactory overrides query creation for tests.
	queryFactory             QueryFactory
	continuationStartTimeout time.Duration
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithQueryFactory overrides how sessions create queries.
func WithQueryFactory(factory QueryFactory) ServerOption {
	return func(s *Server) { s.queryFactory = factory }
}

// WithTestDriver forces the deterministic test driver.
func WithTestDriver() ServerOption {
	return func(s *Server) { s.testDriver = true }
}

// WithContinuationStartTimeout overrides the synthetic continuation timeout.
func WithContinuationStartTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) { s.continuationStartTimeout = timeout }
}

func NewServer(output io.Writer, options ...ServerOption) *Server {
	server := &Server{
		sink:     NewEventSink(output),
		sessions: map[string]*SessionRuntime{},
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// Serve reads NDJSON requests until EOF, handling each sequentially the way
// the TypeScript sidecar awaits handleRequest per line.
func (s *Server) Serve(input io.Reader) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 256*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		request, err := ParseRequest(line)
		if err != nil {
			s.sink.Emit("error", "", map[string]any{"error": err.Error()})
			continue
		}
		s.HandleRequest(request)
	}
	return scanner.Err()
}

// HandleRequest routes one request; failures become error events carrying the
// request id.
func (s *Server) HandleRequest(request Request) {
	if err := s.handleRequest(request); err != nil {
		s.sink.Emit("error", request.ID, map[string]any{"error": err.Error()})
	}
}

func (s *Server) handleRequest(request Request) error {
	payload := request.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	switch request.Type {
	case "start":
		return s.handleStart(request.ID, payload)
	case "exec":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		var goal *GoalCommandDispatch
		if stringValue(payload["goalOperationId"]) != "" &&
			numberValue(payload["goalRevision"]) > 0 &&
			(stringValue(payload["goalAction"]) == "set" || stringValue(payload["goalAction"]) == "clear") {
			goal = &GoalCommandDispatch{
				OperationID: stringValue(payload["goalOperationId"]),
				Revision:    numberValue(payload["goalRevision"]),
				RepairEpoch: numberValue(payload["goalRepairEpoch"]),
				Action:      stringValue(payload["goalAction"]),
			}
		}
		// Prefer structured content; prompt is the legacy text fallback.
		session.Exec(
			stringValue(payload["turnId"]),
			stringValue(payload["prompt"]),
			payload["content"],
			stringValue(payload["turnOrigin"]),
			goal,
		)
		s.sink.Emit("ok", request.ID, nil)
		return nil
	case "guide":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		// Prefer structured content; prompt is the legacy text fallback.
		session.Guide(stringValue(payload["prompt"]), payload["content"])
		s.sink.Emit("ok", request.ID, nil)
		return nil
	case "cancel":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		canceled, cancelErr := session.Cancel(stringValue(payload["turnId"]))
		if cancelErr != nil {
			return cancelErr
		}
		s.sink.Emit("ok", request.ID, map[string]any{"canceled": canceled})
		return nil
	case "stop_task":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		parentToolUseID := firstNonEmptyText(
			stringValue(payload["toolCallId"]),
			stringValue(payload["parentToolUseId"]),
		)
		stopped, stopErr := session.StopTask(stringValue(payload["taskId"]), parentToolUseID)
		if stopErr != nil {
			return stopErr
		}
		s.sink.Emit("ok", request.ID, map[string]any{"stopped": stopped})
		return nil
	case "submit_interactive":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		result := session.SubmitInteractive(
			stringValue(payload["turnId"]),
			stringValue(payload["requestId"]),
			stringValue(payload["action"]),
			stringValue(payload["optionId"]),
			recordOrEmpty(payload["payload"]),
		)
		s.sink.Emit("ok", request.ID, result)
		return nil
	case "interactive_disposition":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		result := session.InteractiveDisposition(
			stringValue(payload["turnId"]),
			stringValue(payload["requestId"]),
			stringValue(payload["action"]),
			stringValue(payload["optionId"]),
			recordOrEmpty(payload["payload"]),
		)
		s.sink.Emit("ok", request.ID, result)
		return nil
	case "apply_settings":
		session, err := s.requireSession(payload)
		if err != nil {
			return err
		}
		if applyErr := session.ApplySettings(payload); applyErr != nil {
			return applyErr
		}
		s.sink.Emit("ok", request.ID, nil)
		return nil
	case "close":
		agentSessionID := stringValue(payload["agentSessionId"])
		session := s.sessions[agentSessionID]
		if session != nil {
			if closeErr := session.Close(); closeErr != nil {
				return closeErr
			}
		}
		delete(s.sessions, agentSessionID)
		s.sink.Emit("ok", request.ID, nil)
		return nil
	default:
		return fmt.Errorf("unsupported request type %s", request.Type)
	}
}

func (s *Server) handleStart(requestID string, payload map[string]any) error {
	agentSessionID := stringValue(payload["agentSessionId"])
	providerSessionID := stringValue(payload["providerSessionId"])
	if providerSessionID == "" {
		providerSessionID = randomUUID()
	}
	testDriver := s.testDriver || os.Getenv(TestDriverEnv) == "1"
	session := NewSessionRuntime(SessionRuntimeConfig{
		ProviderSessionID:        providerSessionID,
		CWD:                      stringValue(payload["cwd"]),
		Env:                      envObject(payload["env"]),
		Restore:                  booleanValue(payload["restore"]),
		TestDriver:               testDriver,
		Settings:                 sidecarSessionSettings(payload),
		ClaudeOptions:            sidecarClaudeOptionsFromPayload(payload),
		ResumeCursor:             recordOrEmpty(payload["resumeCursor"]),
		Emit:                     s.sink.Emit,
		QueryFactory:             s.queryFactory,
		ContinuationStartTimeout: s.continuationStartTimeout,
	})
	s.sessions[agentSessionID] = session
	if err := session.Start(); err != nil {
		return err
	}
	s.sink.Emit("ok", requestID, map[string]any{
		"providerSessionId": session.ProviderSessionID(),
	})
	return nil
}

func (s *Server) requireSession(payload map[string]any) (*SessionRuntime, error) {
	agentSessionID := stringValue(payload["agentSessionId"])
	session := s.sessions[agentSessionID]
	if session == nil {
		return nil, fmt.Errorf("session %s is not started", agentSessionID)
	}
	return session, nil
}

func recordOrEmpty(value any) map[string]any {
	if record := recordValue(value); record != nil {
		return record
	}
	return map[string]any{}
}
