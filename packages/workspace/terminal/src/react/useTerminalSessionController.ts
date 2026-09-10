import { useEffect, useMemo } from "react";
import { useExternalStoreSnapshot } from "@tutti-os/ui-react-hooks";
import type { TerminalNodeFeature } from "../core/feature.ts";
import {
  acquireTerminalSessionController,
  type TerminalSessionControllerState
} from "../core/sessionController.ts";

export function useTerminalSessionController(input: {
  feature: TerminalNodeFeature;
  nodeId: string;
  retainLease?: boolean;
  sessionId: string;
}) {
  const controller = useMemo(
    () =>
      acquireTerminalSessionController({
        feature: input.feature,
        nodeId: input.nodeId,
        sessionId: input.sessionId
      }),
    [input.feature, input.nodeId, input.sessionId]
  );

  useEffect(() => {
    if (input.retainLease === false) {
      return;
    }
    controller.retain();
    return () => {
      controller.release();
    };
  }, [controller, input.retainLease]);
  const state = useExternalStoreSnapshot<TerminalSessionControllerState>({
    getSnapshot() {
      return controller.getState();
    },
    subscribe(listener) {
      return controller.subscribe(listener);
    }
  });

  return {
    controller,
    state
  };
}
