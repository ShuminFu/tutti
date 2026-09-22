type ComposerModelListProbe = (force: boolean) => void;

const composerModelListProbe: { current: ComposerModelListProbe | null } = {
  current: null
};

export function setComposerModelListProbe(
  probe: ComposerModelListProbe | null
): void {
  composerModelListProbe.current = probe;
}

export function requestComposerModelListProbe(force: boolean): void {
  composerModelListProbe.current?.(force);
}
