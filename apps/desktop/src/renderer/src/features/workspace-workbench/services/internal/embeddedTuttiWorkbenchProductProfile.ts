import {
  bindDesktopWorkbenchContributionFactory,
  type DesktopWorkbenchContributionContext
} from "./workspaceWorkbenchContributionFactory.ts";
import type { WorkbenchProductProfile } from "./workbenchProductProfile.ts";
import { agentGuiWorkbenchContributionFactory } from "./contributions/agentGuiWorkbenchContributionFactory.ts";

// RnDMaster embeds only the Agent surface. Keeping a separate build-time
// profile lets Rollup omit Files/FilePreview and the other standalone workbench
// component trees instead of merely hiding their nodes at runtime.
export function createTuttiWorkbenchProductProfile(
  context: DesktopWorkbenchContributionContext
): WorkbenchProductProfile {
  return {
    productId: "tutti",
    scopeKind: "workspace",
    capabilityFactories: [
      bindDesktopWorkbenchContributionFactory(
        agentGuiWorkbenchContributionFactory,
        pickDesktopWorkbenchContributionContext(context, [
          "agentProviderStatusService",
          "agentQuickPromptService",
          "agentSessionReplayComposition",
          "agentsService",
          "appCenterService",
          "appI18n",
          "comingSoonAgentProviders",
          "computerUseApi",
          "defaultAgentProvider",
          "dockIcons",
          "dockPreviewCache",
          "eventStreamClient",
          "hostFilesApi",
          "hostWindowApi",
          "i18n",
          "onCapabilitySettingsRequest",
          "platformApi",
          "renderAgentsEmpty",
          "reporterService",
          "richTextAtService",
          "runtimeApi",
          "tuttidClient",
          "workspaceAgentActivityService",
          "workspaceFileManagerService",
          "workspaceFilePreviewSurfaceHost",
          "workspaceId",
          "workspaceUserProjectService"
        ])
      )
    ]
  };
}

function pickDesktopWorkbenchContributionContext<
  TKey extends keyof DesktopWorkbenchContributionContext
>(
  context: DesktopWorkbenchContributionContext,
  keys: readonly TKey[]
): Pick<DesktopWorkbenchContributionContext, TKey> {
  return Object.fromEntries(keys.map((key) => [key, context[key]])) as Pick<
    DesktopWorkbenchContributionContext,
    TKey
  >;
}
