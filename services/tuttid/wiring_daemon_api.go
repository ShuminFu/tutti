package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	agentdaemon "github.com/tutti-os/tutti/packages/agent/daemon"
	agenthostadapter "github.com/tutti-os/tutti/packages/agent/daemon/hostadapter"
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	agentstoresqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	tuttiapi "github.com/tutti-os/tutti/services/tuttid/api"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
	agentextensiondata "github.com/tutti-os/tutti/services/tuttid/data/agentextension"
	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	accountservice "github.com/tutti-os/tutti/services/tuttid/service/account"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
	agentextensionservice "github.com/tutti-os/tutti/services/tuttid/service/agentextension"
	agentmaintenanceservice "github.com/tutti-os/tutti/services/tuttid/service/agentmaintenance"
	agentquickpromptservice "github.com/tutti-os/tutti/services/tuttid/service/agentquickprompt"
	agentsessionreplayservice "github.com/tutti-os/tutti/services/tuttid/service/agentsessionreplay"
	agentstatusservice "github.com/tutti-os/tutti/services/tuttid/service/agentstatus"
	agenttargetservice "github.com/tutti-os/tutti/services/tuttid/service/agenttarget"
	automationruleservice "github.com/tutti-os/tutti/services/tuttid/service/automationrule"
	browsersvc "github.com/tutti-os/tutti/services/tuttid/service/browser"
	appclicli "github.com/tutti-os/tutti/services/tuttid/service/cli/appcli"
	collabrunservice "github.com/tutti-os/tutti/services/tuttid/service/collabrun"
	computersvc "github.com/tutti-os/tutti/services/tuttid/service/computer"
	eventstreamservice "github.com/tutti-os/tutti/services/tuttid/service/eventstream"
	managedcredentialsservice "github.com/tutti-os/tutti/services/tuttid/service/managedcredentials"
	managedruntime "github.com/tutti-os/tutti/services/tuttid/service/managedruntime"
	mcpappservice "github.com/tutti-os/tutti/services/tuttid/service/mcpapp"
	modelbindingservice "github.com/tutti-os/tutti/services/tuttid/service/modelbinding"
	modelgatewayservice "github.com/tutti-os/tutti/services/tuttid/service/modelgateway"
	modelplanservice "github.com/tutti-os/tutti/services/tuttid/service/modelplan"
	modelpolicyservice "github.com/tutti-os/tutti/services/tuttid/service/modelpolicy"
	preferencesservice "github.com/tutti-os/tutti/services/tuttid/service/preferences"
	reporterservice "github.com/tutti-os/tutti/services/tuttid/service/reporter"
	tuttiagentservice "github.com/tutti-os/tutti/services/tuttid/service/tuttiagent"
	tuttimodeactivationservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeactivation"
	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
	tuttimodeplanservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeplan"
	userprojectservice "github.com/tutti-os/tutti/services/tuttid/service/userproject"
	workspaceservice "github.com/tutti-os/tutti/services/tuttid/service/workspace"
	workspaceagentservice "github.com/tutti-os/tutti/services/tuttid/service/workspaceagent"
	tuttitypes "github.com/tutti-os/tutti/services/tuttid/types"
)

func buildDaemonAPI(
	ctx context.Context,
	store workspacedata.CatalogStore,
	analyticsReporter reporterservice.Reporter,
	browserService *browsersvc.Service,
	computerService *computersvc.Service,
	modelGateway *modelgatewayservice.Gateway,
	connectorRuntime agentservice.ConnectorRuntime,
	installTuttiModeWatchdog func(tuttimodeexecutionservice.Worker),
) (tuttiapi.DaemonAPI, *workspaceservice.AppCenterService, *agentdaemon.Runtime, *agentservice.ProviderAuthWatcher, error) {
	workspaceStore, _ := store.(workspacedata.WorkbenchStore)
	issueStore, _ := store.(workspaceissues.Store)
	tuttiModeExecutionStore, _ := store.(tuttimodeexecutionservice.Store)
	tuttiModeArchiveStore, _ := store.(tuttimodeexecutionservice.ArchiveStore)
	tuttiModeDeletionAdmissionStore, _ := store.(tuttimodeexecutionservice.SourceDeletionAdmissionStore)
	tuttiModeWakeStore, _ := store.(tuttimodeexecutionservice.WakeStore)
	tuttiModeReviewerActivity, ok := store.(tuttimodeexecutionservice.ReviewerActivityReader)
	if !ok {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf(
			"tutti mode reviewer activity reader is unavailable",
		)
	}
	preferencesStore, _ := store.(workspacedata.PreferencesStore)
	agentTargetStore, _ := store.(workspacedata.AgentTargetStore)
	managedCredentialsStore, _ := store.(workspacedata.ManagedCredentialsStore)
	modelPlansStore, _ := store.(workspacedata.ModelPlansStore)
	agentActivityRepo, _ := store.(workspacedata.AgentActivityStore)
	agentQuickPromptStore, _ := store.(workspacedata.AgentQuickPromptStore)
	agentProviderRuntimeSelectionStore, _ := store.(workspacedata.AgentProviderRuntimeSelectionStore)
	userProjectStore, _ := store.(workspacedata.UserProjectStore)
	appStore, _ := store.(workspacedata.AppStore)
	appFactoryStore, _ := store.(workspacedata.AppFactoryStore)
	workflowStore, _ := store.(tuttimodeplanservice.Store)
	tuttiModeActivationStore, _ := store.(tuttimodeactivationservice.Store)
	fileAdapter := workspacedata.LocalFilesAdapter{}
	issueAttachmentFiles, err := reconcileIssueAttachmentFiles(ctx, store)
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, err
	}

	events := eventstreamservice.NewService(eventstreamservice.DefaultCatalog(), nil)
	preferencesPublisher := eventstreamservice.DesktopPreferencesPublisher{Service: events}
	tuttiModeActivations := &tuttimodeactivationservice.Service{
		Store:     tuttiModeActivationStore,
		Publisher: eventstreamservice.TuttiModeActivationPublisher{Service: events},
	}
	preferences := &preferencesservice.Service{
		Store:                                  preferencesStore,
		Publisher:                              preferencesPublisher,
		AgentComposerDefaultsPublisher:         preferencesPublisher,
		AgentComposerDefaultsResolvedPublisher: preferencesPublisher,
	}
	agentTargets := agenttargetservice.Service{Store: agentTargetStore}
	agentRuntimeDir, err := tuttitypes.DefaultAgentRuntimeDir()
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("resolve agent runtime directory: %w", err)
	}
	agentExtensionBinDir, err := tuttitypes.DefaultAgentExecutableDir()
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("resolve agent extension executable directory: %w", err)
	}
	agentExtensionStateDir := tuttitypes.DefaultStateDir()
	agentSetupDiscovery := agentextensiondata.NewFileSetupDiscoveryDirectory(agentExtensionStateDir)
	agentExtensionManager := &agentextensionservice.Manager{
		Sources:           tuttitypes.ResolveAgentExtensionSources(),
		RuntimeInstallDir: agentRuntimeDir,
		RuntimeBinDir:     agentExtensionBinDir,
		Store:             agentTargetStore,
		Installations:     agentextensiondata.NewFileInstallationStore(agentExtensionStateDir),
		Discovery:         agentSetupDiscovery,
		Preferences:       preferencesStore,
		UserPathAdapter:   agentstatusservice.NewUserPathAdapter(),
	}
	preferences.RegisterChangeObserver(func(ctx context.Context, previous, current preferencesbiz.DesktopPreferences) {
		for _, reconcileErr := range agentExtensionManager.ReconcileDesktopPreferencesChange(ctx, previous, current) {
			payload, _ := json.Marshal(map[string]string{"error": reconcileErr.Error()})
			slog.Warn("agent_extension.reconcile_failed", "payload", string(payload))
		}
	})
	agentTargetInstallPlans := agentextensionservice.InstallPlanService{
		Manager: agentExtensionManager, Workspaces: store, Targets: agentTargetStore,
	}
	agentTargets.AvailabilityResolver = agentExtensionManager
	refreshAgentExtensionsInBackground := restoreAgentExtensionsForStartup(ctx, agentExtensionManager)
	managedCredentials := &managedcredentialsservice.Service{
		Store: managedCredentialsStore,
	}
	modelBindingsStore, _ := store.(workspacedata.AgentModelBindingsStore)
	modelPolicyStore, _ := store.(modelpolicyservice.Store)
	// Narrow cross-domain reads over biz types keep referential integrity
	// bidirectional without any modelbinding <-> modelpolicy service cycle:
	// bindings validate their policy link, and policy deletion checks bindings.
	bindingPolicyLookup, _ := store.(modelbindingservice.PolicyLookup)
	policyBindingReferences, _ := store.(modelpolicyservice.BindingReferenceReader)
	modelBindings := &modelbindingservice.Service{
		Store:    modelBindingsStore,
		Plans:    modelPlansStore,
		Targets:  agentTargetStore,
		Policies: bindingPolicyLookup,
	}
	modelPolicies := &modelpolicyservice.Service{
		Store:             modelPolicyStore,
		BindingReferences: policyBindingReferences,
	}
	modelConfigurationPublisher := eventstreamservice.AgentModelConfigurationPublisher{Service: events}
	workspaceAgentsStore, _ := store.(workspacedata.WorkspaceAgentsStore)
	workspaceAgents := &workspaceagentservice.Service{
		Store:      workspaceAgentsStore,
		Targets:    agentTargetStore,
		Plans:      modelPlansStore,
		Workspaces: store,
		Publisher:  modelConfigurationPublisher,
	}
	automationRulesStore, _ := store.(workspacedata.AutomationRulesStore)
	automationRules := &automationruleservice.Service{
		Store:     automationRulesStore,
		Agents:    workspaceAgents,
		Targets:   agentTargetStore,
		Usage:     automationRulesStore,
		Publisher: eventstreamservice.AgentAutomationRulesPublisher{Service: events},
	}
	modelPlans := &modelplanservice.Service{
		Store: modelPlansStore,
		// Plan deletion stays blocked while any consumer domain still points at
		// the plan: agent model bindings, model usage policies, and workspace agents.
		References: modelplanservice.CompositeReferenceResolver{modelBindings, modelPolicies, workspaceAgents},
	}
	collabRunsStore, _ := store.(workspacedata.CollaborationRunsStore)
	collabRuns := &collabrunservice.Service{
		Store:     collabRunsStore,
		Plans:     modelPlansStore,
		Completer: modelPlans,
		Publisher: eventstreamservice.AgentCollaborationPublisher{Service: events},
	}
	modelPolicies.ConfigureReviewAutomation(modelBindingsStore, nil, collabRuns, collabRuns)
	events.RegisterIntentHandler(
		eventstreamservice.TopicPreferencesDesktopUpdateRequested,
		eventstreamservice.NewPreferencesDesktopUpdateRequestedHandler(preferences),
	)
	events.RegisterIntentHandler(
		eventstreamservice.TopicPreferencesAgentComposerDefaultsPatchRequested,
		eventstreamservice.NewPreferencesAgentComposerDefaultsPatchRequestedHandler(preferences),
	)
	events.RegisterIntentHandler(
		eventstreamservice.TopicPreferencesAgentSessionLaunchModePatchRequested,
		eventstreamservice.NewPreferencesAgentSessionLaunchModePatchRequestedHandler(preferences),
	)
	agentActivityProjection := agentservice.NewActivityProjection(agentActivityRepo)
	modelPolicies.Sessions = modelPolicySessionTargetResolver{projection: agentActivityProjection}
	collabRuns.Timeline = agentservice.CollaborationTimelineReporter{Projection: agentActivityProjection}
	agentActivityProjection.SetAnalyticsReporter(analyticsReporter)
	agentActivityProjection.SetPublisher(eventstreamservice.AgentActivityPublisher{Service: events})
	if agentTargetResolver, ok := store.(agentservice.AgentTargetResolver); ok {
		agentActivityProjection.SetAgentTargetResolver(agentTargetResolver)
	}
	managedRuntimeResolver := managedruntime.DefaultResolver{}
	agentStatusService := agentstatusservice.NewService(agentstatusservice.ServiceDependencies{
		AnalyticsReporter:          analyticsReporter,
		ManagedRuntime:             managedRuntimeResolver,
		ClaudeCodeRuntimeDir:       filepath.Join(agentRuntimeDir, "claude-code"),
		UserCommandBinDir:          agentExtensionBinDir,
		CodexRuntimeSelectionStore: agentProviderRuntimeSelectionStore,
		UserPathAdapter:            agentstatusservice.NewUserPathAdapter(),
	})
	// Shared so a runtime auth failure (reporter side) surfaces in the status
	// probe (List side) — see agentRunOutcomeReporter.
	runOutcomes := agentStatusService.RunOutcomes
	accountService := accountservice.NewService("")
	mobileRemoteService, err := buildMobileRemoteService(
		agentExtensionStateDir,
		accountService,
		events,
	)
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, err
	}
	agentProcessComposition, err := buildAgentProcessComposition(
		resolveAgentSessionRecordingEnabled(ctx, preferences),
	)
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, err
	}
	replayComposition := agentCassetteReplayActive()
	agentHostMetadata := agentdaemon.HostMetadata{
		ClientInfo:       agentdaemon.ClientInfo{Name: "tutti-desktop", Title: "Tutti", Version: "0.1.0"},
		WorkspaceEnvName: "TUTTI_WORKSPACE_ID", OpenClawSessionKeyPrefix: "agent:main:tsh-",
	}
	agentTargetSetup := agentextensionservice.NewSetupService(context.Background())
	agentTargetSetup.Plans = agentTargetInstallPlans
	agentTargetSetup.Transport = agentProcessComposition.transport
	agentTargetSetup.Host = agentHostMetadata
	agentTargetSetup.Actions = agentextensiondata.NewFileSetupActionStore(agentExtensionStateDir)
	agentTargetSetup.Discovery = agentSetupDiscovery
	agentTargetSetup.AuthInvalidation = runOutcomes
	agentRuntimeConfig := agentdaemon.Config{
		Reporter: agentRunOutcomeReporter{
			DurableActivityReporter: agentActivityProjection,
			store:                   runOutcomes,
		},
		ProcessTransport: agentProcessComposition.transport,
		HostMetadata:     agentHostMetadata,
		AdapterResolver: agentextensionservice.RuntimeResolver{
			Manager: agentExtensionManager, Transport: agentProcessComposition.transport, Host: agentHostMetadata,
		},
		ProviderCommandResolver:    agentProviderCommandResolver(&agentStatusService),
		CommandNetworkAccessPolicy: tuttiDesktopCommandNetworkAccessPolicy,
		LiveSessionReaperLoad:      agentLiveSessionReaperLoader(preferences),
	}
	agentRuntimeConfig = applyAgentReplayRuntimeComposition(agentRuntimeConfig, replayComposition)
	agentRuntime, err := agentdaemon.NewRuntime(agentRuntimeConfig)
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("create agent runtime: %w", err)
	}
	preferences.RegisterChangeObserver(func(_ context.Context, previous, current preferencesbiz.DesktopPreferences) {
		if previous.AgentRuntimeKeepAliveEnabled != current.AgentRuntimeKeepAliveEnabled ||
			previous.AgentRuntimeIdleMinutes != current.AgentRuntimeIdleMinutes ||
			previous.AgentRuntimeMaxResident != current.AgentRuntimeMaxResident {
			agentRuntime.WakeLiveSessionReaper()
		}
	})
	agentRuntimePreparer := runtimeprep.NewDefaultPreparer(tuttitypes.DefaultStateDir())
	agentRuntimePreparer.RegisterProvider(runtimeprep.CodexPreparer{AuthProjector: runtimeprep.MutagenAuthFileProjector{StateDir: tuttitypes.DefaultStateDir()}})
	agentRuntimePreparer.RegisterProvider(tuttiagentservice.NewPreparer(tuttitypes.DefaultStateDir()))
	configureAgentRuntimeAvailability(agentRuntimePreparer, browserService, computerService)
	userProjectService := userprojectservice.Service{
		Store:     userProjectStore,
		Publisher: eventstreamservice.UserProjectPublisher{Service: events},
	}
	agentQuickPromptService := agentquickpromptservice.Service{
		Store:     agentQuickPromptStore,
		Publisher: eventstreamservice.AgentQuickPromptPublisher{Service: events},
	}
	agentRuntimeController := newAgentRuntimeAdapter(agentRuntime.Controller())
	agentRuntime.Controller().SetStreamEventObserver(agentRuntimeActivityEventBridge{
		publisher: eventstreamservice.AgentActivityPublisher{Service: events},
	})
	agentModelCapabilities := agentservice.NewModelCapabilitiesService()
	agentModelCatalog := agentservice.NewAgentModelCatalog()
	agentModelCatalog.ModelCapabilities = agentModelCapabilities
	agentModelCatalog.ProviderCommands = &agentStatusService
	agentSessionPurgeStore, ok := agentActivityRepo.(agenthost.SessionPurgeStore)
	if !ok {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("agent session purge store is unavailable")
	}
	var sessionDeletionGuard agenthost.SessionDeletionGuard
	if tuttiModeDeletionAdmissionStore != nil {
		sourceDeletionGuard := &tuttimodeexecutionservice.SourceDeletionGuard{
			Store:   tuttiModeDeletionAdmissionStore,
			Context: ctx,
		}
		if err := sourceDeletionGuard.Recover(ctx); err != nil {
			return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf(
				"recover source session deletion admissions: %w", err,
			)
		}
		sessionDeletionGuard = sourceDeletionGuard
	}
	goalReconcileInbox, ok := agentActivityRepo.(interface {
		agentservice.GoalReconcileInboxStore
		agentservice.GoalReconcileInboxWriter
	})
	if !ok {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("agent goal reconcile inbox store is unavailable")
	}
	agentActivityProjection.SetGoalReconcileInboxWriter(goalReconcileInbox)
	goalProvenanceLedger, ok := agentActivityRepo.(agentservice.GoalProvenanceLedgerStore)
	if !ok {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("agent goal provenance ledger store is unavailable")
	}
	agentActivityProjection.SetGoalProvenanceLedger(goalProvenanceLedger)
	workspaceIDs := agentWorkspaceIDs(store)
	promptAttachments := agentservice.PromptAttachmentStore{
		RootDir:       tuttitypes.DefaultStateDir(),
		SourceRootDir: filepath.Join(tuttitypes.DefaultStateDir(), "agent-prompt-assets"),
	}
	var agentRuntimePreparation runtimeprep.Preparer
	var browserUseAvailable func() bool
	var computerUseAvailable func() bool
	var availabilityChecker agentservice.ProviderAvailabilityChecker
	if !replayComposition {
		agentRuntimePreparation = agentRuntimePreparer
		browserUseAvailable = agentRuntimePreparer.BrowserUseAvailable
		computerUseAvailable = agentRuntimePreparer.ComputerUseAvailable
		availabilityChecker = agentservice.AgentStatusProviderAvailabilityChecker{
			Service: &agentStatusService,
		}
	} else {
		availabilityChecker = replayProviderAvailabilityChecker{}
	}
	canonicalStoreProvider, ok := store.(interface {
		AgentCanonicalStore() *agentstoresqlite.Store
	})
	if !ok || canonicalStoreProvider.AgentCanonicalStore() == nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("canonical agent store is unavailable")
	}
	historicalStateStore := canonicalStoreProvider.AgentCanonicalStore()
	if !replayComposition {
		// MCP Apps: tuttid resolves `_meta.ui` resources of the RnDMaster
		// contract's stdio MCP servers itself (the provider CLIs drop them),
		// snapshots the HTML into the agent store, and annotates tool_call
		// payloads with mcpApp. Replay never launches real MCP servers.
		agentRuntime.Controller().SetMCPAppResolver(&mcpappservice.Resolver{
			Transport: agentruntime.NewLocalProcessTransport(),
			Store:     historicalStateStore,
		})
	}
	canonicalHostStore := &agenthost.SQLiteWorkspaceStore{
		StoreForWorkspace: func(string) *agentstoresqlite.Store {
			return canonicalStoreProvider.AgentCanonicalStore()
		},
		Observer:             agentActivityProjection,
		InitializationPolicy: agentActivityProjection,
	}
	var agentSessionResourceReleaser agentservice.AgentSessionResourceReleaser
	if browserService != nil {
		agentSessionResourceReleaser = browserService
	}
	configureWorkspaceAgentProjection(agentActivityProjection, workspaceAgentsStore)
	sourceActivityObservers := &agentservice.TuttiModeSourceActivityObservers{}
	turnCancelObservers := &agentservice.TurnCancelObservers{}
	commitObserver, commitObservers := buildAgentCommitObserver(
		agentActivityProjection,
		agentProcessComposition.recorder != nil,
	)
	agentSessionConfig := agentservice.ServiceConfig{
		Runtime: agentservice.ServiceRuntimeConfig{
			Preparer:                 agentRuntimePreparation,
			Connector:                connectorRuntime,
			ModelGateway:             modelGateway,
			BrowserUseAvailable:      browserUseAvailable,
			ComputerUseAvailable:     computerUseAvailable,
			RuntimeOperationStore:    agentActivityRepo,
			RuntimeOperationOwner:    uuid.NewString(),
			StaleTurnSettler:         agentActivityProjection,
			GoalStateStore:           agentActivityRepo,
			GoalGenerationFenceStore: agentActivityRepo,
			GoalReconcileInboxStore:  goalReconcileInbox,
			GoalOperationOwner:       uuid.NewString(),
			ModelBindings:            modelBindingsStore,
			ModelPlans:               modelPlansStore,
		},
		Sessions: agentservice.ServiceSessionConfig{
			Initializer:       agentActivityProjection,
			Reader:            agentActivityProjection,
			DeletedSessions:   agentActivityProjection,
			PurgeStore:        agentSessionPurgeStore,
			DeletionGuard:     sessionDeletionGuard,
			UserProjectReader: userProjectService,
			MessageReader:     agentActivityProjection,
			TurnStore:         agentActivityRepo,
			TurnSummaryReader: agentActivityRepo,
			SubmitClaimStore:  agentActivityRepo,
		},
		Composer: agentservice.ServiceComposerConfig{
			AvailabilityChecker:         availabilityChecker,
			ModelCatalog:                replayAgentModelCatalog(replayComposition, agentProcessComposition, agentModelCatalog),
			ReplayMode:                  replayComposition,
			ModelCapabilities:           agentModelCapabilities,
			AgentTargetStore:            agentTargetStore,
			WorkspaceAgentResolver:      workspaceAgents,
			AgentComposerDefaultsReader: preferences,
			DesktopPreferencesReader:    preferences,
			ExtensionComposerProfiles: agentExtensionComposerProfileResolver{
				manager: agentExtensionManager,
			},
		},
		ExternalImport: agentservice.ServiceExternalImportConfig{
			Store:            agentActivityRepo,
			Catalog:          openExternalImportCatalog(),
			ParseConcurrency: 8,
		},
		Resources: agentservice.ServiceResourceConfig{
			AgentSessionResourceReleaser: agentSessionResourceReleaser,
			SessionDirectoryAllocator: agentservice.LocalSessionDirectoryAllocator{
				StateDir: tuttitypes.DefaultStateDir(),
			},
			WorktreeStateDir:      tuttitypes.DefaultStateDir(),
			WorkspaceIDs:          workspaceIDs,
			PromptAttachmentStore: promptAttachments,
		},
		Observers: agentservice.ServiceObserverConfig{
			AnalyticsReporter:              analyticsReporter,
			CommitObserver:                 commitObserver,
			RuntimeOperationEventPublisher: agentActivityProjection,
			TuttiModeActivations:           tuttiModeActivations,
			TuttiModeSourceActivity:        sourceActivityObservers,
			TurnCancelObserver:             turnCancelObservers,
		},
	}
	agentHostRuntime := &agenthostadapter.RuntimeController{
		Backend: agentRuntime.Controller(),
	}
	agentServiceComponents := agentservice.NewServiceComponents(
		agentRuntimeController,
		agentSessionConfig,
		canonicalHostStore,
	)
	agentHost := agentservice.NewApplicationHostWithPorts(
		agentServiceComponents.HostSupportPorts(),
		canonicalHostStore,
		canonicalStoreProvider.AgentCanonicalStore(),
		historicalStateStore,
		agentHostRuntime,
	)
	if agentHost == nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("compose agent host")
	}
	configureAgentProviderGoalAdoption(agentRuntime.Controller(), agentHost)
	// One owner for "is this session's turn slot free?": the runtime tells the
	// Host, and the Host admits the prompts parked behind it. Without this the
	// admission queue never drains and a busy-session prompt hangs forever.
	agentRuntime.Controller().SetTurnSlotObserver(agentruntime.TurnSlotObserverFunc(func(roomID, sessionID string) {
		agentHost.ObserveTurnSlotReleased(roomID, sessionID)
		agentRuntime.WakeLiveSessionReaper()
	}))
	agentActivityProjection.SetTurnForkabilityResolver(agentHost)
	agentSessionConfig.Host = agentservice.ServiceHostConfig{
		ApplicationHost: agentHost,
		Components:      agentServiceComponents,
	}
	agentSessionService := agentservice.NewService(agentRuntimeController, agentSessionConfig)
	if sqliteStore, ok := store.(*workspacedata.SQLiteStore); ok {
		agentSessionService.UseComposerLiveModelCacheStore(sqliteStore)
	}
	agentStatusService.OnProviderStatusInvalidated = agentSessionService.InvalidateProviderAvailabilityCache
	// A replaced extension runtime may advertise a different composer option
	// set (models, reasoning levels), so a (re)install must drop the provider's
	// cached projections — including the target-scoped runtime evidence the
	// sparse defaults patch validates against.
	agentExtensionManager.OnInstallationChanged = func(provider string) {
		agentSessionService.InvalidateLiveComposerModels(provider)
	}
	preferences.AgentComposerDefaultsValidator = agentComposerDefaultsValidatorAdapter{agents: agentSessionService}
	modelPlans.NativeSubscriptionProbe = modelPlanNativeSubscriptionProbe{Agents: agentSessionService}
	automationExecutor := &automationruleservice.DaemonExecutor{Agents: agentSessionService, Ledger: automationRulesStore}
	automationRules.Executor = automationExecutor
	automationRules.Sources = automationExecutor
	agentSessionRecordingService, err := buildAgentSessionRecordingService(
		store,
		agentProcessComposition.recorder,
		agentSessionService,
	)
	if err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, err
	}
	if commitObservers != nil && agentSessionRecordingService != nil {
		commitObservers.Add(agentSessionRecordingService)
	}
	configureAgentSessionRecordingObservers(
		agentActivityProjection,
		agentRuntimeController,
		agentSessionRecordingService,
	)
	var replaySemanticRuntime *agentsessionreplayservice.SemanticRuntime
	if replayComposition {
		sqliteWorkspaceStore, ok := store.(*workspacedata.SQLiteStore)
		if !ok {
			return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf(
				"agent session replay semantic store is unavailable",
			)
		}
		replaySemanticRuntime, err = prepareReplaySemanticRuntime(
			ctx,
			sqliteWorkspaceStore,
			agentHost,
			agentProcessComposition.replayRegistrations,
		)
		if err != nil {
			return tuttiapi.DaemonAPI{}, nil, nil, nil, err
		}
	}
	var providerObservers agentProviderObservationObservers
	if agentSessionRecordingService != nil {
		providerObservers = append(providerObservers, agentSessionRecordingService)
	}
	if replaySemanticRuntime != nil {
		providerObservers = append(providerObservers, replaySemanticRuntime)
	}
	if providerObserver := composeAgentProviderObservationObserver(
		providerObservers,
	); providerObserver != nil {
		agentRuntime.Controller().SetProviderObservationObserver(providerObserver)
	}
	// Host fixes startup order: durable runtime operations first, then goal
	// operations and reconcile inbox work, and only then stale turns.
	if err := agentHost.Recover(ctx); err != nil {
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("recover agent host: %w", err)
	}
	go func() {
		if err := agentHost.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "agent Host worker lifecycle stopped", "error", err)
		}
	}()
	var agentMaintenance *agentmaintenanceservice.Service
	if maintenanceState, ok := store.(agentmaintenanceservice.StateStore); ok {
		agentMaintenance = &agentmaintenanceservice.Service{
			Host: agentHost, Preferences: preferences, State: maintenanceState,
			Resources: agentSessionService,
			IsIdle:    agentSessionService.IdleForDataMaintenance,
		}
		if compactor, ok := store.(agentmaintenanceservice.DatabaseCompactor); ok {
			agentMaintenance.Compactor = compactor
		}
		go agentMaintenance.Run(ctx)
	}

	workspaceService := workspaceservice.CatalogService{
		Store:            store,
		PreferencesStore: preferencesStore,
	}
	issueRunLaunchGate := workspaceservice.NewIssueRunLaunchGate()
	issueRunCanceller := issueRunSessionCanceller{Host: agentHost, Sessions: agentSessionService}
	tuttiModeMainWakeOwner := "tuttid-main-wake:" + uuid.NewString()
	tuttiModeExecutions := &tuttimodeexecutionservice.Service{
		Store:                  tuttiModeExecutionStore,
		Archives:               tuttiModeArchiveStore,
		Wakes:                  tuttiModeWakeStore,
		ArchiveAutomationTurns: issueRunCanceller,
		MainWakeTargets: tuttiModeMainWakeAgentAdapter{
			Host:     agentHost,
			Sessions: agentSessionService,
		},
		ReviewerActivity: tuttiModeReviewerActivity,
	}
	tuttiModeExecutions.ReviewerTargets = tuttiModeReviewerAgentAdapter{
		Host:     agentHost,
		Sessions: agentSessionService,
	}
	tuttiModeSourceActivity := tuttiModeSourceActivityAdapter{
		Executions: tuttiModeExecutions,
	}
	sourceActivityObservers.Add(tuttiModeSourceActivity)
	tuttiModeMainWakeRecovery := &tuttiModeMainWakeReadyRecovery{Delegate: tuttiModeExecutions}
	issueService := workspaceservice.IssueManagerService{
		RunLauncher:                  issueRunAgentLauncher{Sessions: agentSessionService, Host: agentHost},
		RunLaunchGate:                issueRunLaunchGate,
		RunCancellationRequester:     issueRunCanceller,
		SourceSessionContextResolver: issueSourceSessionContextResolver{Sessions: agentActivityProjection},
		Publisher:                    eventstreamservice.WorkspaceIssuePublisher{Service: events},
		Store:                        issueStore,
		AttachmentFiles:              issueAttachmentFiles,
		AttachmentLaunchPins:         workspaceservice.NewIssueAttachmentLaunchPins(),
		AgentTargetReader:            agentTargetStore,
		PlanningTimeline:             agentservice.IssuePlanningTimelineReporter{Projection: agentActivityProjection},
		TuttiModeExecutions:          tuttiModeExecutions,
		MutationLocks:                workspaceservice.NewIssueMutationLocks(),
	}
	tuttiModePlans := &tuttimodeplanservice.Service{
		Store:             workflowStore,
		Revisions:         workspacedata.WorkflowRevisionFiles{StateDir: tuttitypes.DefaultStateDir()},
		Publisher:         eventstreamservice.WorkspaceWorkflowPublisher{Service: events},
		IssueMaterializer: tuttimodeplanservice.WorkspaceIssueMaterializer{Issues: &issueService},
		FeedbackDispatcher: &tuttiModePlanFeedbackDispatcher{
			Agents:    agentSessionService,
			TurnLinks: workflowStore,
		},
	}
	// Recover accepted Tutti Mode plans before buildDaemonAPI returns the
	// public service graph. This is a one-shot durable recovery pass, not a
	// background worker; deterministic Issue materialization makes retries
	// converge after a response or process loss.
	if workflowStore != nil {
		// The single-review flow retired the two-phase configuration
		// checkpoint. Cancel any legacy pending configuration reviews first so
		// stale review panels cannot reappear alongside the new flow. A
		// failure to retire one legacy row must not block daemon startup; the
		// next boot retries the remaining pending rows idempotently.
		if err := tuttiModePlans.RetireConfigurationReviewWorkflows(ctx); err != nil {
			slog.Warn("retire legacy Tutti Mode configuration reviews failed",
				"event", "tutti_mode_plan.configuration_review_retirement_failed",
				"error", err)
		}
		if err := tuttiModePlans.RecoverCreateIssueOperations(ctx); err != nil {
			return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("recover Tutti Mode plan operations: %w", err)
		}
	}
	issueExecutionCoordinator := &workspaceservice.IssueExecutionCoordinator{
		Issues:              &issueService,
		RunSessionCanceller: issueRunCanceller,
		SettlementReader:    issueRunSettlementReader{Host: agentHost},
	}
	issueService.RunReconciler = issueExecutionCoordinator
	tuttiModeExecutions.ArchiveRuns = issueExecutionCoordinator
	// A user's stop on a planning conversation cascades to every running task
	// run its accepted plan dispatched.
	turnCancelObservers.Add(issueExecutionCoordinator)
	issueService.ExecutionRecoveryQueue = workspaceservice.NewWorkspaceExecutionRecoveryQueue(workspaceservice.WorkspaceExecutionRecoveryQueueOptions{
		Context:  ctx,
		Delay:    3 * time.Second,
		Interval: 15 * time.Second,
		Reconcile: func(ctx context.Context, workspaceID string) (workspaceservice.WorkspaceExecutionRecoveryResult, error) {
			runResult, err := reconcileTuttiModeRunsAndMainWakes(
				ctx,
				workspaceID,
				tuttiModeMainWakeOwner,
				issueExecutionCoordinator.ReconcileIssueExecutions,
				tuttiModeMainWakeRecovery,
			)
			if err != nil {
				return workspaceservice.WorkspaceExecutionRecoveryResult{}, err
			}
			pendingArchives, err := tuttiModeExecutions.RecoverArchivesAndCount(ctx, workspaceID)
			return workspaceservice.WorkspaceExecutionRecoveryResult{
				Pending: runResult.RunningCount > runResult.CompletedCount ||
					pendingArchives > 0,
			}, err
		},
	})
	tuttiModeExecutions.ArchiveRecoveryQueue = issueService.ExecutionRecoveryQueue
	if tuttiModeArchiveStore != nil {
		workspaces, err := store.List(ctx)
		if err != nil {
			return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("list workspaces for Tutti archive recovery: %w", err)
		}
		for _, workspace := range workspaces {
			pendingArchives, err := tuttiModeExecutions.RecoverArchivesAndCount(ctx, workspace.ID)
			if err != nil {
				return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf(
					"recover Tutti archives for workspace %s: %w", workspace.ID, err,
				)
			}
			if pendingArchives > 0 {
				issueService.ExecutionRecoveryQueue.Enqueue(workspace.ID)
			}
		}
	}
	appShellAdapter := workspaceservice.NewPlatformAppShellAdapter()
	appCenterService := &workspaceservice.AppCenterService{
		Store:                 appStore,
		AppFactoryStore:       appFactoryStore,
		WorkspaceStore:        store,
		PreferencesStore:      preferencesStore,
		Runner:                &workspaceservice.AppRunner{RuntimeResolver: managedRuntimeResolver, ShellAdapter: appShellAdapter},
		ShellAdapter:          appShellAdapter,
		StateDir:              tuttitypes.DefaultStateDir(),
		HostTuttiVersion:      tuttitypes.ResolveAppVersion(),
		HostTuttiCapabilities: tuttitypes.ResolveAppCapabilities(),
		Publisher:             eventstreamservice.WorkspaceAppPublisher{Service: events},
	}
	if strings.TrimSpace(os.Getenv("RNDMASTER_DOCK_SETUP_BASE")) == "" {
		startManagedRuntimeProfilePreload(managedRuntimeResolver)
		go func() {
			// The packaged sidecar bundle no longer carries the native claude
			// binary; provision it up front so the first Claude session does not
			// pay the download. Sessions started before this completes fall back
			// to a PATH-installed claude (see runtimeprep.ClaudeCodePreparer).
			// The deadline bounds a stalled CDN/npm connection (the shared HTTP
			// client deliberately has no timeout) while leaving room for a large
			// fallback download through a slow proxy.
			preloadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			startedAt := time.Now()
			slog.Info("claude code binary preload started", "event", "tutti.claude_code_binary.preload_started")
			status, err := agentStatusService.EnsureClaudeCodeBinary(preloadCtx)
			if err != nil {
				slog.Warn("claude code binary preload failed", "event", "tutti.claude_code_binary.preload_failed", "durationMs", time.Since(startedAt).Milliseconds(), "error", err)
				return
			}
			slog.Info("claude code binary preload completed", "event", "tutti.claude_code_binary.preload_completed", "source", status.Source, "version", status.Version, "path", status.Path, "durationMs", time.Since(startedAt).Milliseconds())
		}()
	}
	appCLIRegistry := appclicli.NewRegistry(workspaceService, appCenterService)
	appCenterService.AppCLIRegistry = appCLIRegistry
	if err := appCenterService.InitBuiltinPackages(ctx); err != nil {
		agentRuntime.Close()
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("initialize builtin workspace apps: %w", err)
	}
	appFactoryService := &workspaceservice.AppFactoryService{
		Store:                 appFactoryStore,
		AppStore:              appStore,
		WorkspaceStore:        store,
		WorkspaceRootResolver: workspaceservice.FileService{Adapter: fileAdapter},
		AppCenter:             appCenterService,
		AgentSessionService:   agentSessionService,
		AgentTargetStore:      agentTargetStore,
		AgentMessageReader:    agentActivityProjection,
		AgentSessionReader:    agentActivityProjection,
		AgentSessionState:     agentActivityProjection,
		Runner:                &workspaceservice.AppRunner{RuntimeResolver: managedRuntimeResolver, ShellAdapter: appShellAdapter},
		ShellAdapter:          appShellAdapter,
		StateDir:              tuttitypes.DefaultStateDir(),
		Publisher:             eventstreamservice.WorkspaceAppFactoryPublisher{Service: events},
	}
	agentActivityProjection.SetRootTurnObserver(rootTurnObserverFanout{
		agentRuntimeController,
		tuttiModeSourceTurnActivityObserver{
			Activities: tuttiModeSourceActivity,
		},
		tuttiModeMainWakeTurnObserver{
			Settlements: tuttiModeExecutions,
			Queue:       issueService.ExecutionRecoveryQueue,
		},
		tuttiModeReviewerTurnObserver{
			Settlements: tuttiModeExecutions,
		},
	})
	agentActivityProjection.SetSessionMessageObserver(appFactoryService)
	sessionStateObservers := []agentservice.SessionStateObserverRegistration{
		{Observer: appFactoryService, RootTurnSettlements: agentservice.RootTurnSettlementsObserve},
		{Observer: modelPolicies, RootTurnSettlements: agentservice.RootTurnSettlementsObserve},
		{Observer: automationRules, RootTurnSettlements: agentservice.RootTurnSettlementsObserve},
		{Observer: issueExecutionCoordinator, RootTurnSettlements: agentservice.RootTurnSettlementsObserve},
	}
	if err := agentActivityProjection.ConfigureSessionStateObservers(sessionStateObservers...); err != nil {
		agentRuntime.Close()
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("configure agent session state observers: %w", err)
	}
	if _, err := appFactoryService.ReconcileInterruptedJobs(ctx); err != nil {
		agentRuntime.Close()
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("reconcile interrupted app factory jobs: %w", err)
	}
	workspaces, err := recoverIssueExecutionsAtStartup(
		ctx,
		workspaceService,
		issueExecutionCoordinator,
		tuttiModeExecutions,
	)
	if err != nil {
		agentRuntime.Close()
		return tuttiapi.DaemonAPI{}, nil, nil, nil, err
	}
	tuttiModeWatchdogWorker := newTuttiModeWatchdogWorker(
		ctx, tuttiModeExecutions, tuttiModeMainWakeOwner,
		func(ctx context.Context) ([]string, error) {
			summaries, err := workspaceService.List(ctx)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(summaries))
			for _, summary := range summaries {
				ids = append(ids, summary.ID)
			}
			return ids, nil
		},
	)
	if installTuttiModeWatchdog != nil {
		installTuttiModeWatchdog(tuttiModeWatchdogWorker)
	}
	cliRegistry, err := buildDaemonCLIRegistry(daemonCLIRegistryInput{
		Workspaces: workspaceService, Issues: &issueService,
		Apps: appCenterService, Events: events,
		ManagedCredentials: managedCredentials,
		AgentSessions:      agentSessionService, AgentTargets: agentTargets,
		AgentTargetSetup: agentTargetSetup,
		Preferences:      preferences, TuttiModePlans: tuttiModePlans,
		TuttiModeExecutions:  tuttiModeExecutions,
		TuttiModeActivations: tuttiModeActivations,
		Browser:              browserService, Computer: computerService,
		AppCommands: appCLIRegistry,
	})
	if err != nil {
		agentRuntime.Close()
		return tuttiapi.DaemonAPI{}, nil, nil, nil, fmt.Errorf("create cli registry: %w", err)
	}
	agentRuntimePreparer.CommandCatalog = runtimePrepCommandCatalog{Catalog: cliRegistry}

	terminalService := workspaceservice.NewTerminalService(workspaceservice.NewPlatformTerminalProcessFactory())
	tuttiAgentReadiness := configureReplayAwareTuttiAgentReadiness(
		replayComposition, accountService, &agentStatusService, agentTargets,
	)

	providerAuthWatcher := startAgentModelInvalidationAuthWatcher(
		replayComposition, agentModelCatalog, agentSessionService, events,
	)

	agentSessionReplayVerifier := composeAgentReplayVerifier(agentProcessComposition.replay, replaySemanticRuntime)

	return tuttiapi.DaemonAPI{
		AccountService:            accountService,
		MobileRemoteService:       mobileRemoteService,
		UserProjectService:        userProjectService,
		AgentQuickPromptService:   agentQuickPromptService,
		AgentTargetService:        agentTargets,
		AgentTargetSetupService:   agentTargetSetup,
		PreferencesService:        preferences,
		AgentMaintenanceService:   agentMaintenance,
		ManagedCredentialsService: managedCredentials,
		ModelPlanService:          modelPlans,
		WorkspaceAgentService:     workspaceAgents,
		AgentModelBindingService:  modelBindings,
		ModelPolicyService:        modelPolicies,
		CollaborationRunService:   collabRuns,
		AutomationRuleService:     automationRules,
		EventStreamService:        events,
		WorkspaceService:          workspaceService,
		WorkbenchService: workspaceservice.WorkbenchService{
			Store: workspaceStore,
			SnapshotReconciler: workspaceservice.TerminalWorkbenchSnapshotReconciler{
				TerminalService: terminalService,
			},
		},
		AppCenterService:  appCenterService,
		AppFactoryService: appFactoryService,
		FileService: workspaceservice.FileService{
			Adapter: fileAdapter,
		},
		AgentSessionService:          agentSessionService,
		AgentMCPAppResources:         historicalStateStore,
		AgentSessionRecordingService: agentSessionRecordingService,
		AgentSessionReplayVerifier:   agentSessionReplayVerifier,
		AgentStatusService:           replayAgentProviderStatusAPI(replayComposition, &agentStatusService),
		TuttiAgentReadiness:          tuttiAgentReadiness,
		TerminalService:              terminalService,
		IssueService:                 issueService,
		IssueExecutionService:        issueExecutionCoordinator,
		TuttiModePlanService:         tuttiModePlans,
		TuttiModeExecutionService:    tuttiModeExecutions,
		TuttiModeActivationService:   tuttiModeActivations,

		TuttiModeGoalReviewService: tuttiModeExecutions,

		CLIRegistry:       cliRegistry,
		AnalyticsReporter: analyticsReporter,
		OnListenerReady: func() {
			if refreshAgentExtensionsInBackground {
				startAgentExtensionBackgroundRefresh(agentExtensionManager)
			}
			tuttiModeMainWakeRecovery.MarkReady()
			for _, workspace := range workspaces {
				issueService.ExecutionRecoveryQueue.Enqueue(workspace.ID)
			}
		},
	}, appCenterService, agentRuntime, providerAuthWatcher, nil
}

// agentComposerDefaultsValidatorAdapter bridges the agent service's per-field
// patch validation onto the preferences service's interface. The two result
// types are structurally identical but deliberately independent: agent must not
// import service/preferences for a shape, and preferences must not import agent.
type agentComposerDefaultsValidatorAdapter struct {
	agents *agentservice.Service
}

func (a agentComposerDefaultsValidatorAdapter) ValidateAgentComposerDefaultsPatch(
	ctx context.Context,
	agentTargetID string,
	patch preferencesbiz.AgentComposerDefaultsPatch,
) (preferencesservice.AgentComposerDefaultsPatchValidation, error) {
	if a.agents == nil {
		return preferencesservice.AgentComposerDefaultsPatchValidation{}, fmt.Errorf("agent service is unavailable")
	}
	result, err := a.agents.ValidateAgentComposerDefaultsPatch(ctx, agentTargetID, patch)
	if err != nil {
		return preferencesservice.AgentComposerDefaultsPatchValidation{}, err
	}
	rejected := make([]preferencesservice.AgentComposerDefaultsPatchOutcome, 0, len(result.Rejected))
	for _, field := range result.Rejected {
		rejected = append(rejected, preferencesservice.AgentComposerDefaultsPatchOutcome{
			Field:      field.Field,
			ReasonCode: field.ReasonCode,
			Message:    field.Message,
		})
	}
	return preferencesservice.AgentComposerDefaultsPatchValidation{
		Applied:  result.Applied,
		Rejected: rejected,
	}, nil
}

func openExternalImportCatalog() agentservice.ExternalImportCatalog {
	store, err := externalimportcatalog.Open(externalimportcatalog.CatalogPath(tuttitypes.DefaultStateDir()))
	if err != nil {
		slog.Warn("external import catalog unavailable; using live scans", "error", err)
		return nil
	}
	return store
}

// agentLiveSessionReaperLoader 把「Agent 进程常驻」偏好翻成回收策略（DINTAL-5308）。
//
// 每次扫描前重读一次，所以用户在设置里改完立刻生效，不用重启 tuttid。
// 读不到偏好就退回默认（常驻 + 30 分钟）—— 回收是省内存的优化，
// 读一次配置失败不该变成「把用户正在聊的会话的进程掐了」。
func agentLiveSessionReaperLoader(
	preferences *preferencesservice.Service,
) func() agentdaemon.LiveSessionReaperConfig {
	return func() agentdaemon.LiveSessionReaperConfig {
		keepAlive := preferencesbiz.DefaultDesktopAgentRuntimeKeepAliveEnabled
		idleMinutes := preferencesbiz.DefaultDesktopAgentRuntimeIdleMinutes
		maxResident := preferencesbiz.DefaultDesktopAgentRuntimeMaxResident
		if preferences != nil {
			if current, err := preferences.Get(context.Background()); err == nil {
				keepAlive = current.AgentRuntimeKeepAliveEnabled
				idleMinutes = preferencesbiz.NormalizeDesktopAgentRuntimeIdleMinutes(current.AgentRuntimeIdleMinutes)
				maxResident = preferencesbiz.NormalizeDesktopAgentRuntimeMaxResident(current.AgentRuntimeMaxResident)
			}
		}
		idleAfter := time.Duration(idleMinutes) * time.Minute
		return agentdaemon.LiveSessionReaperConfig{
			IdleAfter: idleAfter,
			// 条数上限是 TTL 之外的第二道闸：TTL 拦不住「半小时里开一堆、每条都刚聊过」，
			// 那种情况下所有会话都还「新鲜」，内存却已经见底。超了就挤最久没说话的那条。
			MaxLiveSessions: maxResident,
			// 扫描频率定得比默认密，是因为「回合结束即放」这一档的时效由它决定：
			// 机器派工的会话等一整个默认间隔才被收走，就不叫「即放」了。
			// 单次扫描只是内存里过一遍 + 问适配器进程在不在，代价可以忽略。
			SweepInterval: 30 * time.Second,
			IdleAfterFor: func(session agentdaemon.Session) time.Duration {
				// visible=false = 机器派工（工作流 / 自动化 / CLI 派工）。它没有
				// 「下一句话」，留着进程纯占内存，所以恒按「不在跑回合就放」走，
				// 不看用户的常驻开关 —— 那个开关是给用户自己的会话用的。
				if !session.Visible {
					return 0
				}
				if !keepAlive {
					return 0
				}
				// 0 分钟 = 用户明说永不回收（拿内存换秒回）。
				if idleMinutes == 0 {
					return -1
				}
				return idleAfter
			},
		}
	}
}
