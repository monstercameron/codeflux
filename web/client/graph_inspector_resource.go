package main

import (
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/graphinspector"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

type graphInspectorResourceActions struct {
	Mode                primitives.Mode
	ComparisonAvailable bool
	OnExplainInChat     func(domain.NodeID)
	OnExpandNeighbors   func(domain.NodeID)
	OnDependencyCone    func(domain.NodeID)
	OnEvidenceCone      func(domain.NodeID)
	OnCompareRevision   func(domain.GraphRevisionID)
	OnOpenInEditor      func(string, uint32)
}

func graphInspectorProps(resource graphNodeResource, revisionID domain.GraphRevisionID, actions graphInspectorResourceActions) (graphinspector.Props, error) {
	if revisionID.IsZero() {
		return graphinspector.Props{}, errGraphResourceScopeUnavailable
	}
	locations := make([]graphinspector.SourceLocation, 0, len(resource.SourceLocations))
	for _, value := range resource.SourceLocations {
		location, err := graphinspector.NewSourceLocation(value.RepositoryID, value.RepositoryRevision, value.RelativePath, value.StartLine, value.StartColumn, value.EndLine, value.EndColumn)
		if err != nil {
			return graphinspector.Props{}, errors.Join(errGraphResourceMalformed, err)
		}
		locations = append(locations, location)
	}
	props := graphinspector.Props{
		Mode: actions.Mode, RevisionID: revisionID, Node: resource.Node,
		Evidence:        graphinspector.EvidenceView{UnknownReason: "supporting evidence detail is not included in the graph node query"},
		Duration:        graphinspector.DurationAttribution{UnknownReason: "node duration attribution is not modeled"},
		Tokens:          graphinspector.TokenAttribution{UnknownReason: "node token attribution is not modeled"},
		Cost:            graphinspector.CostAttribution{UnknownReason: "node cost attribution is not modeled"},
		RelatedMessages: resource.MessageIDs, SourceLocations: locations,
		OnExplainInChat: actions.OnExplainInChat, OnExpandNeighbors: actions.OnExpandNeighbors,
		OnIsolateDependencyCone: actions.OnDependencyCone, OnIsolateEvidenceCone: actions.OnEvidenceCone,
		OnCompareRevision: actions.OnCompareRevision, OnOpenInEditor: actions.OnOpenInEditor,
	}
	props.Actions = graphinspector.Actions{
		ExplainInChat:   graphInspectorAction(actions.OnExplainInChat != nil, "Chat explanation is unavailable."),
		ExpandNeighbors: graphInspectorAction(actions.OnExpandNeighbors != nil, "Neighbor expansion is unavailable."),
		DependencyCone:  graphInspectorAction(actions.OnDependencyCone != nil, "Dependency-cone isolation is unavailable."),
		EvidenceCone:    graphInspectorAction(actions.OnEvidenceCone != nil, "Evidence-cone isolation is unavailable."),
		CompareRevision: graphInspectorAction(actions.ComparisonAvailable && actions.OnCompareRevision != nil, "No comparable graph revision is available."),
		OpenInEditor:    graphInspectorAction(len(locations) > 0 && actions.OnOpenInEditor != nil, "No validated source location is available."),
	}
	if err := props.Validate(); err != nil {
		return graphinspector.Props{}, errors.Join(errGraphResourceMalformed, err)
	}
	return props, nil
}

func graphInspectorAction(enabled bool, reason string) graphinspector.ActionAvailability {
	if enabled {
		return graphinspector.ActionAvailability{Enabled: true}
	}
	return graphinspector.ActionAvailability{DisabledReason: reason}
}
