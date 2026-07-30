//go:build js && wasm

// Command client is the GoWebComponents v5 Milestone 06 browser spike.
package main

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/interop"
	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
	"github.com/monstercameron/GoWebComponents/v5/virtualization"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	syntheticThreadCount = 10_000
	syntheticGraphNodes  = 300
	syntheticEventCount  = 10_000
	maxClientMessageSize = (4 << 20) + 1024
)

type threadItem struct {
	ID    string
	Title string
}

type transportControl struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
}

var (
	syntheticThreads = buildSyntheticThreads()
	streamControl    transportControl
)

func main() {
	routes := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: "/"})
	routes.Register("/", spikePage, router.Options{Title: "Codeflux transport spike"})
	routes.Register("/details", detailsPage, router.Options{Title: "Codeflux spike details"})
	routes.Register("*", spikePage)
	routes.Mount("#app")
	utils.WaitForever()
}

func spikePage(router.Attrs) ui.Node {
	return ui.CreateElement(spikeApplication)
}

func detailsPage(router.Attrs) ui.Node {
	navigate := router.UseNavigate()
	back := ui.UseEvent(func() { navigate.Navigate("/") })
	return html.Main(
		html.Props{
			ID: "details-route",
			Class: className(
				u.Flex,
				u.FlexCol,
				u.GapV(css.Px(20)),
				css.MinHeight(css.Vh(100)),
				css.Padding(css.Px(32)),
				css.Bg(css.Hex("#071019")),
				css.TextColor(css.Hex("#d8e7ef")),
				css.Font(css.SansStack),
			),
		},
		html.P(html.Props{Class: eyebrowClass()}, html.Text("HISTORY ROUTER / DETAILS")),
		html.H1(html.Props{Class: headingClass()}, html.Text("Browser history is live.")),
		html.P(
			html.Props{Class: bodyCopyClass()},
			html.Text("Use the browser Back and Forward buttons after returning to prove route state follows the History API."),
		),
		html.Button(
			html.Props{
				ID:      "return-to-spike",
				Type:    "button",
				Class:   buttonClass(),
				OnClick: back,
			},
			html.Text("Return to transport spike"),
		),
	)
}

func spikeApplication() ui.Node {
	navigate := router.UseNavigate()
	transportStatus := state.UseAtom("transport-spike-status", "connecting")
	shellRenders := ui.UseRef(0)
	shellRenders.Set(shellRenders.Get() + 1)
	openDetails := ui.UseEvent(func() { navigate.Navigate("/details") })

	return html.Main(
		html.Props{
			ID: "spike-root",
			Data: map[string]string{
				"framework":     "GoWebComponents-v5.0.1",
				"transport":     "websocket-grpc",
				"shell-renders": strconv.Itoa(shellRenders.Get()),
			},
			Class: className(
				u.Flex,
				u.FlexCol,
				u.GapV(css.Px(20)),
				css.MinHeight(css.Vh(100)),
				css.Padding(css.Px(20)),
				css.Bg(css.Hex("#071019")),
				css.TextColor(css.Hex("#d8e7ef")),
				css.Font(css.SansStack),
			),
		},
		html.Header(
			html.Props{Class: className(u.Flex, u.ItemsCenter, u.JustifyBetween, u.GapV(css.Px(16)))},
			html.Div(
				html.Props{},
				html.P(html.Props{Class: eyebrowClass()}, html.Text("CODEFLUX / M06 CONFORMANCE")),
				html.H1(html.Props{Class: headingClass()}, html.Text("Typed Go, live evidence")),
			),
			html.Div(
				html.Props{Class: className(u.Flex, u.ItemsCenter, u.GapV(css.Px(12)))},
				html.Span(
					html.Props{
						ID:    "transport-status",
						Role:  "status",
						Aria:  map[string]string{"live": "polite"},
						Class: statusClass(),
					},
					html.Text(transportStatus.Get()),
				),
				html.Button(
					html.Props{
						ID:      "open-details",
						Type:    "button",
						Class:   buttonClass(),
						OnClick: openDetails,
					},
					html.Text("Details"),
				),
			),
		),
		html.Div(
			html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(20)), u.ItemsStart)},
			ui.CreateElement(threadRail),
			html.Div(
				html.Props{Class: className(u.Flex, u.FlexCol, u.GapV(css.Px(20)), css.Flex(css.Num(1), css.Num(1), css.Px(480)), css.MinWidth(css.Px(320)))},
				ui.CreateElement(streamPanel),
				ui.CreateElement(graphPanel),
			),
		),
	)
}

func threadRail() ui.Node {
	selected := ui.UseState("thread-00001")
	diagnostics := ui.UseState(virtualization.ViewportDiagnostics{})
	return html.Aside(
		html.Props{
			ID:    "thread-rail",
			Aria:  map[string]string{"label": "Synthetic thread list"},
			Class: className(panelRules()...),
		},
		html.H2(html.Props{Class: sectionHeadingClass()}, html.Text("10,000 threads")),
		html.P(
			html.Props{ID: "virtualization-diagnostics", Class: captionClass()},
			html.Text(fmt.Sprintf(
				"rendered %d, visible %d-%d",
				diagnostics.Get().RenderedCount,
				diagnostics.Get().VisibleStart,
				diagnostics.Get().VisibleEnd,
			)),
		),
		virtualization.List(virtualization.ListProps[threadItem]{
			ID:        "synthetic-thread-list",
			Items:     syntheticThreads,
			Height:    420,
			RowHeight: 42,
			Overscan:  4,
			OuterProps: html.Props{
				TabIndex: html.TabIndexZero,
				Role:     "listbox",
				Aria:     map[string]string{"label": "10,000 synthetic threads"},
			},
			Class: className(
				css.MarginY(css.Px(12)),
				css.Border(css.Px(1), css.Hex("#17394a")),
				css.Rounded(css.Px(12)),
			),
			ItemKey:          func(item threadItem) string { return item.ID },
			OnViewportChange: diagnostics.Set,
			RenderRow: func(row virtualization.RowRenderProps[threadItem]) ui.Node {
				active := row.Item.ID == selected.Get()
				rowClass := threadRowClass(false)
				if active {
					rowClass = threadRowClass(true)
				}
				selectRow := ui.UseEvent(func() { selected.Set(row.Item.ID) })
				return html.Button(
					html.Props{
						Type:    "button",
						Role:    "option",
						Aria:    map[string]string{"selected": strconv.FormatBool(active)},
						Class:   rowClass,
						OnClick: selectRow,
					},
					html.Text(row.Item.Title),
				)
			},
		}),
		html.P(
			html.Props{ID: "selected-thread", Class: captionClass()},
			html.Text("Selected: "+selected.Get()),
		),
	)
}

func streamPanel() ui.Node {
	status := state.UseAtom("transport-spike-status", "connecting")
	lastSequence := ui.UseState(uint64(0))
	tokenBatchCount := ui.UseState(0)
	retryRevision := ui.UseState(0)
	clipboardStatus := ui.UseState("clipboard idle")
	editorStatus := ui.UseState("editor capability idle")
	renderCount := ui.UseRef(0)
	renderCount.Set(renderCount.Get() + 1)

	ui.UseEffect(func() func() {
		ctx, cancel := context.WithCancel(context.Background())
		generation := streamControl.set(cancel)
		go runTransportStream(ctx, status, lastSequence, tokenBatchCount)
		return func() {
			cancel()
			streamControl.clear(generation)
		}
	}, retryRevision.Get())

	cancelStream := ui.UseEvent(func() {
		streamControl.cancelActive()
	})
	restartStream := ui.UseEvent(func() {
		streamControl.cancelActive()
		lastSequence.Set(0)
		tokenBatchCount.Set(0)
		retryRevision.Update(func(previous int) int { return previous + 1 })
	})
	copyEvidence := ui.UseEvent(func() {
		clipboard, err := interop.GetClipboard()
		if err != nil {
			clipboardStatus.Set("clipboard unavailable")
			return
		}
		go func() {
			if err := clipboard.WriteText(context.Background(), "Codeflux M06 transport evidence"); err != nil {
				clipboardStatus.Set("clipboard write denied")
				return
			}
			clipboardStatus.Set("evidence copied")
		}()
	})
	checkEditor := ui.UseEvent(func() {
		editorStatus.Set("checking capability")
		go checkEditorCapability(editorStatus)
	})

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	costMicrousd := lastSequence.Get() * 3
	taskState := status.Get()
	if taskState == "complete" {
		taskState = "verified"
	}
	return html.Section(
		html.Props{
			ID: "stream-panel",
			Data: map[string]string{
				"chat-renders":  strconv.Itoa(renderCount.Get()),
				"last-sequence": strconv.FormatUint(lastSequence.Get(), 10),
				"token-batches": strconv.Itoa(tokenBatchCount.Get()),
			},
			Class: className(panelRules()...),
		},
		html.Div(
			html.Props{Class: className(u.Flex, u.JustifyBetween, u.ItemsCenter, u.GapV(css.Px(12)))},
			html.Div(
				html.Props{},
				html.P(html.Props{Class: eyebrowClass()}, html.Text("ORDERED SESSION STREAM")),
				html.H2(html.Props{Class: sectionHeadingClass()}, html.Text("Token, cost, and task state")),
			),
			html.Span(
				html.Props{ID: "task-state", Role: "status", Class: statusClass()},
				html.Text(taskState),
			),
		),
		html.Div(
			html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(12)), css.MarginY(css.Px(16)))},
			metric("Last sequence", strconv.FormatUint(lastSequence.Get(), 10), "last-sequence"),
			metric("Token batches", strconv.Itoa(tokenBatchCount.Get()), "token-batches"),
			metric("Cost", fmt.Sprintf("%d μUSD", costMicrousd), "synthetic-cost"),
			metric("Go heap", fmt.Sprintf("%.1f MiB", float64(memory.Alloc)/(1024*1024)), "go-heap"),
			metric("Chat renders", strconv.Itoa(renderCount.Get()), "chat-renders"),
		),
		html.Div(
			html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(10)))},
			actionButton("cancel-stream", "Cancel stream", cancelStream),
			actionButton("restart-stream", "Restart stream", restartStream),
			actionButton("copy-evidence", "Copy evidence", copyEvidence),
			actionButton("check-editor", "Check editor handoff", checkEditor),
		),
		html.Div(
			html.Props{
				Role:  "status",
				Aria:  map[string]string{"live": "polite"},
				Class: className(u.Flex, u.FlexCol, u.GapV(css.Px(6)), css.MarginY(css.Px(12))),
			},
			html.P(html.Props{ID: "clipboard-status", Class: captionClass()}, html.Text(clipboardStatus.Get())),
			html.P(html.Props{ID: "editor-status", Class: captionClass()}, html.Text(editorStatus.Get())),
		),
	)
}

func graphPanel() ui.Node {
	activeNode := ui.UseState(0)
	patchCount := ui.UseState(0)
	nodeCount := ui.UseState(syntheticGraphNodes)
	updatesRunning := ui.UseState(true)
	frameEvidence := ui.UseState("measuring 60 frames")
	renderCount := ui.UseRef(0)
	renderCount.Set(renderCount.Get() + 1)

	ui.UseEffect(func() func() {
		if !updatesRunning.Get() {
			return nil
		}
		timer, err := interop.ScheduleInterval(75*time.Millisecond, func() {
			patchCount.Update(func(previous int) int { return previous + 1 })
			activeNode.Update(func(previous int) int { return (previous + 1) % nodeCount.Get() })
		})
		if err != nil {
			return nil
		}
		return func() { _ = timer.Cancel() }
	}, updatesRunning.Get(), nodeCount.Get())

	ui.UseEffect(func() func() {
		active := true
		var cancelFrame func()
		var previousTimestamp float64
		frameDurations := make([]float64, 0, 60)
		var scheduleFrame func()
		scheduleFrame = func() {
			cancelFrame = interop.RequestAnimationFrame(func(timestamp float64) {
				if !active {
					return
				}
				if previousTimestamp > 0 {
					frameDurations = append(frameDurations, timestamp-previousTimestamp)
				}
				previousTimestamp = timestamp
				if len(frameDurations) < 60 {
					scheduleFrame()
					return
				}
				sort.Float64s(frameDurations)
				frameEvidence.Set(fmt.Sprintf(
					"60 frames · p50 %.1f ms · p95 %.1f ms · max %.1f ms",
					frameDurations[30],
					frameDurations[57],
					frameDurations[59],
				))
			})
		}
		scheduleFrame()
		return func() {
			active = false
			if cancelFrame != nil {
				cancelFrame()
			}
		}
	}, nodeCount.Get())

	patchGraph := ui.UseEvent(func() {
		patchCount.Update(func(previous int) int { return previous + 1 })
		activeNode.Update(func(previous int) int { return (previous + 17) % nodeCount.Get() })
	})
	toggleUpdates := ui.UseEvent(func() {
		updatesRunning.Update(func(previous bool) bool { return !previous })
	})
	use300Nodes := ui.UseEvent(func() {
		nodeCount.Set(300)
		activeNode.Set(0)
	})
	use600Nodes := ui.UseEvent(func() {
		nodeCount.Set(600)
		activeNode.Set(0)
	})
	use900Nodes := ui.UseEvent(func() {
		nodeCount.Set(900)
		activeNode.Set(0)
	})
	use1200Nodes := ui.UseEvent(func() {
		nodeCount.Set(1200)
		activeNode.Set(0)
	})
	keyGraph := ui.UseEvent(func(event ui.KeyboardEvent) {
		switch event.GetKey() {
		case "ArrowRight", "ArrowDown":
			event.PreventDefault()
			activeNode.Update(func(previous int) int { return (previous + 1) % nodeCount.Get() })
		case "ArrowLeft", "ArrowUp":
			event.PreventDefault()
			activeNode.Update(func(previous int) int {
				if previous == 0 {
					return nodeCount.Get() - 1
				}
				return previous - 1
			})
		}
	})

	graphChildren := make([]ui.Node, 0, nodeCount.Get()*2)
	for index := 0; index < nodeCount.Get(); index++ {
		x := 18 + (index%30)*24
		y := 18 + (index/30)*24
		if index > 0 {
			previousX := 18 + ((index-1)%30)*24
			previousY := 18 + ((index-1)/30)*24
			graphChildren = append(graphChildren, html.Line(html.Props{
				Key: "edge-" + strconv.Itoa(index),
				Raw: map[string]any{
					"x1":           previousX,
					"y1":           previousY,
					"x2":           x,
					"y2":           y,
					"stroke":       "#1f5669",
					"stroke-width": 1,
				},
			}))
		}
		fill := "#2a7187"
		if index == activeNode.Get() {
			fill = "#f5b94c"
		}
		graphChildren = append(graphChildren, html.Circle(html.Props{
			Key: "node-" + strconv.Itoa(index),
			Raw: map[string]any{
				"cx":   x,
				"cy":   y,
				"r":    5,
				"fill": fill,
			},
		}))
	}
	graphHeight := ((nodeCount.Get()+29)/30)*24 + 20
	updateLabel := "Pause graph updates"
	if !updatesRunning.Get() {
		updateLabel = "Resume graph updates"
	}

	return html.Section(
		html.Props{
			ID: "graph-panel",
			Data: map[string]string{
				"graph-renders": strconv.Itoa(renderCount.Get()),
				"patch-count":   strconv.Itoa(patchCount.Get()),
				"node-count":    strconv.Itoa(nodeCount.Get()),
			},
			Class: className(panelRules()...),
		},
		html.Div(
			html.Props{Class: className(u.Flex, u.JustifyBetween, u.ItemsCenter, u.GapV(css.Px(12)))},
			html.Div(
				html.Props{},
				html.P(html.Props{Class: eyebrowClass()}, html.Text("TASK-SCOPED GRAPH")),
				html.H2(
					html.Props{Class: sectionHeadingClass()},
					html.Text(fmt.Sprintf("%d directed nodes", nodeCount.Get())),
				),
			),
			html.Div(
				html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(8)))},
				actionButton("patch-graph", "Apply graph patch", patchGraph),
				actionButton("toggle-graph-updates", updateLabel, toggleUpdates),
			),
		),
		html.Div(
			html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(8)))},
			actionButton("graph-300", "300 nodes", use300Nodes),
			actionButton("graph-600", "600 nodes", use600Nodes),
			actionButton("graph-900", "900 nodes", use900Nodes),
			actionButton("graph-1200", "1,200 nodes", use1200Nodes),
		),
		html.Div(
			html.Props{
				ID:       "graph-interaction",
				Role:     "application",
				TabIndex: html.TabIndexZero,
				Aria: map[string]string{
					"label":       "Synthetic directed graph. Use arrow keys to move the active node.",
					"description": "graph-instructions",
				},
				OnKeyDown: keyGraph,
				Class: className(
					css.MarginY(css.Px(12)),
					css.Border(css.Px(1), css.Hex("#17394a")),
					css.Rounded(css.Px(12)),
					css.OverflowX.Auto,
				),
			},
			html.P(
				html.Props{ID: "graph-instructions", Class: captionClass()},
				html.Text("Arrow keys move focus evidence; Enter is not required."),
			),
			html.Svg(
				html.Props{
					ID:     "synthetic-graph",
					Width:  "740",
					Height: strconv.Itoa(graphHeight),
					Role:   "img",
					Aria: map[string]string{
						"label": fmt.Sprintf("%d-node directed synthetic graph", nodeCount.Get()),
					},
					Raw: map[string]any{"viewBox": fmt.Sprintf("0 0 740 %d", graphHeight)},
				},
				graphChildren...,
			),
		),
		html.Div(
			html.Props{Class: className(u.Flex, u.FlexWrap.Wrap, u.GapV(css.Px(12)))},
			metric("Active node", strconv.Itoa(activeNode.Get()+1), "active-node"),
			metric("Graph patches", strconv.Itoa(patchCount.Get()), "graph-patches"),
			metric("Graph renders", strconv.Itoa(renderCount.Get()), "graph-renders"),
			metric("SVG nodes", strconv.Itoa(nodeCount.Get()), "graph-node-count"),
		),
		html.P(
			html.Props{ID: "frame-evidence", Role: "status", Class: captionClass()},
			html.Text(frameEvidence.Get()),
		),
	)
}

func runTransportStream(
	ctx context.Context,
	status state.Atom[string],
	lastSequence ui.State[uint64],
	tokenBatches ui.State[int],
) {
	after := lastSequence.Get()
	for after < syntheticEventCount {
		if ctx.Err() != nil {
			status.Set("cancelled")
			return
		}
		status.Set("connecting")
		connection, err := grpctunnel.DialContext(
			ctx,
			"/grpc",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxClientMessageSize)),
		)
		if err != nil {
			status.Set("reconnecting")
			if !waitContext(ctx, 250*time.Millisecond) {
				status.Set("cancelled")
				return
			}
			continue
		}
		client := codefluxv1.NewTransportSpikeServiceClient(connection)
		health, err := client.CheckHealth(ctx, &codefluxv1.TransportSpikeServiceCheckHealthRequest{})
		if err != nil || health.GetStatus() != "ready" {
			_ = connection.Close()
			status.Set("reconnecting")
			if !waitContext(ctx, 250*time.Millisecond) {
				status.Set("cancelled")
				return
			}
			continue
		}
		status.Set("streaming")
		stream, err := client.SubscribeSession(ctx, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
			AfterSequence:     after,
			EventCount:        uint32(syntheticEventCount - after),
			PayloadBytes:      32,
			BatchMilliseconds: 100,
		})
		if err != nil {
			_ = connection.Close()
			status.Set("reconnecting")
			continue
		}
		lastPaint := time.Now()
		for {
			event, receiveErr := stream.Recv()
			if receiveErr != nil {
				_ = connection.Close()
				if errorsIsEOF(receiveErr) && after == syntheticEventCount {
					status.Set("complete")
					return
				}
				status.Set("reconnecting")
				break
			}
			if event.GetSequence() != after+1 {
				_ = connection.Close()
				status.Set("sequence-error")
				return
			}
			after = event.GetSequence()
			if time.Since(lastPaint) >= 40*time.Millisecond || after == syntheticEventCount {
				lastSequence.Set(after)
				tokenBatches.Update(func(previous int) int { return previous + 1 })
				lastPaint = time.Now()
			}
			if after == syntheticEventCount {
				_ = connection.Close()
				status.Set("complete")
				return
			}
		}
	}
	status.Set("complete")
}

func checkEditorCapability(status ui.State[string]) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := grpctunnel.DialContext(
		ctx,
		"/grpc",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		status.Set("editor bridge unavailable")
		return
	}
	defer connection.Close()
	response, err := codefluxv1.NewTransportSpikeServiceClient(connection).CheckEditorCapability(
		ctx,
		&codefluxv1.TransportSpikeServiceCheckEditorCapabilityRequest{
			RelativePath: "internal/transportspike/server.go",
			Line:         1,
			Column:       1,
		},
	)
	if err != nil {
		status.Set("editor capability rejected")
		return
	}
	status.Set(response.GetDecision())
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (control *transportControl) set(cancel context.CancelFunc) uint64 {
	control.mu.Lock()
	defer control.mu.Unlock()
	control.generation++
	control.cancel = cancel
	return control.generation
}

func (control *transportControl) clear(generation uint64) {
	control.mu.Lock()
	defer control.mu.Unlock()
	if generation != control.generation {
		return
	}
	control.cancel = nil
}

func (control *transportControl) cancelActive() {
	control.mu.Lock()
	cancel := control.cancel
	control.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func buildSyntheticThreads() []threadItem {
	items := make([]threadItem, syntheticThreadCount)
	for index := range items {
		items[index] = threadItem{
			ID:    fmt.Sprintf("thread-%05d", index+1),
			Title: fmt.Sprintf("Thread %05d · evidence stream", index+1),
		}
	}
	return items
}

func className(rules ...css.Rule) string {
	return css.New(rules...).String()
}

func panelRules() []css.Rule {
	return []css.Rule{
		u.Flex,
		u.FlexCol,
		css.Flex(css.Num(1), css.Num(1), css.Px(280)),
		css.Gap(css.Px(8)),
		css.Padding(css.Px(18)),
		css.Bg(css.Hex("#0c1b25")),
		css.Border(css.Px(1), css.Hex("#17394a")),
		css.Rounded(css.Px(16)),
		css.MinWidth(css.Px(280)),
	}
}

func eyebrowClass() string {
	return className(
		css.Margin(css.Zero),
		css.TextColor(css.Hex("#65d1d0")),
		css.FontSize(css.Rem(0.72)),
		css.FontWeight.Semibold,
		css.Tracking(css.Rem(0.16)),
	)
}

func headingClass() string {
	return className(
		css.Margin(css.Zero),
		css.TextColor(css.Hex("#f2f7f8")),
		css.FontSize(css.Rem(2.1)),
		css.FontWeight.Bold,
		css.LineHeight(css.Num(1.1)),
	)
}

func sectionHeadingClass() string {
	return className(
		css.Margin(css.Zero),
		css.TextColor(css.Hex("#f2f7f8")),
		css.FontSize(css.Rem(1.2)),
		css.FontWeight.Semibold,
	)
}

func bodyCopyClass() string {
	return className(
		css.MaxWidth(css.Ch(68)),
		css.TextColor(css.Hex("#a9c0cb")),
		css.FontSize(css.Rem(1)),
		css.LineHeight(css.Num(1.6)),
	)
}

func captionClass() string {
	return className(
		css.Margin(css.Zero),
		css.TextColor(css.Hex("#89a5b2")),
		css.FontSize(css.Rem(0.82)),
		css.LineHeight(css.Num(1.4)),
	)
}

func statusClass() string {
	return className(
		css.PaddingX(css.Px(12)),
		css.PaddingY(css.Px(7)),
		css.Bg(css.Hex("#133841")),
		css.Border(css.Px(1), css.Hex("#2b6d72")),
		css.Rounded(css.Px(999)),
		css.TextColor(css.Hex("#a9f0e7")),
		css.FontSize(css.Rem(0.78)),
		css.FontWeight.Semibold,
	)
}

func buttonClass() string {
	return className(
		css.PaddingX(css.Px(14)),
		css.PaddingY(css.Px(9)),
		css.Bg(css.Hex("#d99b3b")),
		css.Border(css.Px(1), css.Hex("#f5c66c")),
		css.Rounded(css.Px(10)),
		css.TextColor(css.Hex("#1d1408")),
		css.Font(css.SansStack),
		css.FontSize(css.Rem(0.84)),
		css.FontWeight.Semibold,
		css.Cursor.Pointer,
	)
}

func actionButton(id string, label string, handler ui.Handler) ui.Node {
	return html.Button(
		html.Props{ID: id, Type: "button", Class: buttonClass(), OnClick: handler},
		html.Text(label),
	)
}

func metric(label string, value string, id string) ui.Node {
	return html.Div(
		html.Props{
			Class: className(
				u.Flex,
				u.FlexCol,
				u.GapV(css.Px(4)),
				css.Padding(css.Px(10)),
				css.Bg(css.Hex("#102a36")),
				css.Rounded(css.Px(10)),
				css.MinWidth(css.Px(110)),
			),
		},
		html.Span(html.Props{Class: eyebrowClass()}, html.Text(label)),
		html.Strong(html.Props{ID: id}, html.Text(value)),
	)
}

func threadRowClass(active bool) string {
	background := css.Hex("#0e222d")
	foreground := css.Hex("#b6cad3")
	border := css.Hex("#17394a")
	if active {
		background = css.Hex("#154652")
		foreground = css.Hex("#ecfbf8")
		border = css.Hex("#4ea5a3")
	}
	return className(
		u.Flex,
		u.ItemsCenter,
		css.W(css.Full),
		css.H(css.Full),
		css.PaddingX(css.Px(12)),
		css.Bg(background),
		css.Border(css.Px(1), border),
		css.Rounded(css.Px(8)),
		css.TextColor(foreground),
		css.Font(css.SansStack),
		css.TextAlign.Start,
		css.Cursor.Pointer,
	)
}
