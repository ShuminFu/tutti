import type { DesktopRuntimeApi } from "@preload/types";
import { AppRendererErrorReporter } from "@renderer/features/analytics/reporters/app-renderer-error/appRendererErrorReporter.ts";
import type { IReporterService } from "@renderer/features/analytics/services/reporterService.interface.ts";
import { isBenignResizeObserverLoopError } from "@renderer/lib/reactDiagnostics.ts";

let installed = false;

export function installRendererDiagnostics(
  runtimeApi: Pick<DesktopRuntimeApi, "logRendererDiagnostic">,
  source = "workspace-renderer",
  reporterService?: Pick<IReporterService, "trackEvents">
): void {
  if (installed) {
    return;
  }
  installed = true;

  window.addEventListener("error", (event) => {
    // DinTalDock: benign ResizeObserver warning, not a crash (see reactDiagnostics.ts).
    if (isBenignResizeObserverLoopError(event)) {
      event.preventDefault();
      return;
    }
    const details = errorDetails(event.error ?? event.message);
    sendRendererDiagnostic(runtimeApi, {
      details: {
        ...details,
        column: finiteNumber(event.colno),
        filename: trimmedString(event.filename),
        line: finiteNumber(event.lineno)
      },
      event: "renderer.uncaught_error",
      level: "error",
      source
    });
    reportRendererError(reporterService, {
      errorMessage: sanitizedErrorMessage(details.message),
      errorType: sanitizedErrorType(details.name),
      source: "unhandled_error"
    });
  });

  window.addEventListener("unhandledrejection", (event) => {
    const details = errorDetails(event.reason);
    sendRendererDiagnostic(runtimeApi, {
      details,
      event: "renderer.unhandled_rejection",
      level: "error",
      source
    });
    reportRendererError(reporterService, {
      errorMessage: sanitizedErrorMessage(details.message),
      errorType: sanitizedErrorType(details.name),
      source: "unhandled_rejection"
    });
  });
}

function reportRendererError(
  reporterService: Pick<IReporterService, "trackEvents"> | undefined,
  params: {
    errorMessage: string;
    errorType: string;
    source: "unhandled_error" | "unhandled_rejection";
  }
): void {
  if (!reporterService) {
    return;
  }
  void new AppRendererErrorReporter(params, { reporterService })
    .report()
    .catch(() => undefined);
}

function sendRendererDiagnostic(
  runtimeApi: Pick<DesktopRuntimeApi, "logRendererDiagnostic">,
  input: Parameters<DesktopRuntimeApi["logRendererDiagnostic"]>[0]
): void {
  void runtimeApi.logRendererDiagnostic(input).catch(() => {});
}

function errorDetails(value: unknown): Record<string, unknown> {
  if (value instanceof Error) {
    return {
      message: value.message,
      name: value.name,
      stack: limitDiagnosticText(value.stack)
    };
  }
  return {
    message: limitDiagnosticText(stringifyDiagnosticValue(value)),
    name: typeof value
  };
}

function stringifyDiagnosticValue(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (value === null || value === undefined) {
    return String(value);
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return value.toString();
  }
  if (typeof value === "bigint") {
    return value.toString();
  }
  if (typeof value === "symbol") {
    return value.description ? `Symbol(${value.description})` : "Symbol()";
  }
  if (typeof value === "function") {
    return value.name ? `[function ${value.name}]` : "[function]";
  }
  try {
    return JSON.stringify(value);
  } catch {
    return Object.prototype.toString.call(value);
  }
}

function limitDiagnosticText(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  if (!trimmed) {
    return undefined;
  }
  const maxLength = 8_000;
  return trimmed.length > maxLength
    ? `${trimmed.slice(0, maxLength)}...`
    : trimmed;
}

function sanitizedErrorMessage(value: unknown): string {
  return (
    limitDiagnosticText(typeof value === "string" ? value : undefined) ?? ""
  );
}

function sanitizedErrorType(value: unknown): string {
  return (
    limitDiagnosticText(typeof value === "string" ? value : undefined) ??
    "Error"
  );
}

function finiteNumber(value: number): number | undefined {
  return Number.isFinite(value) ? value : undefined;
}

function trimmedString(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed || undefined;
}
