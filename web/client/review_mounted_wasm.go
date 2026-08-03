//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/diffreview"
	"codeflux.dev/codeflux/web/frontend/evidencereport"
	"codeflux.dev/codeflux/web/frontend/pipelineledger"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const mountedReviewTimeout = 5 * time.Second

type reviewNavigation struct {
	OpenPlan       func(uint64, string)
	OpenEvent      func(domain.EventID)
	OpenValidation func(domain.ValidationID)
}

func useMountedReview(
	thread threadrail.Thread,
	mode primitives.Mode,
	requestedPath string,
	navigation reviewNavigation,
) (ui.Node, pipelineledger.SkipSummary) {
	selectedPath := ui.UseState(requestedPath)
	filters := ui.UseState(activeReviewCategoryFilters())
	whitespace := ui.UseState(false)
	activeRow := ui.UseState("")
	status := ui.UseState("")
	dependency := thread.TaskID().String()
	resource := fetch.UseResource(func(parent context.Context) (reviewResource, error) {
		ctx, cancel := context.WithTimeout(parent, mountedReviewTimeout)
		defer cancel()
		return loadReviewResource(ctx, openBrowserReviewResourceClient, thread.TaskID())
	}, dependency)
	state := resource.Get()
	// The pipeline ledger is fetched unconditionally, alongside the review
	// resource above, so this hook's call order never depends on the review
	// resource's own state -- GoWebComponents hooks are positional, and an
	// early return before this line on some renders and after it on others
	// would desync them. Its own LedgerCard already carries loading, empty,
	// and error presentation, so it is safe to show beside review content in
	// every state review itself can be in (PIPE-006a).
	//
	// skipSummary is returned to the caller too (PIPE-044a), rather than
	// discarded here and fetched a second time from a second call site: two
	// independent fetch.UseResource instances asking the same coordinator the
	// same question on every render raced under load in practice -- one
	// would occasionally never resolve within a generous bound, and because
	// this hook's only dependency (the task ID) does not change once a task
	// is selected, a fetch stuck that way stays stuck for the rest of the
	// browser session with no visible retry. Returning the one instance this
	// hook already had is the fix, not a longer timeout on a second one.
	ledgerNode, skipSummary := useMountedPipelineLedger(thread, mode, 0)

	var body ui.Node
	switch {
	case thread.TaskID().IsZero():
		body = primitives.InlineAlert(primitives.InlineAlertProps{Title: "Review unavailable", Message: "Select an authoritative task to load its review.", Tone: design.StatusNeutral, Mode: mode})
	case state.Loading:
		body = primitives.InlineAlert(primitives.InlineAlertProps{Title: "Loading task review", Message: "Loading sealed evidence and the bounded diff.", Tone: design.StatusNeutral, Mode: mode})
	case state.Error != nil || !state.Ready:
		message := "The sealed evidence report could not be loaded."
		if state.Error != nil {
			message = state.Error.Error()
		}
		body = primitives.InlineAlert(primitives.InlineAlertProps{Title: "Review unavailable", Message: message, Tone: design.StatusFailure, Mode: mode})
	default:
		value := state.Value
		children := []ui.Node{evidencereport.ReportCard(evidencereport.Props{Report: value.Report, Mode: mode})}
		if value.Diff == nil {
			message := "The live diff no longer matches this immutable evidence report. Refresh task evidence before making a decision."
			if value.DiffMatchesReport {
				message = "No bounded textual diff is available for this report."
			}
			children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{Title: "Diff unavailable", Message: message, Tone: design.StatusWarning, Mode: mode}))
		} else {
			selection := selectedPath.Get()
			if _, ok := diffreview.FindChangedFile(value.Diff.Files, selection); !ok && len(value.Diff.Files) > 0 {
				selection = value.Diff.Files[0].Path.String()
			}
			props := diffreview.Props{
				Mode: mode, DiffIdentity: value.Diff.Identity, BaseRevision: value.Diff.BaseRevision,
				HeadRevision: value.Diff.HeadRevision, Files: value.Diff.Files,
				CategoryFilters: filters.Get(), SelectedPath: selection,
				WhitespaceVisible: whitespace.Get(), ActiveHunkRowKey: activeRow.Get(),
				OnSelectFile: selectedPath.Set,
				OnToggleCategory: func(category diffreview.FileCategory, active bool) {
					next := append([]diffreview.CategoryFilter(nil), filters.Get()...)
					for index := range next {
						if next[index].Category == category {
							next[index].Active = active
						}
					}
					filters.Set(next)
				},
				OnToggleWhitespace: whitespace.Set, OnActiveHunkRowChange: activeRow.Set,
			}
			props.OnOpenPlanStep = func(link taskgraph.PlanStepLink) {
				if navigation.OpenPlan != nil {
					navigation.OpenPlan(link.PlanRevision, link.StepID)
				}
			}
			props.OnOpenToolEvent = navigation.OpenEvent
			props.OnOpenValidation = navigation.OpenValidation
			props.OnOpenInEditor = func(path string, line uint32) {
				status.Set("Opening " + path + " in the configured editor…")
				ui.SafeGo("open reviewed file in editor", func() {
					ctx, cancel := context.WithTimeout(context.Background(), mountedReviewTimeout)
					defer cancel()
					err := openReviewedFile(ctx, thread.WorkspaceID(), path, line)
					ui.PostAsync(func() {
						if err != nil {
							status.Set("Editor open failed: " + err.Error())
							return
						}
						status.Set("Opened " + path + " in the configured editor.")
					})
				})
			}
			children = append(children, diffreview.DiffReview(props))
		}
		if status.Get() != "" {
			children = append(children, html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Text: status.Get()}))
		}
		body = html.Div(html.Props{Data: map[string]string{"component": "mounted-task-review"}, Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(mode.Tokens().Spacing.LG))).String()}, children...)
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "mounted-task-review-and-ledger"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(mode.Tokens().Spacing.LG))).String(),
	}, body, pipelineLedgerSection(mode, ledgerNode)), skipSummary
}

// pipelineLedgerSection wraps the mounted pipeline ledger card with its own
// labelled heading so it reads as a distinct section of the task review
// rather than an unexplained extra block (PIPE-006a).
func pipelineLedgerSection(mode primitives.Mode, ledger ui.Node) ui.Node {
	tokens := mode.Tokens()
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Pipeline stage ledger"},
		Data:  map[string]string{"component": "review-pipeline-ledger-section"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String(),
	}, ledger)
}

func openBrowserReviewResourceClient(ctx context.Context) (reviewResourceLease, error) {
	connection, err := grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return reviewResourceLease{}, err
	}
	return reviewResourceLease{client: codefluxv1.NewReviewServiceClient(connection), close: connection.Close}, nil
}

func openReviewedFile(ctx context.Context, workspaceID domain.WorkspaceID, path string, line uint32) error {
	lease, err := openBrowserReviewResourceClient(ctx)
	if err != nil {
		return err
	}
	defer lease.close()
	_, err = lease.client.OpenInEditor(ctx, &codefluxv1.OpenInEditorRequest{
		Control:               &codefluxv1.MutationControl{IdempotencyKey: fmt.Sprintf("review-editor-%d", time.Now().UnixNano())},
		WorkspaceId:           &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, Value: workspaceID.String()},
		WorkspaceRelativePath: path, Line: line, Column: 1,
	})
	return err
}
