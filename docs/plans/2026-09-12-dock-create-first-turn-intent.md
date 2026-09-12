# Preserve first-turn intent in embedded session creation

Date: 2026-09-12. Implemented with DeepSeek Harness Flash and statically reviewed by Codex.

The embedded session adapter and web host bridge carry explicit permission and plan settings (including false), worktree isolation and project placement, structured initial content, and display text to the RnDMaster host.

Images use data, a URL, or an archived source path. Attachment IDs belong to a specific session and cannot be copied to a newly created host session. Image-only requests may have an empty text prompt; invalid content or an image without a portable source produces an explicit error instead of silently losing content.

The matching RnDMaster host change is required for these optional fields. Its implementation note is `docs/2026-09-12-dock-create-first-turn-intent.md`. The fork pin and packaged artifacts were not updated in this implementation task.

Manual acceptance should cover restricted permissions and plan mode for Claude/Codex, worktree creation in an existing Git project, mixed text/image and image-only first turns, retries preserving content, and recovery retaining the session directory and current settings. No runtime acceptance was performed; repository commit and push hooks provide their normal checks during delivery.
