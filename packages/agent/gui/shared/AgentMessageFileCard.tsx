import { useEffect, useState, type JSX } from "react";
import { Button, toast } from "@tutti-os/ui-system";
import {
  CloudDownload,
  FileArchive,
  FileText,
  FileImage,
  FileAudio,
  FileVideo,
  FolderOpen
} from "lucide-react";
import { useOptionalAgentHostApi } from "../agentActivityHost";
import { useTranslation } from "../i18n/index";
import { ConversationFileContextMenu } from "./agentConversation/components/ConversationFileContextMenu";

export function AgentMessageFileCard({
  path,
  name,
  onOpen
}: {
  path: string;
  name: string;
  onOpen?: (path: string) => void;
}): JSX.Element {
  const host = useOptionalAgentHostApi();
  const { t } = useTranslation();
  const [size, setSize] = useState<number | null>(null);
  const [pending, setPending] = useState(false);
  const getFileInfo = host?.filesystem?.getFileInfo;
  useEffect(() => {
    let cancelled = false;
    setSize(null);
    void getFileInfo?.({ path })
      .then((info) => {
        if (!cancelled) setSize(info?.sizeBytes ?? null);
      })
      .catch(() => {
        /* Missing metadata must not hide a delivered file. */
      });
    return () => {
      cancelled = true;
    };
  }, [getFileInfo, path]);
  const reveal = host?.filesystem?.revealInFolder;
  const readFile = host?.workspace?.readFile;
  const downloadFile = host?.filesystem?.downloadFile;
  const revealLabel = t(
    host?.meta?.platform === "darwin"
      ? "agentHost.agentGui.revealInFinder"
      : "agentHost.agentGui.revealInFileManager"
  );
  const downloadLabel = t("agentHost.workspaceFileManager.downloadFile");
  const run = async (action: () => Promise<void>) => {
    if (pending) return;
    setPending(true);
    try {
      await action();
    } catch (error) {
      (host?.toast?.error ?? toast.error)(
        error instanceof Error ? error.message : String(error)
      );
    } finally {
      setPending(false);
    }
  };
  const Icon = /\.(zip|tar|gz|7z|rar)$/i.test(name)
    ? FileArchive
    : /\.(png|jpe?g|gif|webp|svg)$/i.test(name)
      ? FileImage
      : /\.(mp3|wav)$/i.test(name)
        ? FileAudio
        : /\.mp4$/i.test(name)
          ? FileVideo
          : FileText;
  return (
    <ConversationFileContextMenu
      path={path}
      onOpen={onOpen ? () => onOpen(path) : undefined}
    >
      <div className="agent-message-file-card" data-agent-file-card={path}>
        <span className="agent-message-file-card__icon" aria-hidden="true">
          <Icon size={22} />
        </span>
        <div className="agent-message-file-card__body">
          <button
            type="button"
            className="agent-message-file-card__name"
            title={name}
            disabled={!onOpen}
            onClick={() => onOpen?.(path)}
          >
            {name}
          </button>
          <div className="agent-message-file-card__footer">
            <span className="agent-message-file-card__size">
              {size === null ? "—" : formatAttachmentSize(size)}
            </span>
            <div className="agent-message-file-card__actions">
              {downloadFile || readFile ? (
                <Button
                  variant="chrome"
                  size="icon-sm"
                  title={downloadLabel}
                  aria-label={downloadLabel}
                  disabled={pending}
                  onClick={() =>
                    void run(async () => {
                      if (downloadFile) {
                        await downloadFile({ path });
                        return;
                      }
                      if (!readFile) return;
                      const { bytes } = await readFile({ path });
                      const url = URL.createObjectURL(
                        new Blob([new Uint8Array(bytes)], {
                          type: "application/octet-stream"
                        })
                      );
                      const anchor = document.createElement("a");
                      anchor.href = url;
                      anchor.download = name;
                      document.body.append(anchor);
                      anchor.click();
                      anchor.remove();
                      // Let the browser consume the URL before releasing the bytes.
                      setTimeout(() => URL.revokeObjectURL(url), 1000);
                    })
                  }
                >
                  <CloudDownload size={18} />
                </Button>
              ) : null}
              {reveal ? (
                <Button
                  variant="chrome"
                  size="icon-sm"
                  title={revealLabel}
                  aria-label={revealLabel}
                  disabled={pending}
                  onClick={() =>
                    void run(async () => {
                      const result = await reveal({ path });
                      if (result.fallbackToDirectory)
                        (host?.toast?.info ?? toast.info)(
                          t("agentHost.agentGui.revealFallbackDirectory")
                        );
                    })
                  }
                >
                  <FolderOpen size={18} />
                </Button>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </ConversationFileContextMenu>
  );
}

export function formatAttachmentSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  const unit = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)) - 1,
    units.length - 1
  );
  return `${Number((bytes / 1024 ** (unit + 1)).toFixed(2))} ${units[unit]}`;
}
