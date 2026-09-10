import type { DesktopBackendConfig } from "@shared/contracts/ipc";

export const BOOTSTRAP_NONCE_PARAM = "tuttiBootstrap";
export const BOOTSTRAP_URL_PARAM = "tuttiBootstrapUrl";

export interface WebBackendConfigSources {
  env: {
    VITE_TUTTID_ACCESS_TOKEN?: string;
    VITE_TUTTID_BASE_URL?: string;
  };
  search: string;
  fetchBootstrap?: (endpoint: string, nonce: string) => Promise<unknown>;
}

export async function resolveWebBackendConfigFrom(
  sources: WebBackendConfigSources
): Promise<DesktopBackendConfig> {
  const params = new URLSearchParams(sources.search);
  const nonce = params.get(BOOTSTRAP_NONCE_PARAM)?.trim();
  const endpoint = params.get(BOOTSTRAP_URL_PARAM)?.trim();
  if (nonce && endpoint && sources.fetchBootstrap) {
    const payload = (await sources.fetchBootstrap(endpoint, nonce)) as {
      access_token?: unknown;
      base_url?: unknown;
    };
    const accessToken = readString(payload?.access_token);
    const baseUrl = readLoopbackURL(payload?.base_url);
    if (!accessToken || !baseUrl) {
      throw new Error("managed tutti bootstrap response is invalid");
    }
    return { accessToken, baseUrl };
  }

  return {
    accessToken: readRequiredEnv(
      "VITE_TUTTID_ACCESS_TOKEN",
      sources.env.VITE_TUTTID_ACCESS_TOKEN
    ),
    baseUrl: readRequiredEnv(
      "VITE_TUTTID_BASE_URL",
      sources.env.VITE_TUTTID_BASE_URL
    )
  };
}

function readString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function readLoopbackURL(value: unknown): string | null {
  const raw = readString(value);
  if (!raw) return null;
  try {
    const parsed = new URL(raw);
    if (
      parsed.protocol !== "http:" ||
      (parsed.hostname !== "127.0.0.1" && parsed.hostname !== "[::1]") ||
      !parsed.port ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash
    ) return null;
    return raw.replace(/\/+$/, "");
  } catch {
    return null;
  }
}

function readRequiredEnv(
  name: "VITE_TUTTID_ACCESS_TOKEN" | "VITE_TUTTID_BASE_URL",
  value: string | undefined
): string {
  const trimmed = value?.trim();
  if (!trimmed) {
    throw new Error(`${name} is required for desktop web development.`);
  }
  return trimmed;
}
