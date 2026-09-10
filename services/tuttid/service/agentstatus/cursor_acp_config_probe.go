package agentstatus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

func (s Service) probeCursorACPConfigOptions(ctx context.Context, result ProbeResult, command, env []string, timeout time.Duration) ProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := newInstallExecCommand(probeCtx, command[0], command[1:]...)
	cmd.Env, cmd.WaitDelay = env, defaultProbeWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return standardACPHandshakeFailure(result, err.Error())
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return standardACPHandshakeFailure(result, err.Error())
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return standardACPHandshakeFailure(result, err.Error())
	}
	responses := make(chan standardACPHandshakeResponse, 8)
	readDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			var response standardACPHandshakeResponse
			if json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), &response) == nil {
				responses <- response
			}
		}
		readDone <- scanner.Err()
	}()
	initializeID := newStandardACPHandshakeRequestID()
	err = cursorACPProbeWrite(stdin, map[string]any{"jsonrpc": "2.0", "id": initializeID, "method": "initialize",
		"params": standardACPInitializeParams{ProtocolVersion: standardACPHandshakeProtocolVersion,
			ClientCapabilities: standardACPClientCapabilities{Meta: standardACPClientCapabilitiesMeta{TerminalOutput: true}},
			ClientInfo: standardACPClientInfo{Name: standardACPHandshakeClientName, Version: "0.0.0"}}})
	if err == nil {
		_, err = cursorACPProbeWait(probeCtx, responses, readDone, initializeID, "initialize")
	}
	if err == nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			err = cwdErr
		} else {
			sessionID := newStandardACPHandshakeRequestID()
			err = cursorACPProbeWrite(stdin, map[string]any{"jsonrpc": "2.0", "id": sessionID, "method": "session/new",
				"params": map[string]any{"cwd": cwd, "mcpServers": []any{}}})
			if err == nil {
				var response standardACPHandshakeResponse
				response, err = cursorACPProbeWait(probeCtx, responses, readDone, sessionID, "session/new")
				if err == nil {
					var session struct {
						SessionID string `json:"sessionId"`
						ConfigOptions []map[string]any `json:"configOptions"`
					}
					err = json.Unmarshal(response.Result, &session)
					if err == nil && session.SessionID == "" {
						err = errors.New("Cursor ACP session/new returned empty sessionId")
					}
					result.ConfigOptions = cloneConfigOptions(session.ConfigOptions)
				}
			}
		}
	}
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if err != nil {
		return standardACPHandshakeFailure(result, firstNonBlank(trimProbeOutput(stderr.String()), err.Error()))
	}
	result.Status = ProbeReady
	return result
}

func cursorACPProbeWrite(stdin interface{ Write([]byte) (int, error) }, request any) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	_, err = stdin.Write(append(payload, '\n'))
	return err
}

func cursorACPProbeWait(ctx context.Context, responses <-chan standardACPHandshakeResponse, readDone <-chan error, requestID int, method string) (standardACPHandshakeResponse, error) {
	for {
		select {
		case response := <-responses:
			if !isStandardACPHandshakeResponse(response, requestID) {
				continue
			}
			if response.Error != nil {
				return response, fmt.Errorf("Cursor ACP rejected %s: %s", method, response.Error.Message)
			}
			return response, nil
		case err := <-readDone:
			if err != nil {
				return standardACPHandshakeResponse{}, err
			}
			return standardACPHandshakeResponse{}, fmt.Errorf("Cursor ACP exited before responding to %s", method)
		case <-ctx.Done():
			return standardACPHandshakeResponse{}, fmt.Errorf("Cursor ACP did not respond to %s before timeout", method)
		}
	}
}

func cloneConfigOptions(options []map[string]any) []map[string]any {
	if len(options) == 0 {
		return nil
	}
	raw, _ := json.Marshal(options)
	var cloned []map[string]any
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}
