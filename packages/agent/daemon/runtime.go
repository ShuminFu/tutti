package agentdaemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
)

var ErrHostMetadataRequired = errors.New("agent daemon host metadata is required")
var ErrProcessTransportRequired = errors.New("agent daemon process transport is required")

const (
	defaultLiveSessionReaperIdleAfter     = 30 * time.Minute
	defaultLiveSessionReaperSweepInterval = 5 * time.Minute

	// shutdownCloseAllLiveSessionsTimeout bounds how long Runtime.Close waits
	// for CloseAllLiveSessions to force-terminate every live provider
	// process. Each process close is already internally bounded (SIGTERM,
	// then SIGKILL after a short grace period; see localProcessConnection),
	// so this is a backstop against an unexpectedly large number of live
	// sessions, not the primary timeout mechanism.
	shutdownCloseAllLiveSessionsTimeout = 15 * time.Second
)

type ActivityReporter = agentruntime.ActivityReporter
type DurableActivityReporter = agentruntime.DurableActivityReporter
type Adapter = agentruntime.Adapter
type ClientInfo = agentruntime.ClientInfo
type Controller = agentruntime.Controller
type HostMetadata = agentruntime.HostMetadata
type ProcessTransport = agentruntime.ProcessTransport
type RecordingProcessTransport = agentruntime.RecordingProcessTransport
type ReplayPlaybackState = agentruntime.ReplayPlaybackState
type ReplayProcessTransport = agentruntime.ReplayProcessTransport
type Session = agentruntime.Session
type SessionReplayProcessRegistration = agentruntime.SessionReplayProcessRegistration
type SessionReplayProcessTransport = agentruntime.SessionReplayProcessTransport
type SessionRecordingProcessTransport = agentruntime.SessionRecordingProcessTransport
type ProviderCommand = agentruntime.ProviderCommand
type ProviderCommandResolver = agentruntime.ProviderCommandResolver
type ProviderLaunchPrepareInput = agentruntime.ProviderLaunchPrepareInput
type ProviderLaunchPrepareResult = agentruntime.ProviderLaunchPrepareResult
type ProviderLaunchPreparer = agentruntime.ProviderLaunchPreparer
type ProviderLaunchPreparerAdapter = agentruntime.ProviderLaunchPreparerAdapter
type AdapterResolver = agentruntime.AdapterResolver
type CommandNetworkAccessPolicy = agentruntime.CommandNetworkAccessPolicy

type Config struct {
	Reporter                   DurableActivityReporter
	ProcessTransport           ProcessTransport
	HostMetadata               HostMetadata
	ProviderCommandResolver    ProviderCommandResolver
	ProviderLaunchPreparer     ProviderLaunchPreparer
	AdapterResolver            AdapterResolver
	CommandNetworkAccessPolicy CommandNetworkAccessPolicy
	Adapters                   []Adapter
	LiveSessionReaper          LiveSessionReaperConfig
	// LiveSessionReaperLoad 让回收策略可以热改：每次扫描前重读一次。
	//
	// 为什么不能只在启动时读一次：这套旋钮是要给用户在设置里调的（DINTAL-5308
	// 的「Agent 进程常驻」开关与 TTL）。启动时读死意味着改完要重启 tuttid 才生效，
	// 而用户只会看到「我关了它还在」。留 nil 就一直用 LiveSessionReaper 的静态值。
	LiveSessionReaperLoad func() LiveSessionReaperConfig
}

type LiveSessionReaperConfig struct {
	Enabled       *bool
	IdleAfter     time.Duration
	SweepInterval time.Duration
	// IdleAfterFor 按会话定空闲阈值（DINTAL-5308）。留 nil 则所有会话用 IdleAfter。
	// 语义见 agentruntime.ReleaseIdleLiveSessionsInput.IdleAfterFor：
	// >0 等这么久、0 不在跑回合就立刻放、<0 不回收。
	IdleAfterFor func(session Session) time.Duration
	// MaxLiveSessions 是常驻会话数的上限（DINTAL-5308 的 LRU 那一档）。
	//
	// 为什么光有 TTL 不够：半小时里开二十条会话、每条都刚聊过，按 TTL 它们全都
	// 「还新鲜」，可二十个 CLI 进程已经把内存吃光了。上限管的是「一共留了多少条」，
	// 超了就从最久没说话的那条开始挤（LRU）。0 = 不限。
	MaxLiveSessions int
	// EvictionGrace 是挤人时的护身符：刚说过话的会话即使超限也不动，
	// 因为用户很可能正要接着打字。留 0 用默认一分钟。只对上限这一档生效。
	EvictionGrace time.Duration
}

type Runtime struct {
	controller       *Controller
	processTransport ProcessTransport
	cancel           context.CancelFunc
	done             chan struct{}
	closeOnce        sync.Once
}

func NewRuntime(config Config) (*Runtime, error) {
	var controller *Controller
	if len(config.Adapters) > 0 {
		agentruntime.ApplyProviderLaunchPreparer(config.Adapters, config.ProviderLaunchPreparer)
		controller = agentruntime.NewController(config.Adapters, config.Reporter)
	} else {
		if !hasCompleteHostMetadata(config.HostMetadata) {
			return nil, ErrHostMetadataRequired
		}
		if config.ProcessTransport == nil {
			return nil, ErrProcessTransportRequired
		}
		controller = agentruntime.NewDefaultControllerWithOptions(
			config.Reporter,
			config.ProcessTransport,
			agentruntime.ControllerOptions{
				HostMetadata:               config.HostMetadata,
				ProviderCommandResolver:    config.ProviderCommandResolver,
				ProviderLaunchPreparer:     config.ProviderLaunchPreparer,
				AdapterResolver:            config.AdapterResolver,
				CommandNetworkAccessPolicy: config.CommandNetworkAccessPolicy,
			},
		)
	}
	runtime := &Runtime{
		controller:       controller,
		processTransport: config.ProcessTransport,
	}
	runtime.startLiveSessionReaper(config.LiveSessionReaper, config.LiveSessionReaperLoad)
	return runtime, nil
}

func NewLocalProcessTransport() ProcessTransport {
	return agentruntime.NewLocalProcessTransport()
}

func NewRecordingProcessTransport(
	base ProcessTransport,
	directory string,
) (*RecordingProcessTransport, error) {
	return agentruntime.NewRecordingProcessTransport(base, directory)
}

func NewReplayProcessTransport(directory string) (*ReplayProcessTransport, error) {
	return agentruntime.NewReplayProcessTransport(directory)
}

func NewSessionReplayProcessTransport(
	registrations []SessionReplayProcessRegistration,
) (*SessionReplayProcessTransport, error) {
	return agentruntime.NewSessionReplayProcessTransport(registrations)
}

func NewSessionRecordingProcessTransport(
	base ProcessTransport,
) (*SessionRecordingProcessTransport, error) {
	return agentruntime.NewSessionRecordingProcessTransport(base)
}

func MustRuntime(config Config) *Runtime {
	runtime, err := NewRuntime(config)
	if err != nil {
		panic(err)
	}
	return runtime
}

func (r *Runtime) Controller() *Controller {
	if r == nil {
		return nil
	}
	return r.controller
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.closeAllLiveSessions()
		if finalizer, ok := r.processTransport.(interface{ Finalize() error }); ok {
			if err := finalizer.Finalize(); err != nil {
				slog.Error(
					"agent process transport finalization failed",
					"event", "agent_session.process_transport.finalize_failed",
					"error", err,
				)
			}
		}
		if r.cancel != nil {
			r.cancel()
		}
		if r.done != nil {
			<-r.done
		}
	})
}

// closeAllLiveSessions force-terminates every live provider process, including
// Codex app-server, the Claude Code SDK sidecar, and subprocess adapters for
// providers that use ACP,
// before the daemon process exits. A spawned subprocess is not killed automatically
// just because tuttid exits — it is reparented to init and keeps running —
// so without this step, every daemon shutdown (or a desktop-parent-monitor
// triggered shutdown after the host app disappears) would orphan any
// in-flight provider processes, leaving them running unmanaged against the
// session's working directory indefinitely.
func (r *Runtime) closeAllLiveSessions() {
	if r == nil || r.controller == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownCloseAllLiveSessionsTimeout)
	defer cancel()
	result := r.controller.CloseAllLiveSessions(ctx)
	if result.Scanned == 0 {
		return
	}
	slog.Info("agent live session shutdown close completed",
		"event", "agent_session.shutdown_close.completed",
		"scanned", result.Scanned,
		"closed", result.Closed,
		"failed", result.Failed,
	)
}

func (r *Runtime) startLiveSessionReaper(config LiveSessionReaperConfig, load func() LiveSessionReaperConfig) {
	if r == nil || r.controller == nil {
		return
	}
	// 静态配置且明说关掉 —— 保持老行为，连 goroutine 都不起。
	// 有 load 时一律起：用户随时可能在设置里把它打开，循环得在那儿等着。
	if load == nil && !liveSessionReaperEnabled(config) {
		return
	}
	if load == nil {
		static := config
		load = func() LiveSessionReaperConfig { return static }
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		// 用 Timer 而不是 Ticker：扫描间隔本身也可能被用户改，每轮按当前配置重置。
		timer := time.NewTimer(liveSessionReaperSweepInterval(load()))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			current := load()
			timer.Reset(liveSessionReaperSweepInterval(current))
			if !liveSessionReaperEnabled(current) {
				continue
			}
			idleAfter := current.IdleAfter
			if idleAfter <= 0 {
				idleAfter = defaultLiveSessionReaperIdleAfter
			}
			result := r.controller.ReleaseIdleLiveSessions(ctx, agentruntime.ReleaseIdleLiveSessionsInput{
				IdleAfter:       idleAfter,
				IdleAfterFor:    current.IdleAfterFor,
				MaxLiveSessions: current.MaxLiveSessions,
				EvictionGrace:   current.EvictionGrace,
				Now:             time.Now(),
			})
			if result.Scanned == 0 {
				continue
			}
			slog.Info("agent live session reaper sweep completed",
				"event", "agent_session.live_reaper.sweep_completed",
				"scanned", result.Scanned,
				"released", result.Released,
				"skipped_fresh", result.SkippedFresh,
				"skipped_active_turn", result.SkippedActiveTurn,
				"skipped_unsupported", result.SkippedUnsupported,
				"skipped_not_live", result.SkippedNotLive,
				"skipped_busy", result.SkippedBusy,
				"skipped_retained", result.SkippedRetained,
				"evicted_over_cap", result.EvictedOverCap,
				"skipped_over_cap_protected", result.SkippedOverCapProtected,
				"failed", result.Failed,
			)
		}
	}()
}

func liveSessionReaperSweepInterval(config LiveSessionReaperConfig) time.Duration {
	if config.SweepInterval > 0 {
		return config.SweepInterval
	}
	return defaultLiveSessionReaperSweepInterval
}

func liveSessionReaperEnabled(config LiveSessionReaperConfig) bool {
	if config.Enabled == nil {
		return true
	}
	return *config.Enabled
}

func hasCompleteHostMetadata(host HostMetadata) bool {
	return strings.TrimSpace(host.ClientInfo.Name) != "" &&
		strings.TrimSpace(host.ClientInfo.Title) != "" &&
		strings.TrimSpace(host.ClientInfo.Version) != "" &&
		strings.TrimSpace(host.WorkspaceEnvName) != "" &&
		strings.TrimSpace(host.OpenClawSessionKeyPrefix) != ""
}
