package main

type developmentProfile struct {
	Name             string   `json:"name"`
	Purpose          string   `json:"purpose"`
	Database         string   `json:"database"`
	CredentialStore  string   `json:"credential_store"`
	Provider         string   `json:"provider"`
	Clock            string   `json:"clock"`
	IDGenerator      string   `json:"id_generator"`
	Network          string   `json:"network"`
	FaultBoundaries  []string `json:"fault_boundaries,omitempty"`
	ExternalProvider bool     `json:"external_provider"`
}

func developmentProfiles() []developmentProfile {
	return []developmentProfile{
		{
			Name:            "deterministic",
			Purpose:         "Default offline development and test profile.",
			Database:        "new temporary SQLite target",
			CredentialStore: "in-memory fake credentials",
			Provider:        "fixed scripted provider",
			Clock:           "fixed deterministic clock",
			IDGenerator:     "seeded deterministic sequence",
			Network:         "loopback listener only; external network disabled",
		},
		{
			Name:            "interactive-fake",
			Purpose:         "Interactive scripted approval, streaming, failure, disconnect, and recovery scenarios.",
			Database:        "temporary SQLite target",
			CredentialStore: "in-memory fake credentials",
			Provider:        "interactive scripted provider",
			Clock:           "controllable test clock",
			IDGenerator:     "seeded deterministic sequence",
			Network:         "loopback listener only; external network disabled",
		},
		{
			Name:             "live-provider",
			Purpose:          "Explicit real-provider profile with visible identity and cost warning.",
			Database:         "explicit non-test application database",
			CredentialStore:  "operating-system credential reference",
			Provider:         "explicit provider and model",
			Clock:            "system clock",
			IDGenerator:      "production random identifiers",
			Network:          "provider network explicitly enabled",
			ExternalProvider: true,
		},
		{
			Name:            "fault-injection",
			Purpose:         "Deterministic named failures at durable boundaries.",
			Database:        "temporary SQLite target",
			CredentialStore: "in-memory fake credentials",
			Provider:        "scripted provider with named failures",
			Clock:           "controllable test clock",
			IDGenerator:     "seeded deterministic sequence",
			Network:         "loopback listener only; external network disabled",
			FaultBoundaries: []string{
				"before-event-commit",
				"after-event-commit",
				"before-live-publication",
				"after-live-publication",
				"before-edit",
				"after-edit",
				"before-checkpoint",
				"after-checkpoint",
				"during-provider-stream",
				"during-command-process-tree",
				"during-migration",
				"during-graph-patch",
				"during-reconnect-replay",
			},
		},
	}
}
