#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
suite="${1:---list}"
if [[ "$suite" == "--list" || "$suite" == "--help" ]]; then
  cat <<'USAGE'
Usage: bash <integration-tests|benchmarks>/run.sh <suite> [runner arguments...]
No suite runs by default. Available suites:
  packages-agent-store-sqlite: 现有 Go benchmark；仅运行列出的性能函数
  agent-gui: GUI 性能场景；隔离运行环境，遵循已有性能报告策略
USAGE
  exit 0
fi
shift
case "$suite" in
  packages-agent-store-sqlite)
    cd "$ROOT/packages/agent/store-sqlite"
    exec 'go' 'test' './.' '-run' '^$' '-bench' '^(BenchmarkStoreListSessionSectionsLargeRemovedProjectHistory|BenchmarkStoreListWorkspaceGeneratedFileTurns|BenchmarkSessionTitleMigrationBackfillsLargeHistory)$' '-benchmem' '-count=5' "$@"
    ;;
  agent-gui)
    cd "$ROOT/."
    exec 'pnpm' 'perf:agent-gui' '--' "$@"
    ;;
  *) printf 'Unknown suite: %s\n' "$suite" >&2; exit 2 ;;
esac
