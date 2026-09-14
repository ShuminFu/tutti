package agentstatus

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// 同一个 provider 的并发探测被 singleflight 并成**一次**执行，这次执行不能继承「先到者」的
// ctx。
//
// 修复前的样子：诊断端点（预算短）先到并超时 → 它把自己的 ctx 取消 → 共享的探针被一起取消，
// 以 protocolCategory=probe_canceled / diagnostic="context canceled" 结束 → 验证不通过 →
// 结果以 availability=not_installed + reasonCode=codex_runtime_selection_unavailable 广播给
// 同时段**所有**等待者，包括那个本来有足够预算、完全跑得完的状态轮询。表现就是同一个 provider
// 时而 ready 时而 not_installed，取决于哪个调用方先到——而二进制一直装着、版本也一直够。
//
// 这条用例固定「短预算先到」，断言长预算那一方仍拿到真实结果，且这次探测没有被复制成两份。
func TestShortBudgetCallerCannotPoisonSharedProviderDetection(t *testing.T) {
	home := t.TempDir()
	hostCodex := writeCodexVersionFixture(t, filepath.Join(home, "nvm", "bin", "codex"), "0.145.0")
	service := probeTestService(home)
	// PATH 上没有 codex：只有宿主投影能给出这条路径，探测目标唯一。
	service.Environ = func() []string { return []string{"PATH=" + filepath.Join(home, "empty")} }
	service.CodexRuntimeSelectionStore = &memoryCodexRuntimeSelectionStore{}
	service.StatusCache = NewProviderStatusCache()
	service.StatusCacheTTL = time.Minute
	t.Setenv(HostRuntimeSelectionFileEnv, writeHostRuntimeSelection(t,
		`{"version":1,"providers":{"codex":{"binPath":"`+hostCodex+`"}}}`))

	var probes int32
	const probeCost = 200 * time.Millisecond
	service.CodexProtocolProbe = func(ctx context.Context, _ []string, _ []string) CodexProbeEvidence {
		atomic.AddInt32(&probes, 1)
		select {
		case <-time.After(probeCost):
			return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
		case <-ctx.Done():
			// 修复前，短预算调用方的取消会走到这里。
			return CodexProbeEvidence{CommandStarted: true, Category: "probe_canceled"}
		}
	}

	specs, err := service.selectProviderSpecs(context.Background(), []string{agentproviderbiz.Codex}, true)
	if err != nil || len(specs) == 0 {
		t.Fatalf("selectProviderSpecs() = %#v, %v", specs, err)
	}
	spec := specs[0]

	// 探针次数不能直接断言成 1：一次状态探测会**合法地**调用协议探针两次——一次在 runtime
	// resolution 里验证候选（codex_runtime_validation.go），一次在 adapter probe 里
	// （service_adapter_probe.go；AdapterProbeCache 为空时每次都会跑）。所以先量一次「单次探测」
	// 的基准，再拿并发结果跟它比，免得把实现细节写成魔数。
	service.StatusCache.invalidate(agentproviderbiz.Codex)
	_ = service.cachedStatusForSpec(context.Background(), spec, false)
	probesPerDetection := atomic.LoadInt32(&probes)
	if probesPerDetection == 0 {
		t.Fatal("calibration detection never probed: the stub is not on the status path")
	}

	// 空出缓存，让「短预算先到」的两个调用方并发打同一个 provider。
	service.StatusCache.invalidate(agentproviderbiz.Codex)
	atomic.StoreInt32(&probes, 0)

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelShort()
	longCtx, cancelLong := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLong()

	var wg sync.WaitGroup
	var longStatus ProviderStatus
	wg.Add(2)
	// 短预算那个先发，先占住 singleflight。
	go func() { defer wg.Done(); _ = service.cachedStatusForSpec(shortCtx, spec, false) }()
	time.Sleep(20 * time.Millisecond)
	go func() { defer wg.Done(); longStatus = service.cachedStatusForSpec(longCtx, spec, false) }()
	wg.Wait()

	if longStatus.Availability.Status != AvailabilityReady {
		t.Fatalf("long-budget caller got %q (reasonCode=%q); a short-budget caller must not poison the shared detection",
			longStatus.Availability.Status, longStatus.Availability.ReasonCode)
	}
	if got := atomic.LoadInt32(&probes); got != probesPerDetection {
		t.Fatalf("probes = %d, want %d (= exactly one shared detection); a per-caller detection is what "+
			"re-introduces the first-caller-decides hazard", got, probesPerDetection)
	}
}
