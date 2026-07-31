package threadrail

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedThreadRowHeight = 72

// ThreadRailProps binds immutable rail state to user intent callbacks. Rename
// and archive callbacks express intent only; authoritative reducer responses
// remain responsible for changing rows.
type ThreadRailProps struct {
	State               State
	Mode                primitives.Mode
	Height              float64
	Embedded            bool
	NewThreadBusy       bool
	RenameBusy          bool
	ArchiveBusy         bool
	OnNewThread         func()
	OnFilterChange      func(Filter)
	OnActiveChange      func(RowKey)
	OnSelect            func(RowKey)
	OnLoadNext          func()
	OnRetry             func()
	OnRename            func(domain.ThreadID)
	OnArchiveRequest    func(domain.ThreadID)
	OnArchiveCancel     func()
	ArchiveConfirmation domain.ThreadID
	OnArchive           func(domain.ThreadID, bool)
}

// ThreadRail renders the repository-scoped navigation, filters, virtualized
// rows, explicit data states, and selected-thread mutation affordances.
func ThreadRail(props ThreadRailProps) ui.Node {
	if props.State.RepositoryID().IsZero() || props.State.WorkspaceID().IsZero() {
		return threadRailContractError("repository and workspace state are required")
	}
	height := props.Height
	if height == 0 {
		height = 560
	}
	contract, err := NewVirtualListContract(props.State, height)
	if err != nil {
		return threadRailContractError(err.Error())
	}
	tokens := props.Mode.Tokens()
	children := []ui.Node{
		html.Header(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			html.H2(html.Props{Text: map[bool]string{true: "Recent tasks", false: "Threads"}[props.Embedded]}),
			ui.CreateElement(threadRailButton, primitives.ButtonProps{
				ID: "thread-rail-new", Label: "New thread", AccessibleLabel: "Create new thread",
				Primary: true, Busy: props.NewThreadBusy, Disabled: props.OnNewThread == nil,
				Mode: props.Mode, OnClick: props.OnNewThread,
			}),
		),
		threadFilterControls(props),
	}
	listProps := contract.Props(
		props.Mode,
		func(item primitives.VirtualListItemProps[Row]) ui.Node {
			return ui.CreateElement(threadRailRow, threadRailRowProps{
				Row: item.Item, Selected: !item.Item.Pending() && item.Item.ThreadID() == props.State.SelectedThreadID(),
				Active: item.Active, Mode: props.Mode,
			})
		},
		props.OnActiveChange, props.OnSelect, props.OnRetry,
	)
	listProps.RowHeight = mountedThreadRowHeight
	listProps.ItemLabel = mountedThreadRowLabel
	listProps.OnVisibleRangeChange = func(visibleEnd int) {
		requestMountedNextPage(contract, visibleEnd, props.OnLoadNext)
	}
	children = append(children, ui.CreateElement(threadRailVirtualList, listProps))
	if contract.ShouldRequestNextPage(-1) && props.OnLoadNext != nil {
		children = append(children, ui.CreateElement(threadRailEmptyPageAdvance, threadRailEmptyPageAdvanceProps{
			Cursor: props.State.NextCursor(), Filter: props.State.Filter(), OnLoadNext: props.OnLoadNext,
		}))
	}
	if contract.LoadingNext {
		children = append(children, html.Div(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data: map[string]string{"component": "rail-pagination", "state": "loading"},
			Text: "Loading more threadsâ€¦",
		}))
	}
	if contract.HasPageError {
		children = append(children,
			ui.CreateElement(threadRailAlert, primitives.InlineAlertProps{
				Title: "More threads unavailable", Message: contract.PaginationError.Message,
				Tone: design.StatusWarning, Mode: props.Mode,
			}),
			ui.CreateElement(threadRailButton, primitives.ButtonProps{
				ID: "thread-rail-page-retry", Label: "Retry", AccessibleLabel: "Retry loading more threads",
				Disabled: props.OnRetry == nil || !contract.PaginationError.Retryable,
				Mode:     props.Mode, OnClick: props.OnRetry,
			}),
		)
	}
	if selected, ok := selectedVisibleRow(props.State); ok {
		children = append(children, selectedThreadActions(props, selected))
	}
	containerRules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
		css.MinWidth(css.Zero), css.W(css.Full), css.MaxWidth(css.Full), css.Overflow.Hidden,
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	}
	if !props.Embedded {
		containerRules = append(containerRules,
			css.Padding(css.Px(tokens.Spacing.MD)), css.MinWidth(css.Px(224)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		)
	}
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Thread navigation"},
		Data: map[string]string{
			"component": "thread-rail", "filter": string(props.State.Filter()),
			"presentation": string(props.State.Presentation()),
		},
		Class: css.New(containerRules...).String(),
	}, children...)
}

type threadRailEmptyPageAdvanceProps struct {
	Cursor     Cursor
	Filter     Filter
	OnLoadNext func()
}

func threadRailEmptyPageAdvance(props threadRailEmptyPageAdvanceProps) ui.Node {
	ui.UseEffect(func() func() {
		props.OnLoadNext()
		return nil
	}, props.Cursor, props.Filter)
	return html.Span(html.Props{
		Hidden: true,
		Data:   map[string]string{"component": "thread-rail-page-advance"},
	})
}

type threadRailRowProps struct {
	Row      Row
	Selected bool
	Active   bool
	Mode     primitives.Mode
}

func threadRailRow(props threadRailRowProps) ui.Node {
	view := props.Row.View()
	tokens := props.Mode.Tokens()
	metadata := []ui.Node{html.Span(html.Props{
		Data: map[string]string{"field": "task-state"}, Text: compactTaskState(view.TaskState),
	})}
	if view.Attention != AttentionNone {
		metadata = append(metadata, html.Span(html.Props{
			Role: "status", Data: map[string]string{"field": "attention", "kind": string(view.Attention)},
			Text: compactAttention(view.Attention),
		}))
	}
	metadata = append(metadata, html.Span(html.Props{
		Data: map[string]string{"field": "last-activity"}, Text: compactRailActivity(props.Row.UpdatedAt()),
	}))
	indicators := make([]ui.Node, 0, 2)
	if view.Unread > 0 {
		indicators = append(indicators, html.Span(html.Props{
			Aria: map[string]string{"label": fmt.Sprintf("%d unread", view.Unread)},
			Data: map[string]string{"field": "unread"},
			Class: css.New(
				css.MinWidth(css.Px(22)), css.PaddingX(css.Px(tokens.Spacing.XS)),
				css.Rounded(css.Px(tokens.Geometry.PillRadius)),
				css.Bg(css.Hex(string(tokens.Colors.Selection))),
				css.TextColor(css.Hex(string(tokens.Colors.OnSelection))),
				css.TextAlign.Center, css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
			Text: strconv.FormatUint(uint64(view.Unread), 10),
		}))
	}
	if view.Archived {
		indicators = append(indicators, html.Span(html.Props{Data: map[string]string{"field": "archived"}, Text: "Archived"}))
	}
	rowRules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
		css.Padding(css.Px(tokens.Spacing.SM)), css.MinHeight(css.Px(mountedThreadRowHeight)),
		css.MinWidth(css.Zero), css.W(css.Full), css.Overflow.Hidden,
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
	}
	if props.Selected {
		rowRules = append(rowRules,
			css.Bg(css.Hex(string(tokens.Colors.Selection))),
			css.TextColor(css.Hex(string(tokens.Colors.OnSelection))),
			css.Border(css.Px(tokens.Geometry.BorderStrongWidth), css.Hex(string(tokens.Colors.OnSelection))),
		)
	} else if props.Active {
		rowRules = append(rowRules, css.Border(
			css.Px(tokens.Geometry.BorderStrongWidth), css.Hex(string(tokens.Colors.BorderStrong)),
		))
	}
	titleColor := tokens.Colors.TextPrimary
	metadataColor := tokens.Colors.TextMuted
	if props.Selected {
		titleColor = tokens.Colors.OnSelection
		metadataColor = tokens.Colors.OnSelection
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "thread-row", "pending": strconv.FormatBool(view.Pending),
			"selected": strconv.FormatBool(props.Selected), "active": strconv.FormatBool(props.Active),
			"archived":  strconv.FormatBool(view.Archived),
			"attention": string(view.Attention), "unread": strconv.FormatUint(uint64(view.Unread), 10),
		},
		Class: css.New(rowRules...).String(),
	},
		html.Div(html.Props{Class: css.New(
			u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero),
		).String()},
			html.Strong(html.Props{
				Title: view.Title,
				Class: css.New(
					css.MinWidth(css.Zero), css.Overflow.Hidden, css.WhiteSpace.NoWrap,
					css.TextOverflowEllipsis(), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
					css.TextColor(css.Hex(string(titleColor))),
				).String(),
				Text: view.Title,
			}),
			html.Div(html.Props{Class: css.New(u.Flex, css.Gap(css.Px(tokens.Spacing.XS))).String()}, indicators...),
		),
		html.Div(html.Props{
			Aria: map[string]string{"label": "Thread status"},
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinWidth(css.Zero), css.Overflow.Hidden, css.WhiteSpace.NoWrap,
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(metadataColor))),
			).String(),
		}, metadata...),
	)
}

func selectedThreadActions(props ThreadRailProps, row Row) ui.Node {
	title := row.Title()
	archived := row.Archived()
	confirmationOpen := !archived && props.ArchiveConfirmation == row.ThreadID()
	archiveLabel := "Archive"
	archiveName := "Archive thread " + title
	if archived {
		archiveLabel = "Restore"
		archiveName = "Restore archived thread " + title
	}
	children := []ui.Node{
		ui.CreateElement(threadRailButton, primitives.ButtonProps{
			ID: "thread-rail-rename", Label: "Rename", AccessibleLabel: "Rename thread " + title,
			Busy: props.RenameBusy, Disabled: props.OnRename == nil, Mode: props.Mode,
			OnClick: func() {
				if props.OnRename != nil {
					props.OnRename(row.ThreadID())
				}
			},
		}),
	}
	disabled := props.OnArchive == nil
	onClick := func() {
		if archived {
			if props.OnArchive != nil {
				props.OnArchive(row.ThreadID(), false)
			}
			return
		}
		if props.OnArchiveRequest != nil {
			props.OnArchiveRequest(row.ThreadID())
		}
	}
	if !archived {
		disabled = props.OnArchiveRequest == nil && !confirmationOpen
	}
	children = append(children, ui.CreateElement(threadRailButton, primitives.ButtonProps{
		ID: "thread-rail-archive", Label: archiveLabel, AccessibleLabel: archiveName,
		Busy: props.ArchiveBusy, Disabled: disabled, Mode: props.Mode, OnClick: onClick,
		Expanded: &confirmationOpen, Controls: "thread-rail-archive-dialog",
	}))
	if confirmationOpen {
		children = append(children, archiveConfirmationActions(props, row))
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Selected thread actions"},
		Data: map[string]string{"component": "thread-actions", "thread-id": row.ThreadID().String()},
		Class: css.New(
			u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(props.Mode.Tokens().Spacing.XS)),
		).String(),
	}, children...)
}

func archiveConfirmationActions(props ThreadRailProps, row Row) ui.Node {
	return primitives.Dialog(primitives.OverlayProps{
		ID: "thread-rail-archive-dialog", Open: true,
		LabelledBy:           "thread-rail-archive-title",
		DescribedBy:          "thread-rail-archive-description",
		InitialFocusSelector: "#thread-rail-archive-confirm",
		AppRootSelector:      `[data-component="app-shell"]`,
		Mode:                 props.Mode,
		OnDismiss:            props.OnArchiveCancel,
		Content: html.Section(html.Props{
			Data:  map[string]string{"component": "archive-confirmation", "thread-id": row.ThreadID().String()},
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(props.Mode.Tokens().Spacing.SM))).String(),
		},
			html.H2(html.Props{ID: "thread-rail-archive-title", Text: "Archive this thread?"}),
			html.P(html.Props{
				ID:   "thread-rail-archive-description",
				Text: "Archive " + row.Title() + "? You can restore it from Past threads.",
			}),
			html.Div(html.Props{
				Role: "group", Aria: map[string]string{"label": "Archive confirmation actions"},
				Class: css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(props.Mode.Tokens().Spacing.XS))).String(),
			},
				ui.CreateElement(threadRailButton, primitives.ButtonProps{
					ID: "thread-rail-archive-confirm", Label: "Confirm", AccessibleLabel: "Confirm archive thread " + row.Title(),
					Primary: true, Busy: props.ArchiveBusy, Disabled: props.OnArchive == nil,
					Mode: props.Mode,
					OnClick: func() {
						if props.OnArchive != nil {
							props.OnArchive(row.ThreadID(), true)
						}
					},
				}),
				ui.CreateElement(threadRailButton, primitives.ButtonProps{
					ID: "thread-rail-archive-cancel", Label: "Cancel", AccessibleLabel: "Cancel archiving thread " + row.Title(),
					Disabled: props.ArchiveBusy || props.OnArchiveCancel == nil, Mode: props.Mode,
					OnClick: props.OnArchiveCancel,
				}),
			),
		),
	})
}

func requestMountedNextPage(contract VirtualListContract, visibleEnd int, onLoadNext func()) bool {
	if onLoadNext == nil || !contract.ShouldRequestNextPage(visibleEnd) {
		return false
	}
	onLoadNext()
	return true
}

func threadFilterControls(props ThreadRailProps) ui.Node {
	children := make([]ui.Node, 0, 3)
	for _, item := range []struct {
		filter     Filter
		label      string
		accessible string
	}{
		{FilterActive, "Open", "Show current threads"},
		{FilterArchived, "Past", "Show archived threads"},
		{FilterAll, "All", "Show all threads"},
	} {
		item := item
		children = append(children, ui.CreateElement(threadRailToggle, primitives.ToggleButtonProps{
			ID: "thread-filter-" + string(item.filter), Label: item.label,
			AccessibleLabel: item.accessible,
			Pressed:         props.State.Filter() == item.filter, Disabled: props.OnFilterChange == nil,
			Mode: props.Mode,
			OnToggle: func(bool) {
				if props.OnFilterChange != nil {
					props.OnFilterChange(item.filter)
				}
			},
		}))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Thread filters"},
		Data: map[string]string{"component": "thread-filters"},
		Class: css.New(
			u.Grid,
			css.GridCols(css.Repeat(3, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
			css.Gap(css.Px(props.Mode.Tokens().Spacing.XS)),
		).String(),
	}, children...)
}

func mountedThreadRowLabel(row Row) string {
	view := row.View()
	parts := []string{
		view.Title,
		"state " + humanizeRailValue(string(view.TaskState)),
		"last active " + row.UpdatedAt().UTC().Format("January 2, 2006 at 15:04 UTC"),
	}
	if view.Attention != AttentionNone {
		parts = append(parts, compactAttention(view.Attention))
	}
	if view.Unread > 0 {
		parts = append(parts, fmt.Sprintf("%d unread", view.Unread))
	}
	if view.Archived {
		parts = append(parts, "archived")
	}
	return strings.Join(parts, ", ")
}

func compactRailActivity(value time.Time) string {
	return value.UTC().Format("Jan 2")
}

func compactTaskState(value TaskState) string {
	switch value {
	case TaskStateNone:
		return "No task"
	case TaskStateComplete:
		return "Done"
	case TaskStateStopped:
		return "Stopped"
	default:
		return humanizeRailValue(string(value))
	}
}

func compactAttention(value Attention) string {
	switch value {
	case AttentionPendingApproval:
		return "Needs approval"
	case AttentionRecovery:
		return "Needs recovery"
	case AttentionValidationFailure:
		return "Check failed"
	case AttentionUserInput:
		return "Input needed"
	default:
		return ""
	}
}

func selectedVisibleRow(state State) (Row, bool) {
	if state.SelectedThreadID().IsZero() {
		return Row{}, false
	}
	for _, row := range state.Rows() {
		if !row.Pending() && row.ThreadID() == state.SelectedThreadID() {
			return row, true
		}
	}
	return Row{}, false
}

func humanizeRailValue(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func threadRailContractError(message string) ui.Node {
	return html.Section(html.Props{
		Role: "alert", Data: map[string]string{"component": "thread-rail", "state": "invalid-contract"},
		Text: "Thread rail unavailable: " + message,
	})
}

func threadRailButton(props primitives.ButtonProps) ui.Node { return primitives.Button(props) }
func threadRailToggle(props primitives.ToggleButtonProps) ui.Node {
	return primitives.ToggleButton(props)
}
func threadRailAlert(props primitives.InlineAlertProps) ui.Node { return primitives.InlineAlert(props) }
func threadRailVirtualList(props primitives.VirtualListProps[Row]) ui.Node {
	return primitives.VirtualList(props)
}
