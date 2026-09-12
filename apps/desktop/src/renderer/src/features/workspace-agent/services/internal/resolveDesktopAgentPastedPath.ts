import type { AgentGUIProps } from "@tutti-os/agent-gui";
import type { TuttidClient } from "@tutti-os/client-tuttid-ts";

type AgentPastedPathResolver = NonNullable<
  AgentGUIProps["workspace"]["resolvePastedPath"]
>;

/**
 * Turns a pasted absolute path into a file/folder reference so the composer can
 * render a folder chip that still carries the underlying path, and so the same
 * path reaches every agent provider instead of a provider-specific attachment.
 *
 * The existence check goes through the daemon (`POST /v1/user-projects/check`)
 * rather than the Electron `platformApi`: that call is a side-effect-free
 * `os.Stat` and is reachable from both the Electron renderer and the web bundle
 * the host embeds, so the composer behaves the same in every runtime.
 *
 * Two couplings to keep in mind when touching this:
 * - The endpoint is named for user projects but is really "stat this path
 *   without recording anything". If `CheckPath` ever grows project-scoped
 *   semantics (must be a directory, must live under a registered project), this
 *   resolver silently degrades to plain text.
 * - Resolution is a round trip, so the mention lands a few milliseconds after
 *   the paste. Anything that reads the composer value in that window still sees
 *   the pre-paste value.
 *
 * Paths the daemon cannot stat, and paths that would render as a nameless chip,
 * resolve to `null`, which the composer renders as plain text.
 */
export function createDesktopAgentPastedPathResolver(input: {
  tuttidClient: Pick<TuttidClient, "checkUserProjectPath">;
}): AgentPastedPathResolver {
  return async (text) => {
    const path = text.trim();
    if (!path.startsWith("/")) {
      return null;
    }
    let check: Awaited<ReturnType<TuttidClient["checkUserProjectPath"]>>;
    try {
      check = await input.tuttidClient.checkUserProjectPath({ path });
    } catch {
      // A failed check must not turn a paste into an error — fall back to text.
      return null;
    }
    if (!check.exists) {
      return null;
    }
    // The daemon returns the canonical path (absolute, symlink-resolved), which
    // is the form the agent should read.
    const resolvedPath =
      typeof check.path === "string" && check.path.trim()
        ? check.path.trim()
        : path;
    if (!lastPathSegment(resolvedPath)) {
      // `/` and `//` name nothing, and the mention name is derived from the last
      // segment — chipping them would insert a nameless, invisible chip.
      return null;
    }
    return {
      hostPath: resolvedPath,
      kind: check.isDirectory ? "folder" : "file",
      path: resolvedPath
    };
  };
}

function lastPathSegment(path: string): string {
  return path.replace(/\/+$/, "").split("/").filter(Boolean).at(-1) ?? "";
}
