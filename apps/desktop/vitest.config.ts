import { resolve } from "node:path";
import { defineConfig, type Plugin } from "vitest/config";

const root = resolve(".");

function stubStyleImports(): Plugin {
  return {
    name: "stub-style-imports",
    load(id) {
      if (id.endsWith(".css")) {
        return "export default {}";
      }
      return null;
    }
  };
}

export default defineConfig({
  plugins: [stubStyleImports()],
  resolve: {
    alias: {
      "@app/renderer": resolve(root, "../../packages/agent/gui/app/renderer"),
      "@contexts": resolve(root, "../../packages/agent/gui/contexts"),
      "@main": resolve(root, "src/main"),
      "@preload": resolve(root, "src/preload"),
      "@renderer": resolve(root, "src/renderer/src"),
      "@shared/contracts/dto": resolve(
        root,
        "../../packages/agent/gui/shared/contracts/dto"
      ),
      "@shared/errors/appError": resolve(
        root,
        "../../packages/agent/gui/shared/errors/appError.ts"
      ),
      "@shared/featureFlags": resolve(
        root,
        "../../packages/agent/gui/shared/featureFlags"
      ),
      "@shared/types": resolve(root, "../../packages/agent/gui/shared/types"),
      "@shared/utils": resolve(root, "../../packages/agent/gui/shared/utils"),
      "@shared": resolve(root, "src/shared"),
      "@tutti-os/workspace-external-core/contracts": resolve(
        root,
        "../../packages/workspace/external-core/src/contracts/index.ts"
      ),
      "@tutti-os/workspace-external-core/core": resolve(
        root,
        "../../packages/workspace/external-core/src/core/index.ts"
      ),
      "@tutti-os/workspace-external-core/rich-text": resolve(
        root,
        "../../packages/workspace/external-core/src/rich-text/index.ts"
      ),
      "@tutti-os/workspace-external-core": resolve(
        root,
        "../../packages/workspace/external-core/src/index.ts"
      ),
      "@tutti-os/workspace-file-manager/services": resolve(
        root,
        "../../packages/workspace/file-manager/src/services/index.ts"
      ),
      "@tutti-os/workspace-file-manager": resolve(
        root,
        "../../packages/workspace/file-manager/src/index.ts"
      )
    }
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.spec.ts", "src/**/*.spec.tsx"]
  }
});
