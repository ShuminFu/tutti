// 0076 打开指定 agentSessionId 的注册口。宿主 createAgentSession 成功后复用同一条
// activateEmbeddedDintalDockSession 路径，避免 adapter 直接依赖 workbench host。

type EmbeddedHostCreatedSessionOpener = (agentSessionId: string) => boolean;

let embeddedHostCreatedSessionOpener: EmbeddedHostCreatedSessionOpener | null =
  null;

export function registerEmbeddedHostCreatedSessionOpener(
  opener: EmbeddedHostCreatedSessionOpener
): () => void {
  embeddedHostCreatedSessionOpener = opener;
  return () => {
    if (embeddedHostCreatedSessionOpener === opener) {
      embeddedHostCreatedSessionOpener = null;
    }
  };
}

export function openEmbeddedHostCreatedAgentSession(
  agentSessionId: string
): boolean {
  return embeddedHostCreatedSessionOpener?.(agentSessionId) === true;
}
