package settingsview

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The settings surface is a specification sheet, not a page of panels.
//
// It was seven rounded cards in a two-column grid, five thousand pixels tall,
// where each setting was a label, a paragraph of prose, and a full-width
// control at equal weight. The value — the thing somebody opens this page to
// read — was the least prominent element on it, and the two columns yoked
// their heights so a short card sat beside two thousand pixels of empty
// surface.
//
// What replaces it is the document this content actually is: every choice this
// machine runs under, on one strict grid, with the values on a single vertical
// axis so the whole configuration can be read in one pass. The page speaks in
// two voices, which the console's type system already provides and this page
// had been using interchangeably: machine values in mono, human rationale in
// the reading serif. Hairlines separate sections; nothing is boxed.

// valueColumnWidth is the shared axis every value is set against.
//
// It is fixed rather than content-sized because the axis is the point: a
// column that moved with the longest value would stop being a column. It is
// wide enough for the longest value the coordinator sends — an escalation
// ladder, a model with its effort — because a value that wraps is a value
// somebody reads twice.
const valueColumnWidth = 360

// controlColumnWidth is reserved on every row, including rows with no control.
//
// Reserving it is what keeps the axis straight. Without it a value standing
// alone would sit against the edge of the sheet while a value beside a control
// sat 170 pixels to its left, and the column somebody reads down would zigzag.
const controlColumnWidth = 170

// controlHeight is the height every control in a row shares.
//
// A settings row is read far more often than it is touched: this is a desktop
// document, and a control scaled for a fingertip, set beside prose two sizes
// smaller than itself, is most of what makes a page of them look like a toy.
// The controls here are the size of their own text plus the space it needs.
const controlHeight = 30

// fieldHeight is the height of anything typed into.
//
// It is taller than a control because a text field holds a caret and has to
// look like it takes input, and shorter than a fingertip target because
// nothing on this page is reached by one.
const fieldHeight = 36

// rationaleMeasure bounds the prose column at a readable line length.
//
// The sheet fills the pane, but a sentence set across a wide pane is a
// sentence nobody finishes.
const rationaleMeasure = 620

// The sheet holds panels it does not draw — the choices that belong to this
// browser rather than to a run. Those components cannot line up with the axis
// unless they are told where it is, and a component that guessed would line up
// only by accident, so the geometry is published rather than duplicated.
const (
	ValueColumnWidth   = valueColumnWidth
	ControlColumnWidth = controlColumnWidth
	RationaleMeasure   = rationaleMeasure
)

// Sheet renders the whole settings surface.
func Sheet(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	if node, handled := stateNode(props, "settings"); handled {
		return html.Div(html.Props{
			Class: css.New(css.Padding(css.Px(tokens.Spacing.XXL))).String(),
			Data:  map[string]string{"component": "settings-sheet"},
		}, node)
	}
	body := []ui.Node{
		sheetMasthead(props),
		operatingContract(props),
		spendSection(props),
		spendModelSection(props),
		credentialsSection(props),
		runBehaviourSection(props),
		routingSection(props),
		catalogueSection(props),
		machineSection(props),
	}
	return html.Div(
		html.Props{
			Data: map[string]string{"component": "settings-sheet"},
			Class: css.New(
				u.Flex, u.FlexCol,
				css.Gap(css.Px(tokens.Spacing.XXL)),
				css.W(css.Full), css.MinWidth(css.Zero),
				css.Custom("padding-bottom", "160px"),
			).String(),
		},
		body...,
	)
}

// sheetMasthead names the document and what it governs.
func sheetMasthead(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	return html.Header(
		html.Props{
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
		},
		html.H1(html.Props{
			Text: "Settings",
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Display)),
				css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Margin(css.Zero),
			).String(),
		}),
		html.P(html.Props{
			Text: "Everything this machine will do when you start a run, and the " +
				"values it will do it with.",
			Class: proseClass(props, tokens.Colors.TextSecondary),
		}),
		searchBox(props),
	)
}

// operatingContract is the answer to the only question somebody asks before
// starting work: what happens if I run this now.
//
// It is the same grid as every other row on the sheet rather than a separate
// widget, because it is not a summary of the page — it is the page's most
// important four rows, read first.
func operatingContract(props Props) ui.Node {
	rows := []ui.Node{
		contractRow(props, "model", props.Policy.contractModel(), props.Policy.Known),
		contractRow(props, "credential", props.contractCredential(), true),
		contractRow(props, "attempts", props.contractAttempts(), len(props.Flow) > 0),
		contractRow(props, "verification", props.contractVerification(), len(props.Flow) > 0),
	}
	return section(props, "Operating contract", "", rows)
}

// contractRow is one line of the readout.
func contractRow(props Props, name, value string, known bool) ui.Node {
	if !known || value == "" {
		value = "not answered"
	}
	return row(props, rowProps{
		Name:  name,
		Value: html.Span(html.Props{Text: value, Class: contractValueClass(props, known)}),
	})
}

// credentialsSection is the one thing on this page that blocks work.
func credentialsSection(props Props) ui.Node {
	usable := 0
	for _, provider := range props.Providers {
		if provider.Available {
			usable++
		}
	}
	note := ""
	if len(props.Providers) > 0 {
		note = countOf(len(props.Providers), "provider", "providers") + ", " +
			strconv.Itoa(usable) + " usable"
	}
	if len(props.Providers) == 0 {
		return section(props, "Credentials", note, []ui.Node{
			emptyLine(props,
				"No provider is recorded. The coordinator records one the first "+
					"time it prepares work for it."),
		})
	}
	rows := make([]ui.Node, 0, len(props.Providers))
	for _, provider := range props.Providers {
		rows = append(rows, providerRow(props, provider))
	}
	rows = append(rows, noteLine(props,
		"A credential lives in this machine's operating-system credential store. "+
			"CodeFlux keeps an os://service/account reference to it and never "+
			"displays or transmits the credential itself."))
	return section(props, "Credentials", note, rows)
}

// runBehaviourSection is the sheet's body: every choice the engine leaves open.
func runBehaviourSection(props Props) ui.Node {
	if len(props.Flow) == 0 {
		return section(props, "Run behaviour", "", []ui.Node{
			emptyLine(props, "The coordinator described no choice the run flow leaves open."),
		})
	}
	changed, departed := 0, 0
	for _, setting := range props.Flow {
		if _, pending := props.FlowPending[setting.Key]; pending {
			changed++
		}
		if !setting.AtDefault {
			departed++
		}
	}
	note := countOf(len(props.Flow), "setting", "settings")
	if departed > 0 {
		note += ", " + strconv.Itoa(departed) + " off default"
	}
	if changed > 0 {
		note += ", " + strconv.Itoa(changed) + " unsaved"
	}
	if len(props.FlowUnrenderable) > 0 {
		note += ", " + strconv.Itoa(len(props.FlowUnrenderable)) + " not shown"
	}
	rows := []ui.Node{}
	for _, group := range flowGroups(props.Flow) {
		rows = append(rows, groupLine(props, group))
		for _, setting := range props.Flow {
			if setting.Group != group {
				continue
			}
			rows = append(rows, settingRow(props, effectiveFlowSetting(props, setting)))
		}
	}
	if props.FlowNotice != "" {
		rows = append([]ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Run settings", Message: props.FlowNotice,
			Tone: props.FlowTone, Mode: props.Mode,
		})}, rows...)
	}
	if len(props.FlowUnrenderable) > 0 {
		rows = append(rows, noteLine(props,
			"This coordinator declares settings this interface cannot draw yet: "+
				strings.Join(props.FlowUnrenderable, ", ")+
				". They still govern a run."))
	}
	return section(props, "Run behaviour", note, rows)
}

// routingSection reports what is fixed, and says so.
func routingSection(props Props) ui.Node {
	if !props.Policy.Known {
		return section(props, "Routing", "fixed for this prototype", []ui.Node{
			emptyLine(props, "The coordinator has not reported the policy governing runs."),
		})
	}
	rows := []ui.Node{
		readOnlyRow(props, "preset", props.Policy.Preset),
		readOnlyRow(props, "reasoning effort", props.Policy.ReasoningEffort),
		readOnlyRow(props, "risk floor", props.Policy.RiskFloor),
		readOnlyRow(props, "assurance floor", props.Policy.AssuranceFloor),
	}
	if props.Policy.RequestTimeout > 0 {
		rows = append(rows, readOnlyRow(props, "request timeout", props.Policy.RequestTimeout.String()))
	}
	rows = append(rows, noteLine(props,
		"Routing uses one versioned policy through prototype exit. These values "+
			"are reported so you can see what governs a run; nothing here changes "+
			"them. "+props.Policy.sourceSentence()))
	return section(props, "Routing", "fixed", rows)
}

// catalogueSection lists what the coordinator can actually call.
func catalogueSection(props Props) ui.Node {
	rows := []ui.Node{}
	for _, provider := range props.Providers {
		for _, model := range provider.Models {
			rows = append(rows, row(props, rowProps{
				Name:  provider.Name + " · " + model.Name,
				Value: html.Span(html.Props{Text: availabilityWord(model.Available), Class: valueTextClass(props, model.Available)}),
			}))
		}
	}
	if len(rows) == 0 {
		return section(props, "Models", "", []ui.Node{
			emptyLine(props,
				"No model is catalogued. A model is recorded with the exact revision "+
					"and capabilities the coordinator observed for it."),
		})
	}
	return section(props, "Models", countOf(len(rows), "model", "models"), rows)
}

// machineSection holds what belongs to this browser rather than to a run.
//
// The panels inside it are owned by the surfaces that hold those choices, so
// this places and bounds them rather than redrawing them: each gets the same
// group heading the run settings use, and a measure, so a section of borrowed
// components still reads as part of one sheet instead of three pages stacked.
func machineSection(props Props) ui.Node {
	blocks := []ui.Node{}
	for _, panel := range []struct {
		title string
		node  ui.Node
		bound int
		// onAxis says the panel sets a value to the right of its label, as every
		// row of the sheet does, so it is given the sheet's full width and its
		// values land on the same column. A panel that is prose is not: a
		// sentence is held to the measure the rest of the page reads at.
		onAxis bool
	}{
		{"Appearance", props.Appearance, 0, true},
		{"Data", props.LocalData, 0, false},
		// Local telemetry is a log. Left unbounded it is fifty rows of
		// timestamps, which would make the longest block on this sheet the one
		// nobody came here to read.
		{"Local telemetry", props.Telemetry, 460, true},
	} {
		if panel.node == nil {
			continue
		}
		blocks = append(blocks,
			groupLine(props, panel.title),
			machinePanel(props, panel.node, panel.bound, panel.onAxis),
		)
	}
	if len(blocks) == 0 {
		return html.Fragment()
	}
	return section(props, "This machine", "not sent anywhere", blocks)
}

// machinePanel bounds one borrowed panel to the sheet's measure.
func machinePanel(props Props, panel ui.Node, height int, onAxis bool) ui.Node {
	tokens := props.Mode.Tokens()
	rules := []css.Rule{css.MinWidth(css.Zero)}
	if !onAxis {
		rules = append(rules, css.MaxWidth(css.Px(rationaleMeasure)))
	}
	rules = append(rules,
		css.Custom("padding-bottom", strconv.Itoa(tokens.Spacing.MD)+"px"),
		// The borrowed panels set no size on their own prose, so it arrives at
		// the page's default and reads two steps louder than every sentence
		// around it. Setting the sheet's own reading voice here brings the
		// text they inherit into line without reaching into their components.
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	)
	if height > 0 {
		rules = append(rules,
			css.MaxHeight(css.Px(height)),
			css.OverflowY.Auto,
		)
	}
	return html.Div(
		html.Props{
			Data:  map[string]string{"component": "settings-machine-panel"},
			Class: css.New(rules...).String(),
		},
		panel,
	)
}
