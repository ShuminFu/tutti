package storesqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func changedSessionTokenUsage(previous SessionMetadata, next SessionMetadata) *ProviderTokenUsage {
	nextTokens := sessionUsageTokens(next.Usage)
	if !nextTokens.HasReported() {
		return nil
	}
	if providerTokenUsageEqual(sessionUsageTokens(previous.Usage), nextTokens) {
		return nil
	}
	return cloneProviderTokenUsage(nextTokens)
}

// unstampedSessionTokenUsage returns session tokens that have not yet been
// copied onto any assistant payload. This covers usage arriving before the
// assistant row exists, without restamping a later turn with an earlier
// snapshot.
func unstampedSessionTokenUsage(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	agentSessionID string,
	usage *SessionUsage,
) (*ProviderTokenUsage, error) {
	tokens := sessionUsageTokens(usage)
	if !tokens.HasReported() {
		return nil, nil
	}
	stamped, err := assistantAlreadyHasTokenUsageTx(ctx, tx, workspaceID, agentSessionID, tokens)
	if err != nil || stamped {
		return nil, err
	}
	return cloneProviderTokenUsage(tokens), nil
}

func assistantAlreadyHasTokenUsageTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	agentSessionID string,
	usage *ProviderTokenUsage,
) (bool, error) {
	if !usage.HasReported() {
		return false, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT message.payload_json
FROM workspace_agent_messages AS message
WHERE message.workspace_id = ?
  AND message.agent_session_id = ?
  AND message.deleted_at_unix_ms = 0
  AND message.role = 'assistant'
  AND message.kind = 'text'
  AND json_extract(message.payload_json, '$.usage') IS NOT NULL
`, workspaceID, agentSessionID)
	if err != nil {
		return false, fmt.Errorf("list assistant payload usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, fmt.Errorf("scan assistant payload usage: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if providerTokenUsageEqual(ParseProviderTokenUsage(payload["usage"]), usage) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func sessionUsageTokens(usage *SessionUsage) *ProviderTokenUsage {
	if usage == nil {
		return nil
	}
	if usage.Tokens.HasReported() {
		return usage.Tokens
	}
	return ParseProviderTokenUsage(usage.LastTurn)
}

// attachTokenUsageToLatestAssistantMessageTx writes newly reported tokens onto
// the current turn's latest assistant text message so sessionarchive can lift
// payload_json.usage. Prefer this SQLite field over HTTP lastTurn.
func attachTokenUsageToLatestAssistantMessageTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	agentSessionID string,
	usage *ProviderTokenUsage,
	now int64,
) (Message, bool, error) {
	if !usage.HasReported() {
		return Message{}, false, nil
	}
	messageID, turnID, err := latestAssistantTextMessageIDTx(ctx, tx, workspaceID, agentSessionID)
	if err != nil || messageID == "" {
		return Message{}, false, err
	}
	existing, ok, err := getAgentMessageForUpdate(ctx, tx, workspaceID, agentSessionID, messageID)
	if err != nil || !ok {
		return Message{}, false, err
	}
	if providerTokenUsageEqual(ParseProviderTokenUsage(existing.Payload), usage) {
		return existing, false, nil
	}
	payload := cloneJSONMap(existing.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["usage"] = usage.Map()
	updated, accepted, _, err := (*Store)(nil).upsertAgentMessageTx(
		ctx,
		tx,
		workspaceID,
		agentSessionID,
		MessageUpdate{
			MessageID: messageID,
			TurnID:    turnID,
			Role:      existing.Role,
			Kind:      existing.Kind,
			Status:    existing.Status,
			Payload:   payload,
		},
		now,
		false,
		false,
	)
	if err != nil || !accepted {
		return Message{}, false, err
	}
	return updated, true, nil
}

func latestAssistantTextMessageIDTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	agentSessionID string,
) (string, string, error) {
	row := tx.QueryRowContext(ctx, `
SELECT message.message_id, COALESCE(message.turn_id, '')
FROM workspace_agent_messages AS message
WHERE message.workspace_id = ?
  AND message.agent_session_id = ?
  AND message.deleted_at_unix_ms = 0
  AND message.role = 'assistant'
  AND message.kind = 'text'
  AND message.turn_id = COALESCE(
    (
      SELECT NULLIF(TRIM(session.active_turn_id), '')
      FROM workspace_agent_sessions AS session
      WHERE session.workspace_id = message.workspace_id
        AND session.agent_session_id = message.agent_session_id
    ),
    (
      SELECT turn.turn_id
      FROM workspace_agent_turns AS turn
      WHERE turn.workspace_id = message.workspace_id
        AND turn.agent_session_id = message.agent_session_id
      ORDER BY turn.started_at_unix_ms DESC, turn.turn_id DESC
      LIMIT 1
    )
  )
ORDER BY message.occurred_at_unix_ms DESC, message.message_id DESC
LIMIT 1
`, workspaceID, agentSessionID)
	var messageID string
	var turnID string
	if err := row.Scan(&messageID, &turnID); err != nil {
		if err == sql.ErrNoRows {
			return "", "", nil
		}
		return "", "", fmt.Errorf("read latest assistant text message: %w", err)
	}
	return strings.TrimSpace(messageID), strings.TrimSpace(turnID), nil
}
