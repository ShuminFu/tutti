import { v5 as uuidv5 } from "uuid";

// Keep this in sync with cliagent-backend/internal/store.ManagedTuttiSessionID.
const TUTTI_SESSION_NAMESPACE = "6ba7b811-9dad-11d1-80b4-00c04fd430c8";
const TUTTI_SESSION_PREFIX = "rndmaster:tutti-agent-session:";

export function embeddedHostCreatedSessionId(clientSubmitId: string): string {
  return uuidv5(
    `${TUTTI_SESSION_PREFIX}${clientSubmitId.trim()}`,
    TUTTI_SESSION_NAMESPACE
  );
}
