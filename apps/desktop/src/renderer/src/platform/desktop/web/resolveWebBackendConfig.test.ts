import assert from "node:assert/strict";
import test from "node:test";
import { resolveWebBackendConfigFrom } from "./resolveWebBackendConfig.ts";

test("resolveWebBackendConfigFrom uses query params when both base URL and token are present", () => {
  const config = resolveWebBackendConfigFrom({
    search: "?tuttidBaseUrl=http%3A%2F%2F127.0.0.1%3A51234&tuttidToken=query-token",
    env: {
      VITE_TUTTID_BASE_URL: "http://127.0.0.1:18080",
      VITE_TUTTID_ACCESS_TOKEN: "env-token"
    }
  });

  assert.deepEqual(config, {
    accessToken: "query-token",
    baseUrl: "http://127.0.0.1:51234"
  });
});

test("resolveWebBackendConfigFrom falls back to build-time env when query params are absent", () => {
  const config = resolveWebBackendConfigFrom({
    search: "",
    env: {
      VITE_TUTTID_BASE_URL: "http://127.0.0.1:18080",
      VITE_TUTTID_ACCESS_TOKEN: "env-token"
    }
  });

  assert.deepEqual(config, {
    accessToken: "env-token",
    baseUrl: "http://127.0.0.1:18080"
  });
});

test("resolveWebBackendConfigFrom ignores an incomplete query (base URL only) and falls back to env", () => {
  const config = resolveWebBackendConfigFrom({
    search: "?tuttidBaseUrl=http%3A%2F%2F127.0.0.1%3A51234",
    env: {
      VITE_TUTTID_BASE_URL: "http://127.0.0.1:18080",
      VITE_TUTTID_ACCESS_TOKEN: "env-token"
    }
  });

  assert.deepEqual(config, {
    accessToken: "env-token",
    baseUrl: "http://127.0.0.1:18080"
  });
});

test("resolveWebBackendConfigFrom ignores an incomplete query (token only) and falls back to env", () => {
  const config = resolveWebBackendConfigFrom({
    search: "?tuttidToken=query-token",
    env: {
      VITE_TUTTID_BASE_URL: "http://127.0.0.1:18080",
      VITE_TUTTID_ACCESS_TOKEN: "env-token"
    }
  });

  assert.deepEqual(config, {
    accessToken: "env-token",
    baseUrl: "http://127.0.0.1:18080"
  });
});

test("resolveWebBackendConfigFrom throws when neither query nor env can supply a complete config", () => {
  assert.throws(
    () =>
      resolveWebBackendConfigFrom({
        search: "?tuttidToken=query-token",
        env: {}
      }),
    /is required for desktop web development/
  );
});
