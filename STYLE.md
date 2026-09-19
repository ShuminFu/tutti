# Code and UI style

## Tooling

- TypeScript/JavaScript: Oxfmt for formatting, Oxlint for linting.
- JSON, Markdown, YAML, CSS and HTML: repository Prettier configuration.
- Go: `gofmt`; lint uses the pinned `golangci-lint`.
- Business-code files stay within the repository's 800-line static-analysis limit, excluding blank and comment-only lines. See [Static analysis](docs/conventions/static-analysis.md).

## UI and copy

- Reuse `@tutti-os/ui-system` components and semantic color, typography and spacing tokens. Extend the shared system when an existing primitive cannot express the requirement.
- User-visible text uses the owning i18n layer. Chinese UI copy does not end with a Chinese full stop (`。`).
- Detailed visual rules: [Desktop visual language](docs/conventions/desktop-visual-language.md); component ownership: [UI guide](packages/ui/AGENTS.md).

Architecture belongs in [docs/architecture](docs/architecture/README.md). Validation and delivery steps belong in [CONTRIBUTING](CONTRIBUTING.md).
