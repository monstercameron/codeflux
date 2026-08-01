package composer

import (
	"testing"

	"codeflux.dev/codeflux/internal/fingerprint"
)

func TestEveryOfferedKindIsOneTheCoordinatorAccepts(t *testing.T) {
	// A choice the coordinator refuses is a control that fails only after
	// somebody has written their request and pressed send.
	for _, choice := range TaskClassChoices {
		if choice.Value == "" {
			continue
		}
		if !fingerprint.TaskClass(choice.Value).IsValid() {
			t.Errorf("the composer offers %q, which intake refuses", choice.Value)
		}
	}
}

func TestEveryKindTheCoordinatorAcceptsIsOffered(t *testing.T) {
	// A class that exists but cannot be chosen is work a person cannot ask for.
	offered := map[string]bool{}
	for _, choice := range TaskClassChoices {
		offered[choice.Value] = true
	}
	for _, class := range fingerprint.AllTaskClasses() {
		if !offered[string(class)] {
			t.Errorf("intake accepts %q, which the composer never offers", class)
		}
	}
}

func TestNoKindIsPreselected(t *testing.T) {
	// A silently pre-selected class is a guess wearing the person's authority,
	// and it lands inside the fingerprint that gates memory retrieval and
	// routing. The first entry must be a prompt, not an answer.
	if len(TaskClassChoices) == 0 {
		t.Fatal("no kinds are offered at all")
	}
	if TaskClassChoices[0].Value != "" {
		t.Errorf("the first offered kind is %q, which pre-selects an answer",
			TaskClassChoices[0].Value)
	}
}
