import type { DesktopBackendConfig } from "@shared/contracts/ipc";

export const QUERY_BASE_URL_PARAM = "tuttidBaseUrl";
export const QUERY_TOKEN_PARAM = "tuttidToken";

export interface WebBackendConfigSources {
  env: {
    VITE_TUTTID_ACCESS_TOKEN?: string;
    VITE_TUTTID_BASE_URL?: string;
  };
  search: string;
}

export function resolveWebBackendConfigFrom(
  sources: WebBackendConfigSources
): DesktopBackendConfig {
  const params = new URLSearchParams(sources.search);
  const queryBaseUrl = params.get(QUERY_BASE_URL_PARAM)?.trim();
  const queryToken = params.get(QUERY_TOKEN_PARAM)?.trim();

  if (queryBaseUrl && queryToken) {
    return {
      accessToken: queryToken,
      baseUrl: queryBaseUrl
    };
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
