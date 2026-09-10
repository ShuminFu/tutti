import assert from "node:assert/strict";
import test from "node:test";
import { resolveWebBackendConfigFrom } from "./resolveWebBackendConfig.ts";

test("bootstrap nonce resolves the connection in memory", async () => {
  let called: [string, string] | null = null;
  const config = await resolveWebBackendConfigFrom({
    search: "?tuttiBootstrap=n1&tuttiBootstrapUrl=%2Ftutti%2Fbootstrap",
    env: {},
    fetchBootstrap: async (endpoint, nonce) => {
      called = [endpoint, nonce];
      return { base_url: "http://127.0.0.1:51234", access_token: "memory-token" };
    }
  });
  assert.deepEqual(called, ["/tutti/bootstrap", "n1"]);
  assert.deepEqual(config, { baseUrl: "http://127.0.0.1:51234", accessToken: "memory-token" });
});

test("rejects a non-loopback bootstrap response", async () => {
  await assert.rejects(
    resolveWebBackendConfigFrom({
      search: "?tuttiBootstrap=n1&tuttiBootstrapUrl=%2Ftutti%2Fbootstrap",
      env: {},
      fetchBootstrap: async () => ({ base_url: "https://evil.example", access_token: "token" })
    }),
    /invalid/
  );
});

test("falls back to build-time env only without a complete bootstrap", async () => {
  const config = await resolveWebBackendConfigFrom({
    search: "",
    env: {
      VITE_TUTTID_BASE_URL: "http://127.0.0.1:18080",
      VITE_TUTTID_ACCESS_TOKEN: "env-token"
    }
  });
  assert.deepEqual(config, { accessToken: "env-token", baseUrl: "http://127.0.0.1:18080" });
});

test("does not accept legacy token query parameters", async () => {
  await assert.rejects(
    resolveWebBackendConfigFrom({
      search: "?tuttidBaseUrl=http%3A%2F%2F127.0.0.1%3A1&tuttidToken=query-token",
      env: {}
    }),
    /is required/
  );
});
