import { useState } from "react";
import { useService } from "@tutti-os/infra/di";
import { ImportLinedIcon } from "@tutti-os/ui-system";
import { IWorkspaceUserProjectService } from "@renderer/features/workspace-user-project";
import { useTranslation } from "@renderer/i18n";
import { IWorkspaceSettingsService } from "../services/workspaceSettingsService.interface";
import type {
  CursorSkillImportPreview,
  CursorSkillImportResult
} from "../services/internal/adapters/desktopWorkspaceSettingsClient";
import {
  WorkspaceSettingsActionButton,
  workspaceSettingsControlColumnClass
} from "./WorkspaceSettingsActionButton";
import { cn } from "@renderer/lib/format";

export function WorkspaceCursorSkillsImportSettingsRow() {
  const { t } = useTranslation();
  const projects = useService(IWorkspaceUserProjectService);
  const settings = useService(IWorkspaceSettingsService);
  const [sourceDir, setSourceDir] = useState("");
  const [preview, setPreview] = useState<CursorSkillImportPreview | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [results, setResults] = useState<CursorSkillImportResult[] | null>(
    null
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<"preview" | "import" | null>(null);

  async function chooseSource() {
    const picked = await projects.selectDirectory();
    if (!picked) return;
    setBusy(true);
    setError(null);
    setResults(null);
    setPreview(null);
    setSourceDir(picked.path);
    try {
      const next = await settings.previewCursorSkills(picked.path);
      setPreview(next);
      setSelected(
        next.skills
          .filter((item) => item.status === "ready")
          .map((item) => item.name)
      );
    } catch {
      setError("preview");
    } finally {
      setBusy(false);
    }
  }

  async function importSelected() {
    if (!sourceDir || !selected.length) return;
    setBusy(true);
    setError(null);
    try {
      setResults(await settings.importCursorSkills(sourceDir, selected));
      setPreview(await settings.previewCursorSkills(sourceDir));
      setSelected([]);
    } catch {
      setError("import");
    } finally {
      setBusy(false);
    }
  }

  function toggle(name: string) {
    setSelected((current) =>
      current.includes(name)
        ? current.filter((item) => item !== name)
        : [...current, name]
    );
  }

  return (
    <div className="flex w-full flex-col gap-3">
      <div className="flex w-full items-center justify-between gap-4 max-[560px]:flex-col max-[560px]:items-stretch">
        <div className="flex min-w-0 flex-1 flex-col gap-1 max-[560px]:w-full">
          <strong className="text-[13px] font-semibold text-[var(--text-primary)]">
            {t("workspace.settings.agent.cursorSkills.label")}
          </strong>
          <p className="m-0 text-[13px] leading-[1.3] text-[var(--text-secondary)]">
            {t("workspace.settings.agent.cursorSkills.description")}
          </p>
        </div>
        <div
          className={cn(
            "flex justify-end max-[560px]:justify-start",
            workspaceSettingsControlColumnClass
          )}
        >
          <WorkspaceSettingsActionButton
            icon={<ImportLinedIcon className="size-3.5" />}
            label={t("workspace.settings.agent.cursorSkills.choose")}
            disabled={busy}
            type="button"
            onClick={() => void chooseSource()}
          />
        </div>
      </div>
      {busy && (
        <p className="m-0 text-[12px] text-[var(--text-secondary)]">
          {t("workspace.settings.agent.cursorSkills.working")}
        </p>
      )}
      {error && (
        <p role="alert" className="m-0 text-[12px] text-[var(--state-danger)]">
          {t(
            error === "preview"
              ? "workspace.settings.agent.cursorSkills.previewFailed"
              : "workspace.settings.agent.cursorSkills.importFailed"
          )}
        </p>
      )}
      {preview && (
        <div className="flex flex-col gap-2 rounded-[8px] border border-[var(--border-1)] p-3">
          <p className="m-0 break-all text-[12px] text-[var(--text-secondary)]">
            {t("workspace.settings.agent.cursorSkills.destination", {
              path: preview.destination
            })}
          </p>
          {preview.skills.length === 0 ? (
            <p className="m-0 text-[12px] text-[var(--text-secondary)]">
              {t("workspace.settings.agent.cursorSkills.empty")}
            </p>
          ) : (
            preview.skills.map((item, index) => (
              <label
                key={`${item.name}:${index}`}
                className="flex items-center gap-2 text-[13px] text-[var(--text-primary)]"
              >
                <input
                  type="checkbox"
                  checked={
                    item.status === "ready" && selected.includes(item.name)
                  }
                  disabled={busy || item.status !== "ready"}
                  onChange={() => toggle(item.name)}
                />
                <span>{item.name}</span>
                <span className="text-[12px] text-[var(--text-secondary)]">
                  {t(
                    `workspace.settings.agent.cursorSkills.status.${item.status}`
                  )}
                </span>
                {item.reason && item.status !== "exists" && (
                  <span
                    className="break-all text-[12px] text-[var(--text-secondary)]"
                    title={item.reason}
                  >
                    {item.reason}
                  </span>
                )}
              </label>
            ))
          )}
          <WorkspaceSettingsActionButton
            label={t("workspace.settings.agent.cursorSkills.import")}
            disabled={busy || selected.length === 0}
            type="button"
            onClick={() => void importSelected()}
          />
        </div>
      )}
      {results && (
        <div
          role="status"
          className="flex flex-col gap-1 text-[12px] text-[var(--text-secondary)]"
        >
          <p className="m-0">
            {t("workspace.settings.agent.cursorSkills.result", {
              imported: String(
                results.filter((item) => item.status === "imported").length
              ),
              skipped: String(
                results.filter((item) => item.status !== "imported").length
              )
            })}
          </p>
          {results
            .filter((item) => item.status !== "imported")
            .map((item) => (
              <p key={item.name} className="m-0 break-all">
                {item.name}: {item.reason || item.status}
              </p>
            ))}
        </div>
      )}
    </div>
  );
}
