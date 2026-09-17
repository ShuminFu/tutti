package storesqlite

import (
	"context"
	"database/sql"
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
