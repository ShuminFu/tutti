# Composer region layout acceptance

Run from the repository root after `pnpm setup:worktree`:

```sh
pnpm --filter @tutti-os/desktop exec vite --config ../../integration-tests/agent-composer-layout/vite.config.mjs
```

Use the URL Vite prints. This local-only fixture mounts the production
`AgentGUIBottomDockPane`, `AgentComposer`, interaction surfaces, shared styles,
and transcript scroll controller. It replaces provider data and reply bodies
with deterministic local data; no live session, daemon mutation, or filesystem
operation is performed by its controls. It is not an installed-host or provider
end-to-end test.

1. Scroll the long reply away from the end. Record transcript top, height,
   `scrollTop`, and `scrollHeight` with the browser's read-only DOM inspection.
2. Toggle approval, ask-user, lifted approval, and plan replacement. Also toggle
   the notice/goal and queued prompt, then grow the draft. The transcript and
   outer dock bounds must remain unchanged; no persistent card may paint over
   the transcript.
3. Answer the real approval and multi-question form. The receipt must carry the
   exact fixture request identity; the draft must remain available after a
   non-replacement prompt. Scroll the dock to reach all controls.
4. Repeat in regular, short, and narrow panes, at end and detached. Resizing may
   change the transcript viewport, but subsequent card changes must not.
5. Verify the portaled settings menu, editable long draft, and explicit latest-
   reply action remain usable. Dock-local scrolling must not alter transcript
   follow intent. Unit tests separately cover virtualized resize/exposure,
   session switching, and prepend restoration.

The fixture imports production layout without copying or overriding its dock
or transcript rules. Apply the baseline production CSS to the same fixture to
observe the old geometry failure; restore the exact final source afterward.
