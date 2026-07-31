// Package evidencereport renders the immutable final evidence report as a
// typed GoWebComponents report card. SQLite report data remains authoritative.
package evidencereport

import (
	"errors"
	"fmt"

	reportevidence "codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

var ErrInvalidProps = errors.New("invalid final evidence report props")

type Props struct {
	Report reportevidence.Report
	Mode   primitives.Mode
}

func (props Props) Validate() error {
	if err := props.Report.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidProps, err)
	}
	return nil
}
