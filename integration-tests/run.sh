#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
suite="${1:---list}"
if [[ "$suite" == "--list" || "$suite" == "--help" ]]; then
  cat <<'USAGE'
Usage: bash <integration-tests|benchmarks>/run.sh <suite> [runner arguments...]
No suite runs by default. Available suites:
  activity-eventhub: SQLite 活动投影、事件发布与恢复集成；先按 daemon 指南准备 builtin assets
  agent-recovery: 会话初始化、持久化与重启恢复；先准备 builtin assets
  device-authority: 设备授权与响应丢失后的重试
  agent-gui: AgentGUI replay；真实 GUI 环境，仅显式选择运行
  go-packages-clients-device-authority-go: 包内集成测试；平台、环境和 skip 条件见源码
  go-services-tuttid-service-agent: 包内集成测试；平台、环境和 skip 条件见源码
  go-services-tuttid-service-computer: 包内集成测试；平台、环境和 skip 条件见源码
  go-services-tuttid-service-mcpapp: 包内集成测试；平台、环境和 skip 条件见源码
USAGE
  exit 0
fi
shift
case "$suite" in
  activity-eventhub)
    cd "$ROOT/integration-tests"
    exec 'go' 'test' './tuttid' '-count=1' "$@"
    ;;
  agent-recovery)
    cd "$ROOT/services/tuttid"
    exec 'go' 'test' './service/agent' '-count=1' '-run' '^(TestCreateSessionCanonicalInitialization|TestClaudeSessionForkTraverses|TestDurableMarkerSurvives|TestGoalRecovery|TestGoalRepairSet|TestAcceptedClaude)' "$@"
    ;;
  device-authority)
    cd "$ROOT/packages/clients/device-authority-go"
    exec 'go' 'test' '.' '-count=1' '-run' '^(TestClientDeviceAuthorityOwnerLifecycle|TestClientEnrollmentRetry)' "$@"
    ;;
  agent-gui)
    cd "$ROOT/."
    exec 'pnpm' 'e2e:agent-gui' '--' "$@"
    ;;
  go-packages-clients-device-authority-go)
    cd "$ROOT/packages/clients/device-authority-go"
    exec go test './.' -count=1 -run '^(TestClientDeviceAuthorityOwnerLifecycle|TestClientEnrollmentRetryReusesIdentityAfterLostResponse)$' "$@"
    ;;
  go-services-tuttid-service-agent)
    cd "$ROOT/services/tuttid"
    exec go test './service/agent' -count=1 -run '^(TestCreateSessionCanonicalInitializationBarrierWithRealControllerAndSQLite|TestClaudeSessionForkTraversesProductionWiringAcrossRestart|TestDurableMarkerSurvivesObserverFailureAndCanReplay|TestGoalRecoveryDoesNotReplayAcceptedClaudeSet|TestGoalRecoveryTimeoutThenRestartDoesNotReplayClaudeSet|TestGoalRepairSetTimeoutThenRestartDoesNotReplayClaudeSet|TestAcceptedClaudeGoalExpiresWithoutProviderReplay|TestAcceptedClaudeClearExpiresWhenLifecycleEvidenceIsLost)$' "$@"
    ;;
  go-services-tuttid-service-computer)
    cd "$ROOT/services/tuttid"
    exec go test './service/computer' -count=1 -run '^(TestAdaptToolCallIntegration|TestWindowsComputerDriverE2E)$' "$@"
    ;;
  go-services-tuttid-service-mcpapp)
    cd "$ROOT/services/tuttid"
    exec go test './service/mcpapp' -count=1 -run '^(TestFirstShowWidgetCallGetsMCPAppAndSnapshotEndToEnd)$' "$@"
    ;;
  *) printf 'Unknown suite: %s\n' "$suite" >&2; exit 2 ;;
esac
