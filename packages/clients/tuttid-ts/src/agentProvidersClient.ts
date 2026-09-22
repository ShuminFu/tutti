import {
  getAgentProviderComposerOptions,
  getAgentProviderRuntimeCandidates,
  getAgentProviderStatuses,
  probeAgentProvider,
  runAgentProviderAction,
  setAgentProviderRuntimeSelection
} from "./generated/index.ts";
import type { AgentProviderComposerOptionsResponse } from "./generated/types.gen.ts";
import type { Client } from "./generated/client/index.ts";
import { unwrapData } from "./tuttidClientResponse.ts";
import type { TuttidClient } from "./tuttidClientTypes.ts";

type AgentProvidersClient = Pick<
  TuttidClient,
  | "getAgentProviderComposerOptions"
  | "refreshComposerModelList"
  | "getAgentProviderRuntimeCandidates"
  | "getAgentProviderStatuses"
  | "probeAgentProvider"
  | "runAgentProviderAction"
  | "setAgentProviderRuntimeSelection"
>;

export function createAgentProvidersClient(
  client: Client
): AgentProvidersClient {
  return {
    async getAgentProviderComposerOptions(
      provider,
      request = {},
      requestOptions
    ) {
      const response = await getAgentProviderComposerOptions({
        client,
        body: request,
        path: { provider },
        ...requestOptions
      });
      return unwrapData(
        response,
        "Get agent provider composer options request failed."
      );
    },
    async refreshComposerModelList(provider, request = {}, requestOptions) {
      const response = await client.post<AgentProviderComposerOptionsResponse>({
        ...requestOptions,
        body: request,
        path: { provider },
        url: "/v1/agent-providers/{provider}/composer-model-list"
      });
      return unwrapData(
        response,
        "Refresh composer model list request failed."
      ) as AgentProviderComposerOptionsResponse;
    },
    async getAgentProviderStatuses(request = {}) {
      const response = await getAgentProviderStatuses({
        client,
        query: request
      });
      return unwrapData(
        response,
        "Get agent provider statuses request failed."
      );
    },
    async getAgentProviderRuntimeCandidates(provider) {
      const response = await getAgentProviderRuntimeCandidates({
        client,
        path: { provider }
      });
      return unwrapData(
        response,
        "Get agent provider runtime candidates request failed."
      );
    },
    async probeAgentProvider(provider) {
      const response = await probeAgentProvider({ client, path: { provider } });
      return unwrapData(response, "Probe agent provider request failed.");
    },
    async runAgentProviderAction(provider, actionID) {
      const response = await runAgentProviderAction({
        client,
        path: { actionID, provider }
      });
      return unwrapData(response, "Run agent provider action request failed.");
    },
    async setAgentProviderRuntimeSelection(provider, request) {
      const response = await setAgentProviderRuntimeSelection({
        client,
        body: request,
        path: { provider }
      });
      return unwrapData(
        response,
        "Set agent provider runtime selection request failed."
      );
    }
  };
}
