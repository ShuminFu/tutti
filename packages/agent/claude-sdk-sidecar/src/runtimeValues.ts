export function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function normalizeTitle(value: string): string {
  return value.replace(/\s+/gu, " ").trim();
}

// Claude Code's generated session name is `aiTitle` (`claude --resume`).
// `customTitle` is a user rename; `summary` is the older fallback. First-user
// transcript text is not a title source.
export function claudeSessionInfoTitle(info: unknown): string {
  if (!info || typeof info !== "object" || Array.isArray(info)) {
    return "";
  }
  const record = info as Record<string, unknown>;
  return normalizeTitle(
    stringValue(record.customTitle) ||
      stringValue(record.aiTitle) ||
      stringValue(record.summary)
  );
}

export function envObject(value: unknown): Record<string, string | undefined> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const result: Record<string, string | undefined> = {};
  for (const [key, item] of Object.entries(value)) {
    if (typeof item === "string") {
      result[key] = item;
    }
  }
  return result;
}

export function booleanValue(value: unknown): boolean {
  return value === true;
}
