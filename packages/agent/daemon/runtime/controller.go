package agentruntime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	replay "github.com/tutti-os/tutti/packages/agent/session-replay"
)

var (
	ErrSessionNotFound                  = errors.New("agent session not found")
	ErrSessionSettingsRequireNewSession = errors.New("agent session settings update requires a new session to preserve context")
	ErrSessionActiveTurn                = errors.New("agent session already has an active turn")
	ErrSessionForkUnsupported           = errors.New("agent session fork is unsupported")
)

const defaultStreamingReportCoalesceWindow = 50 * time.Millisecond
const interactiveDenyFollowUpStartTimeout = 30 * time.Second
const interactiveDenyFollowUpPollInterval = 25 * time.Millisecond

type execMetadataContextKey struct{}

type Controller struct {
	mu                          sync.Mutex
	streamObserverMu            sync.RWMutex
	providerObservationMu       sync.RWMutex
	goalControlObserverMu       sync.RWMutex
	mcpAppResolverMu            sync.RWMutex
	turnSlotObserverMu          sync.RWMutex
	sessions                    map[string]Session
	sessionAvailabilityWaiters  map[string]*sessionAvailabilityWaiter
	adapters                    map[string]Adapter
	adapterResolver             AdapterResolver
	turns                       map[string]activeTurn
	commands                    map[string]AgentSessionCommandSnapshot
	pendingCommandSnapshots     map[string]AgentSessionCommandSnapshot
	configOptionsUpdates        map[string]AgentSessionConfigOptionsUpdate
	pendingConfigOptionsUpdates map[string][]AgentSessionConfigOptionsUpdate
	provisionalSessions         map[string]bool
	sessionInitializations      map[string]*controllerSessionInitialization
	goalGenerationFences        map[string]*controllerGoalGenerationFenceRegistry
	startupLocks                map[startupLockKey]*controllerLifecycleLock
	lifecycleLocks              map[string]*controllerLifecycleLock
	hub                         *EventHub
	reporter                    DurableActivityReporter
	reportQueue                 *reportRequestQueue
	providerGoalAdoptionSink    ProviderGoalAdoptionSink
	terminalInteractions        terminalInteractiveDispositionStore
	streamObserver              RuntimeStreamEventObserver
	providerObservationObserver ProviderObservationObserver
	goalControlObserver         GoalControlLifecycleObserver
	// mcpAppResolver annotates MCP App tool calls (see mcp_app.go); nil disables it.
	mcpAppResolver MCPAppResolver
	// turnSlotObserver is told when a session's one canonical turn slot frees
	// up, so parked ordinary prompts can be admitted (controller_turn_slot.go).
	turnSlotObserver TurnSlotObserver
}

// RuntimeStreamEventObserver receives the ordered precommit stream projection
// synchronously with the per-session EventHub fan-out. Implementations must
// remain lightweight: the ordering guarantee prevents a durable terminal
// confirmation from overtaking its preceding optimistic deltas.
type RuntimeStreamEventObserver interface {
	ObserveRuntimeStreamEvents(
		context.Context,
		string,
		string,
		[]StreamEvent,
	) error
}

// ProviderObservationObserver receives capture-only provider observations
// synchronously before their durable activity report is queued.
type ProviderObservationObserver interface {
	ObserveProviderObservations(
		context.Context,
		string,
		string,
		[]replay.ProviderObservationBatch,
	) error
}

// GoalControlAppliedObservation is exact provider evidence that one durable
// Goal operation was consumed by the runtime. The Host validates every fence
// before completing the operation.
type GoalControlAppliedObservation struct {
	WorkspaceID      string
	AgentSessionID   string
	OperationID      string
	Revision         int64
	RepairEpoch      int64
	Action           string
	ProviderTurnID   string
	Observed         map[string]any
	OccurredAtUnixMS int64
	ExecutionPending bool
}

type GoalControlLifecycleObserver interface {
	ObserveGoalControlApplied(context.Context, GoalControlAppliedObservation) error
}

type controllerLifecycleLock struct {
	gate chan struct{}
	refs int
}

// controllerSessionInitialization retains every provider observation emitted
// between Runtime start and Host's canonical initialization commit. The map
// entry itself is the publication barrier; it remains present while Publish
// drains events and side-channel snapshots so later observations cannot
// overtake the initial Session report.
type controllerSessionInitialization struct {
	events                  []activityshared.Event
	initialEventsPublished  bool
	commandSnapshotResolved bool
}

// startupLockKey uses agentSessionID for normal Host calls. Provider is set
// only for the legacy path that asks Controller.Start to allocate the ID.
type startupLockKey struct {
	roomID         string
	agentSessionID string
	provider       string
}

type sessionAvailabilityWaiter struct {
	changed chan struct{}
	refs    int
}

type activeTurn struct {
	turnID                string
	cancel                context.CancelFunc
	tuttiModeSnapshot     *TuttiModeTurnSnapshot
	openCallIDs           map[string]struct{}
	pendingTerminalEvents []activityshared.Event
}

type reportRequest struct {
	ctx              context.Context
	report           agentsessionstore.ReportActivityInput
	submitProvenance bool
	barrier          bool
	done             chan error
}

// defaultLiveSessionEvictionGrace 是超限回收的默认护身符：刚说完话不到一分钟的
// 会话不会被挤掉。一分钟足够盖住「回完一轮、用户正在打字」这段。
const defaultLiveSessionEvictionGrace = time.Minute

type ReleaseIdleLiveSessionsInput struct {
	// IdleAfter 是所有会话的默认空闲阈值；IdleAfterFor 为某条会话另行表态时以后者为准。
	IdleAfter time.Duration
	// IdleAfterFor 让调用方按会话定阈值（DINTAL-5308）。
	//
	// 为什么需要按会话分：进程该留多久不是一个全局事实。用户正在界面上聊的会话，
	// 留着是为了下一句话不用冷启动；而 visible=false 的机器派工（工作流/自动化）
	// 没有「下一句话」，回合一结束就该把进程还给系统。
	//
	// 返回值语义：>0 = 空闲这么久之后回收；0 = 只要不在跑回合就立刻回收；
	// <0 = 这条会话不回收（用户选了常驻）。留 nil 则所有会话都用 IdleAfter。
	IdleAfterFor func(session Session) time.Duration
	// MaxLiveSessions 是常驻进程的条数上限（DINTAL-5308）。
	//
	// 光有 TTL 挡不住这种情况：半小时里开了几十条会话、每条都刚聊过所以都没到
	// TTL，于是几十个 provider 进程一起常驻。按 TTL 它们全都「还新鲜」，按内存
	// 它们已经把机器吃光了。所以再加一道按条数的闸：超出上限时，从**最久没说话
	// 的那条**开始回收，直到回到上限以内（LRU）。
	//
	// 0 表示不限条数。
	MaxLiveSessions int
	// EvictionGrace 是超限回收的护身符：只有「安静超过这么久」的会话才会被挤掉。
	// 防的是刚回完一轮、用户正要接着打字就被掐掉进程。留 0 用
	// defaultLiveSessionEvictionGrace。
	//
	// 注意它只管超限回收这一档；TTL 那档本来就有自己的阈值。
	EvictionGrace time.Duration
	Now           time.Time
	Limit         int
}

type ReleaseIdleLiveSessionsResult struct {
	Scanned            int
	Released           int
	SkippedFresh       int
	SkippedActiveTurn  int
	SkippedUnsupported int
	SkippedNotLive     int
	SkippedBusy        int
	// SkippedRetained 是策略明说「这条不回收」的会话（IdleAfterFor 返回负数），
	// 与 SkippedFresh（还没到阈值）分开记：前者是用户的选择，后者只是还没轮到。
	SkippedRetained int
	// EvictedOverCap 是因为超出 MaxLiveSessions 而被挤掉的会话数（LRU 那一档），
	// 与 Released（到点回收）分开记：一个说明上限太紧，一个说明 TTL 到了。
	EvictedOverCap int
	// SkippedOverCapProtected 是超限了、但因为刚说完话还在护身符里而没动的会话。
	// 它不为 0 却还在超限，说明上限设得比「同时在用的会话数」还小。
	SkippedOverCapProtected int
	Failed                  int
}

// CloseAllLiveSessionsResult reports the outcome of CloseAllLiveSessions.
type CloseAllLiveSessionsResult struct {
	// Scanned counts sessions whose adapter reported a live provider process.
	Scanned int
	Closed  int
	Failed  int
}

type asyncActivityReporter interface {
	DurableActivityReporter
	AsyncActivityReporter()
}

func NewController(adapters []Adapter, reporter DurableActivityReporter) *Controller {
	return NewControllerWithAdapterResolver(adapters, reporter, nil)
}

func NewControllerWithAdapterResolver(adapters []Adapter, reporter DurableActivityReporter, resolver AdapterResolver) *Controller {
	byProvider := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		provider := strings.TrimSpace(adapter.Provider())
		if provider != "" {
			byProvider[provider] = adapter
		}
	}
	controller := &Controller{
		sessions:                    make(map[string]Session),
		sessionAvailabilityWaiters:  make(map[string]*sessionAvailabilityWaiter),
		adapters:                    byProvider,
		adapterResolver:             resolver,
		turns:                       make(map[string]activeTurn),
		commands:                    make(map[string]AgentSessionCommandSnapshot),
		pendingCommandSnapshots:     make(map[string]AgentSessionCommandSnapshot),
		configOptionsUpdates:        make(map[string]AgentSessionConfigOptionsUpdate),
		pendingConfigOptionsUpdates: make(map[string][]AgentSessionConfigOptionsUpdate),
		provisionalSessions:         make(map[string]bool),
		sessionInitializations:      make(map[string]*controllerSessionInitialization),
		goalGenerationFences:        make(map[string]*controllerGoalGenerationFenceRegistry),
		startupLocks:                make(map[startupLockKey]*controllerLifecycleLock),
		lifecycleLocks:              make(map[string]*controllerLifecycleLock),
		hub:                         NewEventHub(),
		reporter:                    reporter,
	}
	if reporter != nil {
		if _, ok := reporter.(asyncActivityReporter); !ok {
			controller.reportQueue = newReportRequestQueue()
			go controller.runReportWorker()
		}
	}
	for _, adapter := range byProvider {
		controller.configureAdapter(adapter)
	}
	return controller
}

func (c *Controller) sessionPublicationPendingLocked(key string) bool {
	if c == nil {
		return false
	}
	if c.provisionalSessions[key] {
		return true
	}
	_, pending := c.sessionInitializations[key]
	return pending
}

func (c *Controller) configureAdapter(adapter Adapter) {
	if adapter == nil {
		return
	}
	if sinkAdapter, ok := adapter.(CommandSnapshotSinkAdapter); ok {
		sinkAdapter.SetCommandSnapshotSink(c.applyCommandSnapshotByAgentSessionID)
	}
	if sinkAdapter, ok := adapter.(SessionEventSinkAdapter); ok {
		sinkAdapter.SetSessionEventSink(c.applySessionEventsByAgentSessionID)
	}
	if sinkAdapter, ok := adapter.(GoalReconcileDurableSinkAdapter); ok {
		sinkAdapter.SetGoalReconcileDurableSink(c.reportGoalReconcileDurable)
	}
	if sinkAdapter, ok := adapter.(GoalProvenanceDurableSinkAdapter); ok {
		// Always install the controller boundary. If the configured reporter
		// cannot durably bind/lookup provenance, the controller returns an
		// explicit error and Codex fails closed instead of silently falling back
		// to a restart-unsafe process-local cache.
		sinkAdapter.SetGoalProvenanceDurableSink(c)
	}
	if sinkAdapter, ok := adapter.(ProviderGoalAdoptionSinkAdapter); ok {
		sinkAdapter.SetProviderGoalAdoptionSink(c.adoptProviderGoal)
	}
	if sinkAdapter, ok := adapter.(ConfigOptionsUpdateSinkAdapter); ok {
		sinkAdapter.SetConfigOptionsUpdateSink(c.applyConfigOptionsUpdateByAgentSessionID)
	}
	if sinkAdapter, ok := adapter.(InteractiveDispositionSinkAdapter); ok {
		sinkAdapter.SetInteractiveDispositionSink(c.recordTerminalInteractiveDisposition)
	}
	if sourceAdapter, ok := adapter.(MCPServerInstructionsSourceAdapter); ok {
		// Only adapters whose provider drops MCP initialize instructions opt in;
		// the resolver is read lazily because tuttid installs it after construction.
		sourceAdapter.SetMCPServerInstructionsSource(c.contractMCPServerInstructions)
	}
}

func NewDefaultController(reporter DurableActivityReporter) *Controller {
	return NewDefaultControllerWithProcessTransport(reporter, nil)
}

func NewDefaultControllerWithProcessTransport(
	reporter DurableActivityReporter,
	transport ProcessTransport,
) *Controller {
	return NewDefaultControllerWithOptions(reporter, transport, ControllerOptions{
		HostMetadata: LegacyHostMetadata(),
	})
}

func NewDefaultControllerWithOptions(
	reporter DurableActivityReporter,
	transport ProcessTransport,
	options ControllerOptions,
) *Controller {
	host := options.HostMetadata
	adapters := newMigratedProviderAdapters(
		transport,
		host,
		options.ProviderCommandResolver,
		options.CommandNetworkAccessPolicy,
	)
	setProviderLaunchPreparer(adapters, options.ProviderLaunchPreparer)
	return NewControllerWithAdapterResolver(adapters, reporter, options.AdapterResolver)
}
