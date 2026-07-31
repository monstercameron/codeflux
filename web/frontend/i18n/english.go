package i18n

import "slices"

const (
	MsgProductName                    MessageID = "product.name"
	MsgCommonUnknown                  MessageID = "common.unknown"
	MsgActionRetry                    MessageID = "action.retry"
	MsgActionReload                   MessageID = "action.reload"
	MsgActionRetrySession             MessageID = "action.retry-session"
	MsgActionPause                    MessageID = "action.pause"
	MsgActionPauseTask                MessageID = "action.pause-task"
	MsgActionStop                     MessageID = "action.stop"
	MsgActionStopTask                 MessageID = "action.stop-task"
	MsgActionMore                     MessageID = "action.more"
	MsgActionMoreTaskActions          MessageID = "action.more-task-actions"
	MsgAppStartingTitle               MessageID = "app.starting.title"
	MsgAppStartingBody                MessageID = "app.starting.body"
	MsgAppUpdateRequiredTitle         MessageID = "app.update-required.title"
	MsgAppUpdateRequiredBody          MessageID = "app.update-required.body"
	MsgAppSessionExpiredTitle         MessageID = "app.session-expired.title"
	MsgAppCoordinatorUnavailableTitle MessageID = "app.coordinator-unavailable.title"
	MsgAppDatabaseUnavailableTitle    MessageID = "app.database-unavailable.title"
	MsgAppCannotStartTitle            MessageID = "app.cannot-start.title"
	MsgAppCannotStartBody             MessageID = "app.cannot-start.body"
	MsgAppPageNotFoundTitle           MessageID = "app.page-not-found.title"
	MsgAppPageNotFoundBody            MessageID = "app.page-not-found.body"
	MsgAppRenderFailureTitle          MessageID = "app.render-failure.title"
	MsgAppRenderFailureBody           MessageID = "app.render-failure.body"
	MsgAppRecoveryGuidance            MessageID = "app.recovery-guidance"
	MsgShellSkipMain                  MessageID = "shell.skip-main"
	MsgShellApplicationLabel          MessageID = "shell.application.label"
	MsgShellNoRepository              MessageID = "shell.no-repository"
	MsgShellConnection                MessageID = "shell.connection"
	MsgShellSessionSummaryLabel       MessageID = "shell.session-summary.label"
	MsgShellThreadsLabel              MessageID = "shell.threads.label"
	MsgShellConversationLabel         MessageID = "shell.conversation.label"
	MsgShellTaskGraphLabel            MessageID = "shell.task-graph.label"
	MsgShellTaskSummaryLabel          MessageID = "shell.task-summary.label"
	MsgShellWorkspaceSplitLabel       MessageID = "shell.workspace-split.label"
	MsgShellExpandThreadRail          MessageID = "shell.thread-rail.expand"
	MsgShellShowThreads               MessageID = "shell.thread-rail.show"
	MsgShellCollapseThreadRail        MessageID = "shell.thread-rail.collapse"
	MsgShellHideThreads               MessageID = "shell.thread-rail.hide"
	MsgWorkspaceTaskTitle             MessageID = "workspace.task-title"
	MsgWorkspaceRepositoryLoading     MessageID = "workspace.repository.loading"
	MsgWorkspaceBranchDirty           MessageID = "workspace.branch.dirty"
	MsgWorkspaceBranchClean           MessageID = "workspace.branch.clean"
	MsgRouteRepositoryChooserTitle    MessageID = "route.repository-chooser.title"
	MsgRouteRepositoryChooserBody     MessageID = "route.repository-chooser.body"
	MsgRouteMemoryTitle               MessageID = "route.memory.title"
	MsgRouteSettingsTitle             MessageID = "route.settings.title"
	MsgRouteDiagnosticsTitle          MessageID = "route.diagnostics.title"
	MsgRouteFirstRunTitle             MessageID = "route.first-run.title"
	MsgDataNotRequested               MessageID = "data.not-requested"
	MsgDataLoading                    MessageID = "data.loading"
	MsgDataEmptyTitle                 MessageID = "data.empty.title"
	MsgDataEmptyBody                  MessageID = "data.empty.body"
	MsgDataCount                      MessageID = "data.count"
	MsgDataStaleCount                 MessageID = "data.stale-count"
	MsgDataLoadFailureTitle           MessageID = "data.load-failure.title"
	MsgDataLoadFailureBody            MessageID = "data.load-failure.body"
	MsgDataAccessDeniedTitle          MessageID = "data.access-denied.title"
	MsgDataAccessDeniedBody           MessageID = "data.access-denied.body"
	MsgDataIncompatibleTitle          MessageID = "data.incompatible.title"
	MsgDataIncompatibleBody           MessageID = "data.incompatible.body"
	MsgDataDisconnected               MessageID = "data.disconnected"
	MsgDataUnknownState               MessageID = "data.unknown-state"
	MsgSubjectThreads                 MessageID = "subject.threads"
	MsgSubjectMessages                MessageID = "subject.messages"
	MsgSubjectTaskGraphNodes          MessageID = "subject.task-graph-nodes"
	MsgRepositoryRecentTitle          MessageID = "repository.recent.title"
	MsgRepositoryBrowseTitle          MessageID = "repository.browse.title"
	MsgRepositoryBrowseBody           MessageID = "repository.browse.body"
	MsgRepositoryBrowseAction         MessageID = "repository.browse.action"
	MsgRepositoryCanonicalTitle       MessageID = "repository.canonical.title"
	MsgRepositoryCanonicalBody        MessageID = "repository.canonical.body"
	MsgRepositoryWarningsTitle        MessageID = "repository.warnings.title"
	MsgRepositoryWarningsBody         MessageID = "repository.warnings.body"
	MsgMemoryListTitle                MessageID = "memory.list.title"
	MsgMemoryListBody                 MessageID = "memory.list.body"
	MsgMemoryDetailsTitle             MessageID = "memory.details.title"
	MsgMemoryDetailsBody              MessageID = "memory.details.body"
	MsgMemoryActionsTitle             MessageID = "memory.actions.title"
	MsgMemoryActionsBody              MessageID = "memory.actions.body"
	MsgSettingsProvidersTitle         MessageID = "settings.providers.title"
	MsgSettingsProvidersBody          MessageID = "settings.providers.body"
	MsgSettingsModelsTitle            MessageID = "settings.models.title"
	MsgSettingsModelsBody             MessageID = "settings.models.body"
	MsgSettingsPolicyTitle            MessageID = "settings.policy.title"
	MsgSettingsPolicyBody             MessageID = "settings.policy.body"
	MsgSettingsAppearanceTitle        MessageID = "settings.appearance.title"
	MsgSettingsAppearanceBody         MessageID = "settings.appearance.body"
	MsgSettingsDataTitle              MessageID = "settings.data.title"
	MsgSettingsDataBody               MessageID = "settings.data.body"
	MsgDiagnosticsHealthTitle         MessageID = "diagnostics.health.title"
	MsgDiagnosticsHealthBody          MessageID = "diagnostics.health.body"
	MsgDiagnosticsVersionsTitle       MessageID = "diagnostics.versions.title"
	MsgDiagnosticsVersionsBody        MessageID = "diagnostics.versions.body"
	MsgDiagnosticsTasksTitle          MessageID = "diagnostics.tasks.title"
	MsgDiagnosticsTasksBody           MessageID = "diagnostics.tasks.body"
	MsgDiagnosticsLogsTitle           MessageID = "diagnostics.logs.title"
	MsgDiagnosticsLogsBody            MessageID = "diagnostics.logs.body"
	MsgDiagnosticsBackupTitle         MessageID = "diagnostics.backup.title"
	MsgDiagnosticsBackupBody          MessageID = "diagnostics.backup.body"
	MsgDiagnosticsExportTitle         MessageID = "diagnostics.export.title"
	MsgDiagnosticsExportBody          MessageID = "diagnostics.export.body"
	MsgDiagnosticsTerminologyTitle    MessageID = "diagnostics.terminology.title"
	MsgDiagnosticsTerminologyBody     MessageID = "diagnostics.terminology.body"
	MsgFirstRunLocalPromiseTitle      MessageID = "first-run.local-promise.title"
	MsgFirstRunLocalPromiseBody       MessageID = "first-run.local-promise.body"
	MsgFirstRunProviderTitle          MessageID = "first-run.provider.title"
	MsgFirstRunProviderBody           MessageID = "first-run.provider.body"
	MsgFirstRunRepositoryTitle        MessageID = "first-run.repository.title"
	MsgFirstRunRepositoryBody         MessageID = "first-run.repository.body"
	MsgFirstRunPermissionsTitle       MessageID = "first-run.permissions.title"
	MsgFirstRunPermissionsBody        MessageID = "first-run.permissions.body"
	MsgFirstRunThreadTitle            MessageID = "first-run.thread.title"
	MsgFirstRunThreadBody             MessageID = "first-run.thread.body"
	MsgAnnouncementReconnecting       MessageID = "announcement.reconnecting"
	MsgAnnouncementOffline            MessageID = "announcement.offline"
	MsgAnnouncementConnectionLost     MessageID = "announcement.connection-lost"
	MsgAnnouncementConnectionRestored MessageID = "announcement.connection-restored"
	MsgAnnouncementApprovalRequired   MessageID = "announcement.approval-required"
	MsgAnnouncementTaskPaused         MessageID = "announcement.task-paused"
	MsgAnnouncementTaskCompleted      MessageID = "announcement.task-completed"
	MsgAnnouncementTaskFailed         MessageID = "announcement.task-failed"
	MsgAnnouncementValidationFailure  MessageID = "announcement.validation-failure"
	MsgAnnouncementRecoveryRequired   MessageID = "announcement.recovery-required"
	MsgPrimitiveWorking               MessageID = "primitive.working"
	MsgPrimitiveCopyCode              MessageID = "primitive.copy-code"
	MsgPrimitiveUnavailable           MessageID = "primitive.unavailable"
	MsgPrimitiveSplitterInstructions  MessageID = "primitive.splitter.instructions"
	MsgRoleUser                       MessageID = "role.user"
	MsgRoleAssistant                  MessageID = "role.assistant"
	MsgTermThread                     MessageID = "term.thread"
	MsgTermTask                       MessageID = "term.task"
	MsgTermAttempt                    MessageID = "term.attempt"
	MsgTermPlanRevision               MessageID = "term.plan-revision"
	MsgTermApproval                   MessageID = "term.approval"
	MsgTermCheckpoint                 MessageID = "term.checkpoint"
	MsgTermRecovery                   MessageID = "term.recovery"
)

var englishEntries = []Entry{
	{ID: MsgProductName, Text: "CodeFlux"},
	{ID: MsgCommonUnknown, Text: "Unknown"},
	{ID: MsgActionRetry, Text: "Retry"},
	{ID: MsgActionReload, Text: "Reload"},
	{ID: MsgActionRetrySession, Text: "Retry session"},
	{ID: MsgActionPause, Text: "Pause"},
	{ID: MsgActionPauseTask, Text: "Pause task"},
	{ID: MsgActionStop, Text: "Stop"},
	{ID: MsgActionStopTask, Text: "Stop task"},
	{ID: MsgActionMore, Text: "More"},
	{ID: MsgActionMoreTaskActions, Text: "More task actions"},
	{ID: MsgAppStartingTitle, Text: "Starting CodeFlux"},
	{ID: MsgAppStartingBody, Text: "Checking the local coordinator and restoring your session."},
	{ID: MsgAppUpdateRequiredTitle, Text: "Update required"},
	{ID: MsgAppUpdateRequiredBody, Text: "Reload CodeFlux after the application and coordinator versions match."},
	{ID: MsgAppSessionExpiredTitle, Text: "Session expired"},
	{ID: MsgAppCoordinatorUnavailableTitle, Text: "Coordinator unavailable"},
	{ID: MsgAppDatabaseUnavailableTitle, Text: "Database unavailable"},
	{ID: MsgAppCannotStartTitle, Text: "CodeFlux cannot start"},
	{ID: MsgAppCannotStartBody, Text: "The application entered an unknown startup state."},
	{ID: MsgAppPageNotFoundTitle, Text: "Page not found"},
	{ID: MsgAppPageNotFoundBody, Text: "Choose a repository to continue."},
	{ID: MsgAppRenderFailureTitle, Text: "This view could not render"},
	{ID: MsgAppRenderFailureBody, Text: "Your route and unsent draft were preserved. Retry the view."},
	{ID: MsgAppRecoveryGuidance, Text: "Try again or open diagnostics."},
	{ID: MsgShellSkipMain, Text: "Skip to main content"},
	{ID: MsgShellApplicationLabel, Text: "Application"},
	{ID: MsgShellNoRepository, Text: "No repository selected"},
	{ID: MsgShellConnection, Text: "Connection: {state}"},
	{ID: MsgShellSessionSummaryLabel, Text: "Session summary"},
	{ID: MsgShellThreadsLabel, Text: "Threads"},
	{ID: MsgShellConversationLabel, Text: "Conversation"},
	{ID: MsgShellTaskGraphLabel, Text: "Task graph"},
	{ID: MsgShellTaskSummaryLabel, Text: "Task summary"},
	{ID: MsgShellWorkspaceSplitLabel, Text: "Resize conversation and task graph"},
	{ID: MsgShellExpandThreadRail, Text: "Expand thread rail"},
	{ID: MsgShellShowThreads, Text: "Show threads"},
	{ID: MsgShellCollapseThreadRail, Text: "Collapse thread rail"},
	{ID: MsgShellHideThreads, Text: "Hide threads"},
	{ID: MsgWorkspaceTaskTitle, Text: "Task workspace"},
	{ID: MsgWorkspaceRepositoryLoading, Text: "Repository context is loading"},
	{ID: MsgWorkspaceBranchDirty, Text: "{branch} / uncommitted changes"},
	{ID: MsgWorkspaceBranchClean, Text: "{branch} / clean"},
	{ID: MsgRouteRepositoryChooserTitle, Text: "Choose a repository"},
	{ID: MsgRouteRepositoryChooserBody, Text: "Select an authorized repository to open its task workspace."},
	{ID: MsgRouteMemoryTitle, Text: "Memory"},
	{ID: MsgRouteSettingsTitle, Text: "Settings"},
	{ID: MsgRouteDiagnosticsTitle, Text: "Diagnostics"},
	{ID: MsgRouteFirstRunTitle, Text: "Welcome to CodeFlux"},
	{ID: MsgDataNotRequested, Text: "Not requested"},
	{ID: MsgDataLoading, Text: "Loading {subject}"},
	{ID: MsgDataEmptyTitle, Text: "No {subject}"},
	{ID: MsgDataEmptyBody, Text: "There is nothing to show yet."},
	{ID: MsgDataCount, One: "{count} {subject}", Other: "{count} {subject}"},
	{ID: MsgDataStaleCount, One: "Showing {count} {subject}; updates are delayed", Other: "Showing {count} {subject}; updates are delayed"},
	{ID: MsgDataLoadFailureTitle, Text: "Could not load {subject}"},
	{ID: MsgDataLoadFailureBody, Text: "Retry is available."},
	{ID: MsgDataAccessDeniedTitle, Text: "Access denied"},
	{ID: MsgDataAccessDeniedBody, Text: "Access to {subject} is denied"},
	{ID: MsgDataIncompatibleTitle, Text: "Update required"},
	{ID: MsgDataIncompatibleBody, Text: "{subject} require a compatible CodeFlux version"},
	{ID: MsgDataDisconnected, Text: "{subject} are offline; reconnecting"},
	{ID: MsgDataUnknownState, Text: "Unknown {subject} state"},
	{ID: MsgSubjectThreads, Text: "threads"},
	{ID: MsgSubjectMessages, Text: "messages"},
	{ID: MsgSubjectTaskGraphNodes, Text: "task graph nodes"},
	{ID: MsgRepositoryRecentTitle, Text: "Recent valid workspaces"},
	{ID: MsgRepositoryBrowseTitle, Text: "Browse or open"},
	{ID: MsgRepositoryBrowseBody, Text: "Open an authorized local repository."},
	{ID: MsgRepositoryBrowseAction, Text: "Browse repositories"},
	{ID: MsgRepositoryCanonicalTitle, Text: "Canonical path result"},
	{ID: MsgRepositoryCanonicalBody, Text: "CodeFlux confirms the canonical path after authorization."},
	{ID: MsgRepositoryWarningsTitle, Text: "Warnings and recovery"},
	{ID: MsgRepositoryWarningsBody, Text: "Unavailable repositories stay closed and can be retried."},
	{ID: MsgMemoryListTitle, Text: "Memory list"},
	{ID: MsgMemoryListBody, Text: "Durable memory entries appear here with source labels."},
	{ID: MsgMemoryDetailsTitle, Text: "Memory details"},
	{ID: MsgMemoryDetailsBody, Text: "Select an entry to inspect provenance and scope."},
	{ID: MsgMemoryActionsTitle, Text: "Memory actions"},
	{ID: MsgMemoryActionsBody, Text: "Authorized memory actions appear here."},
	{ID: MsgSettingsProvidersTitle, Text: "Providers"},
	{ID: MsgSettingsProvidersBody, Text: "Provider connections and capabilities."},
	{ID: MsgSettingsModelsTitle, Text: "Models"},
	{ID: MsgSettingsModelsBody, Text: "Model and effort defaults."},
	{ID: MsgSettingsPolicyTitle, Text: "Policy"},
	{ID: MsgSettingsPolicyBody, Text: "Approval, budget, and execution policy."},
	{ID: MsgSettingsAppearanceTitle, Text: "Appearance"},
	{ID: MsgSettingsAppearanceBody, Text: "Theme, density, and motion preferences."},
	{ID: MsgSettingsDataTitle, Text: "Data"},
	{ID: MsgSettingsDataBody, Text: "Backup, retention, and local data controls."},
	{ID: MsgDiagnosticsHealthTitle, Text: "Health"},
	{ID: MsgDiagnosticsHealthBody, Text: "Coordinator and database health."},
	{ID: MsgDiagnosticsVersionsTitle, Text: "Versions"},
	{ID: MsgDiagnosticsVersionsBody, Text: "Application, API, schema, and frontend versions."},
	{ID: MsgDiagnosticsTasksTitle, Text: "Tasks"},
	{ID: MsgDiagnosticsTasksBody, Text: "Active Task and Attempt summaries."},
	{ID: MsgDiagnosticsLogsTitle, Text: "Logs"},
	{ID: MsgDiagnosticsLogsBody, Text: "Redacted local diagnostic logs."},
	{ID: MsgDiagnosticsBackupTitle, Text: "Backup"},
	{ID: MsgDiagnosticsBackupBody, Text: "Local backup status and recovery guidance."},
	{ID: MsgDiagnosticsExportTitle, Text: "Export"},
	{ID: MsgDiagnosticsExportBody, Text: "Create a redacted support export."},
	{ID: MsgDiagnosticsTerminologyTitle, Text: "Terminology"},
	{ID: MsgDiagnosticsTerminologyBody, Text: "A Thread contains conversation. A Task is durable work. An Attempt is one execution. A Plan revision changes the approach. An Approval authorizes a gated action. A Checkpoint is restorable state. Recovery resumes safely."},
	{ID: MsgFirstRunLocalPromiseTitle, Text: "Local-first promise"},
	{ID: MsgFirstRunLocalPromiseBody, Text: "Your coordinator and durable state stay local."},
	{ID: MsgFirstRunProviderTitle, Text: "Provider"},
	{ID: MsgFirstRunProviderBody, Text: "Connect an authorized model provider."},
	{ID: MsgFirstRunRepositoryTitle, Text: "Repository"},
	{ID: MsgFirstRunRepositoryBody, Text: "Choose an authorized repository."},
	{ID: MsgFirstRunPermissionsTitle, Text: "Worktree and permissions"},
	{ID: MsgFirstRunPermissionsBody, Text: "Review file and command boundaries."},
	{ID: MsgFirstRunThreadTitle, Text: "First Thread"},
	{ID: MsgFirstRunThreadBody, Text: "Create the first Thread after setup is complete."},
	{ID: MsgAnnouncementReconnecting, Text: "Live updates are reconnecting"},
	{ID: MsgAnnouncementOffline, Text: "CodeFlux is offline"},
	{ID: MsgAnnouncementConnectionLost, Text: "Connection lost"},
	{ID: MsgAnnouncementConnectionRestored, Text: "Connection restored"},
	{ID: MsgAnnouncementApprovalRequired, Text: "Approval required"},
	{ID: MsgAnnouncementTaskPaused, Text: "Task paused"},
	{ID: MsgAnnouncementTaskCompleted, Text: "Task completed"},
	{ID: MsgAnnouncementTaskFailed, Text: "Task failed"},
	{ID: MsgAnnouncementValidationFailure, Text: "Validation failed"},
	{ID: MsgAnnouncementRecoveryRequired, Text: "Recovery required"},
	{ID: MsgPrimitiveWorking, Text: "Working… {label}"},
	{ID: MsgPrimitiveCopyCode, Text: "Copy code"},
	{ID: MsgPrimitiveUnavailable, Text: "{component} unavailable: {message}"},
	{ID: MsgPrimitiveSplitterInstructions, Text: "Use arrow keys to resize; double-click to collapse or restore"},
	{ID: MsgRoleUser, Text: "User"},
	{ID: MsgRoleAssistant, Text: "Assistant"},
	{ID: MsgTermThread, Text: "Thread"},
	{ID: MsgTermTask, Text: "Task"},
	{ID: MsgTermAttempt, Text: "Attempt"},
	{ID: MsgTermPlanRevision, Text: "Plan revision"},
	{ID: MsgTermApproval, Text: "Approval"},
	{ID: MsgTermCheckpoint, Text: "Checkpoint"},
	{ID: MsgTermRecovery, Text: "Recovery"},
}

var (
	builtInEnglishCatalog  = mustCatalog(LocaleEnglishUnitedStates, englishEntries)
	builtInEnglishRegistry = mustRegistry(LocaleEnglishUnitedStates, builtInEnglishCatalog)
)

func mustCatalog(locale Locale, entries []Entry) Catalog {
	catalog, err := NewCatalog(locale, entries)
	if err != nil {
		panic(err)
	}
	return catalog
}

func mustRegistry(defaultLocale Locale, catalogs ...Catalog) Registry {
	registry, err := NewRegistry(defaultLocale, catalogs...)
	if err != nil {
		panic(err)
	}
	return registry
}

func EnglishCatalog() Catalog   { return builtInEnglishCatalog }
func EnglishRegistry() Registry { return builtInEnglishRegistry }

// CurrentShellMessageIDs returns the stable IDs required by the current M16
// product shell and its shared feedback primitives.
func CurrentShellMessageIDs() []MessageID {
	ids := make([]MessageID, 0, len(englishEntries))
	for _, entry := range englishEntries {
		ids = append(ids, entry.ID)
	}
	return slices.Clone(ids)
}
