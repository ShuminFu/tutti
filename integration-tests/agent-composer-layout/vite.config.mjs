// Run from apps/desktop so its existing aliases and plugins stay authoritative.
import { fileURLToPath } from "node:url";
import desktopConfig from "../../apps/desktop/vite.web.config.mjs";
const repo = fileURLToPath(new URL("../../", import.meta.url));
export default {
  ...desktopConfig,
  root: fileURLToPath(new URL(".", import.meta.url)),
  resolve: {
    ...desktopConfig.resolve,
    alias: {
      ...desktopConfig.resolve.alias,
      "react-dom": `${repo}packages/agent/gui/node_modules/react-dom`,
      react: `${repo}packages/agent/gui/node_modules/react`,
      "@tutti-os/workspace-file-manager/assets/workspace-archive-fallback.png": `${repo}packages/workspace/file-manager/src/runtime-assets/workspace-archive-fallback-url.ts`,
      "@tutti-os/workspace-file-manager/assets/workspace-folder-fallback.png": `${repo}packages/workspace/file-manager/src/runtime-assets/workspace-folder-fallback-url.ts`
    }
  },
  server: {
    host: "127.0.0.1",
    port: 15324,
    strictPort: true,
    fs: { allow: [repo] }
  }
};
