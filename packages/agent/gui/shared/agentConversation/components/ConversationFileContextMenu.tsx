import { useCallback, useRef, useState, type JSX, type ReactNode } from "react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
  toast
} from "@tutti-os/ui-system";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { translate } from "../../../i18n/index";

export function ConversationFileContextMenu({
  path,
  children,
  asChild = false,
  onOpen
}: {
  path: string;
  children: ReactNode;
  asChild?: boolean;
  onOpen?: () => void;
}): JSX.Element {
  const agentHostApi = useOptionalAgentHostApi();
  const actionStartedRef = useRef(false);
  const [menuResetKey, setMenuResetKey] = useState(0);
  const platform = String(agentHostApi?.meta?.platform ?? "").toLowerCase();
  const canReveal =
    typeof agentHostApi?.filesystem?.revealInFolder === "function";
  const canOpen =
    typeof agentHostApi?.filesystem?.openPath === "function" || Boolean(onOpen);
  const revealLabel =
    platform === "darwin"
      ? translate("agentHost.agentGui.revealInFinder")
      : platform === "win32" || platform === "windows"
        ? translate("agentHost.agentGui.revealInFileExplorer")
        : translate("agentHost.agentGui.revealInFileManager");

  const runAndClose = useCallback(
    (action: () => Promise<void> | void) => {
      if (actionStartedRef.current) {
        return;
      }
      actionStartedRef.current = true;
      setMenuResetKey((key) => key + 1);
      void Promise.resolve(action())
        .catch((error: unknown) => {
          const message =
            error instanceof Error ? error.message : String(error || "failed");
          if (agentHostApi?.toast?.error) {
            agentHostApi.toast.error(message);
            return;
          }
          toast.error(message);
        })
        .finally(() => {
          actionStartedRef.current = false;
        });
    },
    [agentHostApi]
  );

  const revealAndClose = useCallback(() => {
    runAndClose(async () => {
      if (!agentHostApi?.filesystem?.revealInFolder) {
        throw new Error(translate("agentHost.agentGui.revealInFileManager"));
      }
      const result = await agentHostApi.filesystem.revealInFolder({ path });
      if (!result?.fallbackToDirectory) {
        return;
      }
      const message = translate("agentHost.agentGui.revealFallbackDirectory");
      if (agentHostApi.toast?.info) {
        agentHostApi.toast.info(message);
        return;
      }
      toast.error(message);
    });
  }, [agentHostApi, path, runAndClose]);

  const openAndClose = useCallback(() => {
    runAndClose(async () => {
      if (agentHostApi?.filesystem?.openPath) {
        await agentHostApi.filesystem.openPath({ path });
        return;
      }
      if (!onOpen) {
        throw new Error(translate("agentHost.agentGui.openWithDefaultApp"));
      }
      onOpen();
    });
  }, [agentHostApi, onOpen, path, runAndClose]);

  if (!path.trim() || (!canReveal && !canOpen)) {
    return <>{children}</>;
  }

  return (
    <ContextMenu key={menuResetKey}>
      <ContextMenuTrigger asChild={asChild}>{children}</ContextMenuTrigger>
      <ContextMenuContent>
        {canOpen ? (
          <ContextMenuItem
            onClick={openAndClose}
            onPointerDown={(event) => {
              if (event.button !== 0) {
                return;
              }
              event.preventDefault();
              openAndClose();
            }}
            onSelect={openAndClose}
          >
            {translate("agentHost.agentGui.openWithDefaultApp")}
          </ContextMenuItem>
        ) : null}
        {canReveal ? (
          <ContextMenuItem
            onClick={revealAndClose}
            onPointerDown={(event) => {
              if (event.button !== 0) {
                return;
              }
              event.preventDefault();
              revealAndClose();
            }}
            onSelect={revealAndClose}
          >
            {revealLabel}
          </ContextMenuItem>
        ) : null}
      </ContextMenuContent>
    </ContextMenu>
  );
}
