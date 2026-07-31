package preferences

import (
	"fmt"

	"codeflux.dev/codeflux/web/frontend/routes"
)

// RestoredPreferences contains only an accepted, currently authorized route
// and normalized UI preferences.
type RestoredPreferences struct {
	Record Record
	Route  routes.Route
}

// RestorationError preserves the policy reason without treating the stored
// destination as authority.
type RestorationError struct {
	Reason routes.RestoreReason
}

func (e *RestorationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrRestorationRejected, e.Reason)
}

func (e *RestorationError) Unwrap() error { return ErrRestorationRejected }

// Restore decodes a preference envelope and accepts its route only after the
// current server-confirmed restoration context approves it.
func Restore(payload []byte, context routes.RestorationContext) (RestoredPreferences, error) {
	record, err := Decode(payload)
	if err != nil {
		return RestoredPreferences{}, err
	}
	return RestoreRecord(record, context)
}

// RestoreRecord applies the same authorization and compatibility gate to an
// already decoded record.
func RestoreRecord(record Record, context routes.RestorationContext) (RestoredPreferences, error) {
	normalized, err := normalizeRecord(record)
	if err != nil {
		return RestoredPreferences{}, err
	}
	restoration := routes.Restore(normalized.LastRoute, context)
	if restoration.Reason != routes.RestoreAccepted {
		return RestoredPreferences{}, &RestorationError{Reason: restoration.Reason}
	}
	return RestoredPreferences{Record: normalized, Route: restoration.Route}, nil
}
