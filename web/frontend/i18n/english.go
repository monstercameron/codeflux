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
	MsgKindCode                       MessageID = "kind.code"
	MsgKindTest                       MessageID = "kind.test"
	MsgKindPlan                       MessageID = "kind.plan"
	MsgKindEvidence                   MessageID = "kind.evidence"
	MsgKindMemory                     MessageID = "kind.memory"
	MsgKindForecast                   MessageID = "kind.forecast"
	MsgKindExecution                  MessageID = "kind.execution"
	MsgKindValidation                 MessageID = "kind.validation"

	// Project-memory inspection and correction UI (M21-090..099).
	MsgMemoryInspectorUnavailable              MessageID = "memory-inspector.error.title"
	MsgMemoryKindRepositoryFact                MessageID = "memory-inspector.kind.repository-fact"
	MsgMemoryKindReviewedCommand               MessageID = "memory-inspector.kind.reviewed-command"
	MsgMemoryKindFileToTestMapping             MessageID = "memory-inspector.kind.file-to-test-mapping"
	MsgMemoryKindRepositoryConvention          MessageID = "memory-inspector.kind.repository-convention"
	MsgMemoryKindAcceptedRegressionCase        MessageID = "memory-inspector.kind.accepted-regression-case"
	MsgMemoryKindExecutionRecipe               MessageID = "memory-inspector.kind.execution-recipe"
	MsgMemoryKindExecutableAtomReference       MessageID = "memory-inspector.kind.executable-atom-reference"
	MsgMemoryKindObservationHypothesis         MessageID = "memory-inspector.kind.observation-hypothesis"
	MsgMemoryMaturityCandidate                 MessageID = "memory-inspector.maturity.candidate"
	MsgMemoryMaturityValidated                 MessageID = "memory-inspector.maturity.validated"
	MsgMemoryMaturityPreferredForExperiment    MessageID = "memory-inspector.maturity.preferred-for-experiment"
	MsgMemoryMaturityQuarantined               MessageID = "memory-inspector.maturity.quarantined"
	MsgMemoryMaturityInvalidated               MessageID = "memory-inspector.maturity.invalidated"
	MsgMemoryMaturityRetired                   MessageID = "memory-inspector.maturity.retired"
	MsgMemoryFilterKindLabel                   MessageID = "memory-inspector.filter.kind.label"
	MsgMemoryFilterMaturityLabel               MessageID = "memory-inspector.filter.maturity.label"
	MsgMemoryFilterValidityLabel               MessageID = "memory-inspector.filter.validity.label"
	MsgMemoryFilterLastConfirmationLabel       MessageID = "memory-inspector.filter.last-confirmation.label"
	MsgMemoryFilterClear                       MessageID = "memory-inspector.filter.clear"
	MsgMemoryFilterValidityAny                 MessageID = "memory-inspector.filter.validity.any"
	MsgMemoryFilterValidityCurrentlyValid      MessageID = "memory-inspector.filter.validity.currently-valid"
	MsgMemoryFilterValidityNotYetValidated     MessageID = "memory-inspector.filter.validity.not-yet-validated"
	MsgMemoryFilterValidityNoLongerValid       MessageID = "memory-inspector.filter.validity.no-longer-valid"
	MsgMemoryFilterConfirmationAny             MessageID = "memory-inspector.filter.last-confirmation.any"
	MsgMemoryFilterConfirmationWithin7Days     MessageID = "memory-inspector.filter.last-confirmation.within-7-days"
	MsgMemoryFilterConfirmationWithin30Days    MessageID = "memory-inspector.filter.last-confirmation.within-30-days"
	MsgMemoryFilterConfirmationOlderThan30Days MessageID = "memory-inspector.filter.last-confirmation.older-than-30-days"
	MsgMemoryFilterConfirmationNeverConfirmed  MessageID = "memory-inspector.filter.last-confirmation.never-confirmed"
	MsgMemoryListTitleLabel                    MessageID = "memory-inspector.list.title"
	MsgMemoryListAriaLabel                     MessageID = "memory-inspector.list.aria-label"
	MsgMemoryListLoading                       MessageID = "memory-inspector.list.loading"
	MsgMemoryListEmptyTitle                    MessageID = "memory-inspector.list.empty.title"
	MsgMemoryListEmptyBody                     MessageID = "memory-inspector.list.empty.body"
	MsgMemoryListErrorTitle                    MessageID = "memory-inspector.list.error.title"
	MsgMemoryListErrorBody                     MessageID = "memory-inspector.list.error.body"
	MsgMemoryListDisconnectedTitle             MessageID = "memory-inspector.list.disconnected.title"
	MsgMemoryListDisconnectedBody              MessageID = "memory-inspector.list.disconnected.body"
	MsgMemoryListRetry                         MessageID = "memory-inspector.list.retry"
	MsgMemoryListRowLastConfirmed              MessageID = "memory-inspector.list.row.last-confirmed"
	MsgMemoryListRowNeverConfirmed             MessageID = "memory-inspector.list.row.never-confirmed"
	MsgMemorySelectedNoneTitle                 MessageID = "memory-inspector.selected.none.title"
	MsgMemorySelectedNoneBody                  MessageID = "memory-inspector.selected.none.body"
	MsgMemorySelectedLoading                   MessageID = "memory-inspector.selected.loading"
	MsgMemorySelectedErrorTitle                MessageID = "memory-inspector.selected.error.title"
	MsgMemorySelectedErrorBody                 MessageID = "memory-inspector.selected.error.body"
	MsgMemorySelectedDeniedTitle               MessageID = "memory-inspector.selected.denied.title"
	MsgMemorySelectedDeniedBody                MessageID = "memory-inspector.selected.denied.body"
	MsgMemorySelectedIncompatibleTitle         MessageID = "memory-inspector.selected.incompatible.title"
	MsgMemorySelectedIncompatibleBody          MessageID = "memory-inspector.selected.incompatible.body"
	MsgMemorySelectedDisconnectedTitle         MessageID = "memory-inspector.selected.disconnected.title"
	MsgMemorySelectedDisconnectedBody          MessageID = "memory-inspector.selected.disconnected.body"
	MsgMemorySelectedRetry                     MessageID = "memory-inspector.selected.retry"
	MsgMemoryDetailIdentityTitle               MessageID = "memory-inspector.detail.identity.title"
	MsgMemoryDetailIdentityArtifactID          MessageID = "memory-inspector.detail.identity.artifact-id"
	MsgMemoryDetailIdentityRevisionID          MessageID = "memory-inspector.detail.identity.revision-id"
	MsgMemoryDetailIdentityScope               MessageID = "memory-inspector.detail.identity.scope"
	MsgMemoryDetailIdentitySupersedes          MessageID = "memory-inspector.detail.identity.supersedes"
	MsgMemoryDetailIdentityFromCorrection      MessageID = "memory-inspector.detail.identity.from-correction"
	MsgMemoryDetailLineageTitle                MessageID = "memory-inspector.detail.lineage.title"
	MsgMemoryDetailLineageDerivedFrom          MessageID = "memory-inspector.detail.lineage.derived-from"
	MsgMemoryDetailLineageInfluencedBy         MessageID = "memory-inspector.detail.lineage.influenced-by"
	MsgMemoryDetailLineageOrigin               MessageID = "memory-inspector.detail.lineage.origin"
	MsgMemoryDetailLineageRoots                MessageID = "memory-inspector.detail.lineage.roots"
	MsgMemoryDetailLineageEmpty                MessageID = "memory-inspector.detail.lineage.empty"
	MsgMemoryDetailLineageUnknownAncestor      MessageID = "memory-inspector.detail.lineage.unknown-ancestor"
	MsgMemoryDetailLineageOriginUnknown        MessageID = "memory-inspector.detail.lineage.origin.unknown"
	MsgMemoryDetailLineageOriginIsSelf         MessageID = "memory-inspector.detail.lineage.origin.is-self"
	MsgMemoryDetailLineageRootsUnknown         MessageID = "memory-inspector.detail.lineage.roots.unknown"
	MsgMemoryDetailLineageRootsIsSelf          MessageID = "memory-inspector.detail.lineage.roots.is-self"
	MsgMemoryDetailEpisodesTitle               MessageID = "memory-inspector.detail.episodes.title"
	MsgMemoryDetailEpisodesEmpty               MessageID = "memory-inspector.detail.episodes.empty"
	MsgMemoryDetailEpisodeUnknown              MessageID = "memory-inspector.detail.episodes.unknown"
	MsgMemoryDetailBindingsTitle               MessageID = "memory-inspector.detail.bindings.title"
	MsgMemoryDetailBindingsUnknown             MessageID = "memory-inspector.detail.bindings.unknown"
	MsgMemoryDetailBindingsExactRevision       MessageID = "memory-inspector.detail.bindings.exact-revision"
	MsgMemoryDetailBindingsValidFrom           MessageID = "memory-inspector.detail.bindings.valid-from"
	MsgMemoryDetailBindingsValidUntil          MessageID = "memory-inspector.detail.bindings.valid-until"
	MsgMemoryDetailBindingsUnmodeled           MessageID = "memory-inspector.detail.bindings.unmodeled"
	MsgMemoryDetailApplicabilityTitle          MessageID = "memory-inspector.detail.applicability.title"
	MsgMemoryDetailApplicabilityUnmodeled      MessageID = "memory-inspector.detail.applicability.unmodeled"
	MsgMemoryDetailHistoryTitle                MessageID = "memory-inspector.detail.history.title"
	MsgMemoryDetailHistoryRetrieved            MessageID = "memory-inspector.detail.history.retrieved"
	MsgMemoryDetailHistoryInfluenced           MessageID = "memory-inspector.detail.history.influenced"
	MsgMemoryDetailHistoryRejected             MessageID = "memory-inspector.detail.history.rejected"
	MsgMemoryDetailHistoryEmpty                MessageID = "memory-inspector.detail.history.empty"
	MsgMemoryCorrectionTitle                   MessageID = "memory-inspector.correction.title"
	MsgMemoryCorrectionExplain                 MessageID = "memory-inspector.correction.explain"
	MsgMemoryCorrectionFieldStatement          MessageID = "memory-inspector.correction.field.statement"
	MsgMemoryCorrectionFieldCommand            MessageID = "memory-inspector.correction.field.command"
	MsgMemoryCorrectionFieldTestPaths          MessageID = "memory-inspector.correction.field.test-paths"
	MsgMemoryCorrectionFieldExpectedOutcome    MessageID = "memory-inspector.correction.field.expected-outcome"
	MsgMemoryCorrectionFieldPlanSummary        MessageID = "memory-inspector.correction.field.plan-summary"
	MsgMemoryCorrectionFieldCanonicalName      MessageID = "memory-inspector.correction.field.canonical-name"
	MsgMemoryCorrectionFieldCommandLines       MessageID = "memory-inspector.correction.field.command-lines"
	MsgMemoryCorrectionFieldTestPathsLines     MessageID = "memory-inspector.correction.field.test-paths-lines"
	MsgMemoryCorrectionListFieldExplain        MessageID = "memory-inspector.correction.list-field.explain"
	MsgMemoryCorrectionSubmit                  MessageID = "memory-inspector.correction.submit"
	MsgMemoryCorrectionInvalid                 MessageID = "memory-inspector.correction.invalid"
	MsgMemoryQuarantineTitle                   MessageID = "memory-inspector.quarantine.title"
	MsgMemoryQuarantineExplain                 MessageID = "memory-inspector.quarantine.explain"
	MsgMemoryQuarantineConfirm                 MessageID = "memory-inspector.quarantine.confirm"
	MsgMemoryQuarantineSubmit                  MessageID = "memory-inspector.quarantine.submit"
	MsgMemoryQuarantineUnavailable             MessageID = "memory-inspector.quarantine.unavailable"
	MsgMemoryInvalidationTitle                 MessageID = "memory-inspector.invalidation.title"
	MsgMemoryInvalidationExplain               MessageID = "memory-inspector.invalidation.explain"
	MsgMemoryInvalidationReasonLabel           MessageID = "memory-inspector.invalidation.reason-label"
	MsgMemoryInvalidationSubmit                MessageID = "memory-inspector.invalidation.submit"
	MsgMemoryInvalidationUnavailable           MessageID = "memory-inspector.invalidation.unavailable"
	MsgMemoryDeletionTitle                     MessageID = "memory-inspector.deletion.title"
	MsgMemoryDeletionExplain                   MessageID = "memory-inspector.deletion.explain"
	MsgMemoryDeletionPreviewLoading            MessageID = "memory-inspector.deletion.preview-loading"
	MsgMemoryDeletionDirectDependents          MessageID = "memory-inspector.deletion.direct-dependents"
	MsgMemoryDeletionContextuallyInfluenced    MessageID = "memory-inspector.deletion.contextually-influenced"
	MsgMemoryDeletionNone                      MessageID = "memory-inspector.deletion.none"
	MsgMemoryDeletionAcknowledge               MessageID = "memory-inspector.deletion.acknowledge"
	MsgMemoryDeletionAcknowledgedList          MessageID = "memory-inspector.deletion.acknowledged-list"
	MsgMemoryDeletionSubmit                    MessageID = "memory-inspector.deletion.submit"
	MsgMemoryExportTitle                       MessageID = "memory-inspector.export.title"
	MsgMemoryExportExplain                     MessageID = "memory-inspector.export.explain"
	MsgMemoryExportEmpty                       MessageID = "memory-inspector.export.empty"
	MsgMemoryExportSubmit                      MessageID = "memory-inspector.export.submit"
	MsgMemoryExportSelect                      MessageID = "memory-inspector.export.select"
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
	{ID: MsgKindCode, Text: "Code"},
	{ID: MsgKindTest, Text: "Test"},
	{ID: MsgKindPlan, Text: "Plan"},
	{ID: MsgKindEvidence, Text: "Evidence"},
	{ID: MsgKindMemory, Text: "Memory"},
	{ID: MsgKindForecast, Text: "Forecast"},
	{ID: MsgKindExecution, Text: "Execution"},
	{ID: MsgKindValidation, Text: "Validation"},

	{ID: MsgMemoryInspectorUnavailable, Text: "Project memory inspector unavailable"},
	{ID: MsgMemoryKindRepositoryFact, Text: "Repository fact"},
	{ID: MsgMemoryKindReviewedCommand, Text: "Reviewed command"},
	{ID: MsgMemoryKindFileToTestMapping, Text: "File-to-test mapping"},
	{ID: MsgMemoryKindRepositoryConvention, Text: "Repository convention"},
	{ID: MsgMemoryKindAcceptedRegressionCase, Text: "Accepted regression case"},
	{ID: MsgMemoryKindExecutionRecipe, Text: "Execution recipe"},
	{ID: MsgMemoryKindExecutableAtomReference, Text: "Executable atom reference"},
	{ID: MsgMemoryKindObservationHypothesis, Text: "Observation or hypothesis"},
	{ID: MsgMemoryMaturityCandidate, Text: "Candidate"},
	{ID: MsgMemoryMaturityValidated, Text: "Validated"},
	{ID: MsgMemoryMaturityPreferredForExperiment, Text: "Preferred for experiment"},
	{ID: MsgMemoryMaturityQuarantined, Text: "Quarantined"},
	{ID: MsgMemoryMaturityInvalidated, Text: "Invalidated"},
	{ID: MsgMemoryMaturityRetired, Text: "Retired"},
	{ID: MsgMemoryFilterKindLabel, Text: "Filter by type"},
	{ID: MsgMemoryFilterMaturityLabel, Text: "Filter by maturity"},
	{ID: MsgMemoryFilterValidityLabel, Text: "Filter by validity"},
	{ID: MsgMemoryFilterLastConfirmationLabel, Text: "Filter by last confirmation"},
	{ID: MsgMemoryFilterClear, Text: "Clear filters"},
	{ID: MsgMemoryFilterValidityAny, Text: "Any validity"},
	{ID: MsgMemoryFilterValidityCurrentlyValid, Text: "Currently valid"},
	{ID: MsgMemoryFilterValidityNotYetValidated, Text: "Not yet validated"},
	{ID: MsgMemoryFilterValidityNoLongerValid, Text: "No longer valid"},
	{ID: MsgMemoryFilterConfirmationAny, Text: "Any time"},
	{ID: MsgMemoryFilterConfirmationWithin7Days, Text: "Confirmed within 7 days"},
	{ID: MsgMemoryFilterConfirmationWithin30Days, Text: "Confirmed within 30 days"},
	{ID: MsgMemoryFilterConfirmationOlderThan30Days, Text: "Confirmed more than 30 days ago"},
	{ID: MsgMemoryFilterConfirmationNeverConfirmed, Text: "Never confirmed"},
	{ID: MsgMemoryListTitleLabel, Text: "Project memory"},
	{ID: MsgMemoryListAriaLabel, Text: "Project memory artifacts"},
	{ID: MsgMemoryListLoading, Text: "Loading memory artifacts"},
	{ID: MsgMemoryListEmptyTitle, Text: "No memory artifacts match"},
	{ID: MsgMemoryListEmptyBody, Text: "Adjust filters or wait for new artifacts to be captured."},
	{ID: MsgMemoryListErrorTitle, Text: "Could not load memory artifacts"},
	{ID: MsgMemoryListErrorBody, Text: "Retry is available."},
	{ID: MsgMemoryListDisconnectedTitle, Text: "Memory list is offline"},
	{ID: MsgMemoryListDisconnectedBody, Text: "Reconnect to see live memory artifacts."},
	{ID: MsgMemoryListRetry, Text: "Retry"},
	{ID: MsgMemoryListRowLastConfirmed, Text: "Last confirmed {state}"},
	{ID: MsgMemoryListRowNeverConfirmed, Text: "Never confirmed"},
	{ID: MsgMemorySelectedNoneTitle, Text: "No artifact selected"},
	{ID: MsgMemorySelectedNoneBody, Text: "Select an artifact from the list to inspect it."},
	{ID: MsgMemorySelectedLoading, Text: "Loading artifact detail"},
	{ID: MsgMemorySelectedErrorTitle, Text: "Could not load artifact detail"},
	{ID: MsgMemorySelectedErrorBody, Text: "Retry is available."},
	{ID: MsgMemorySelectedDeniedTitle, Text: "Access denied"},
	{ID: MsgMemorySelectedDeniedBody, Text: "You are not authorized to inspect this artifact."},
	{ID: MsgMemorySelectedIncompatibleTitle, Text: "Update required"},
	{ID: MsgMemorySelectedIncompatibleBody, Text: "This artifact requires a compatible CodeFlux version."},
	{ID: MsgMemorySelectedDisconnectedTitle, Text: "Detail is offline"},
	{ID: MsgMemorySelectedDisconnectedBody, Text: "Reconnect to load artifact detail."},
	{ID: MsgMemorySelectedRetry, Text: "Retry"},
	{ID: MsgMemoryDetailIdentityTitle, Text: "Identity and maturity"},
	{ID: MsgMemoryDetailIdentityArtifactID, Text: "Stable artifact identity"},
	{ID: MsgMemoryDetailIdentityRevisionID, Text: "Current revision"},
	{ID: MsgMemoryDetailIdentityScope, Text: "Project scope"},
	{ID: MsgMemoryDetailIdentitySupersedes, Text: "Supersedes revision"},
	{ID: MsgMemoryDetailIdentityFromCorrection, Text: "Created from a user correction"},
	{ID: MsgMemoryDetailLineageTitle, Text: "Lineage"},
	{ID: MsgMemoryDetailLineageDerivedFrom, Text: "Derived from (semantic dependency)"},
	{ID: MsgMemoryDetailLineageInfluencedBy, Text: "Influenced by (contextual exposure)"},
	{ID: MsgMemoryDetailLineageOrigin, Text: "Origin artifact"},
	{ID: MsgMemoryDetailLineageRoots, Text: "Lineage roots"},
	{ID: MsgMemoryDetailLineageEmpty, Text: "None recorded"},
	{ID: MsgMemoryDetailLineageUnknownAncestor, Text: "Unknown — ancestor artifact record is unavailable"},
	{ID: MsgMemoryDetailLineageOriginUnknown, Text: "Unknown — {message}"},
	{ID: MsgMemoryDetailLineageOriginIsSelf, Text: "This artifact is its own origin: it has no derived-from ancestor."},
	{ID: MsgMemoryDetailLineageRootsUnknown, Text: "Unknown — {message}"},
	{ID: MsgMemoryDetailLineageRootsIsSelf, Text: "This artifact is itself the only lineage root."},
	{ID: MsgMemoryDetailEpisodesTitle, Text: "Supporting episodes"},
	{ID: MsgMemoryDetailEpisodesEmpty, Text: "No supporting episodes are recorded."},
	{ID: MsgMemoryDetailEpisodeUnknown, Text: "Unknown — {message}"},
	{ID: MsgMemoryDetailBindingsTitle, Text: "Bindings"},
	{ID: MsgMemoryDetailBindingsUnknown, Text: "Unknown — {message}"},
	{ID: MsgMemoryDetailBindingsExactRevision, Text: "Exact revision"},
	{ID: MsgMemoryDetailBindingsValidFrom, Text: "Valid from revision"},
	{ID: MsgMemoryDetailBindingsValidUntil, Text: "Valid until revision"},
	{ID: MsgMemoryDetailBindingsUnmodeled, Text: "Unknown — no revision binding is modeled for this artifact kind."},
	{ID: MsgMemoryDetailApplicabilityTitle, Text: "Applicability predicate"},
	{ID: MsgMemoryDetailApplicabilityUnmodeled, Text: "Unknown — applicability predicate is not modeled for this artifact kind."},
	{ID: MsgMemoryDetailHistoryTitle, Text: "Retrieval and influence history"},
	{ID: MsgMemoryDetailHistoryRetrieved, Text: "Retrieved (not necessarily used)"},
	{ID: MsgMemoryDetailHistoryInfluenced, Text: "Influenced accepted outcome"},
	{ID: MsgMemoryDetailHistoryRejected, Text: "Retrieved and rejected"},
	{ID: MsgMemoryDetailHistoryEmpty, Text: "No retrieval history is recorded."},
	{ID: MsgMemoryCorrectionTitle, Text: "Correct this artifact"},
	{ID: MsgMemoryCorrectionExplain, Text: "A correction always creates a new candidate revision. The current revision is preserved and never mutated."},
	{ID: MsgMemoryCorrectionFieldStatement, Text: "Statement"},
	{ID: MsgMemoryCorrectionFieldCommand, Text: "Command (space-separated arguments, joined by \"; \")"},
	{ID: MsgMemoryCorrectionFieldTestPaths, Text: "Test paths (joined by \"; \")"},
	{ID: MsgMemoryCorrectionFieldExpectedOutcome, Text: "Expected outcome"},
	{ID: MsgMemoryCorrectionFieldPlanSummary, Text: "Plan summary"},
	{ID: MsgMemoryCorrectionFieldCanonicalName, Text: "Canonical name"},
	{ID: MsgMemoryCorrectionFieldCommandLines, Text: "Command (one argument per line)"},
	{ID: MsgMemoryCorrectionFieldTestPathsLines, Text: "Test paths (one per line)"},
	{ID: MsgMemoryCorrectionListFieldExplain, Text: "Enter one value per line. A blank line is ignored; leading and trailing space on each line is trimmed."},
	{ID: MsgMemoryCorrectionSubmit, Text: "Create corrected revision"},
	{ID: MsgMemoryCorrectionInvalid, Text: "This correction is not valid: {message}"},
	{ID: MsgMemoryQuarantineTitle, Text: "Quarantine"},
	{ID: MsgMemoryQuarantineExplain, Text: "Quarantine stops new retrieval immediately and is terminal for this revision. It cannot be un-quarantined or restored with inherited authority."},
	{ID: MsgMemoryQuarantineConfirm, Text: "I understand this action is terminal for this revision"},
	{ID: MsgMemoryQuarantineSubmit, Text: "Quarantine this revision"},
	{ID: MsgMemoryQuarantineUnavailable, Text: "Quarantine is unavailable: {message}"},
	{ID: MsgMemoryInvalidationTitle, Text: "Invalidate"},
	{ID: MsgMemoryInvalidationExplain, Text: "Invalidation records that this revision's claim is no longer trusted."},
	{ID: MsgMemoryInvalidationReasonLabel, Text: "Reason for invalidation"},
	{ID: MsgMemoryInvalidationSubmit, Text: "Invalidate this revision"},
	{ID: MsgMemoryInvalidationUnavailable, Text: "Invalidation is unavailable: {message}"},
	{ID: MsgMemoryDeletionTitle, Text: "Delete"},
	{ID: MsgMemoryDeletionExplain, Text: "Deletion cannot proceed until the affected-descendant preview has loaded."},
	{ID: MsgMemoryDeletionPreviewLoading, Text: "Loading affected-descendant preview"},
	{ID: MsgMemoryDeletionDirectDependents, Text: "Direct semantic dependents"},
	{ID: MsgMemoryDeletionContextuallyInfluenced, Text: "Contextually influenced descendants"},
	{ID: MsgMemoryDeletionNone, Text: "None"},
	{ID: MsgMemoryDeletionAcknowledge, Text: "I acknowledge these dependents will be affected"},
	{ID: MsgMemoryDeletionAcknowledgedList, Text: "Acknowledged dependents"},
	{ID: MsgMemoryDeletionSubmit, Text: "Delete this artifact"},
	{ID: MsgMemoryExportTitle, Text: "Export selected records"},
	{ID: MsgMemoryExportExplain, Text: "Export is secret-free by construction. Redaction happens upstream before a record reaches this list; this view performs no additional scrubbing."},
	{ID: MsgMemoryExportEmpty, Text: "No exportable records are available."},
	{ID: MsgMemoryExportSubmit, Text: "Export selected"},
	{ID: MsgMemoryExportSelect, Text: "Select {label} for export"},
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
