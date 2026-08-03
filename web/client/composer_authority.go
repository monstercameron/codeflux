package main

import (
	"codeflux.dev/codeflux/web/frontend/composer"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskactionview"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

type authoritativeComposerCallbacks = taskactionview.Callbacks

func bindAuthoritativeComposerTaskActionsWithCallbacks(
	props composer.Props,
	task taskprojection.TaskProjection,
	connection frontendstate.ConnectionState,
	controls *taskcontrols.Props,
	callbacks authoritativeComposerCallbacks,
) composer.Props {
	return taskactionview.Bind(
		props, task, taskprojection.ConnectionProjection(connection), controls, callbacks,
	)
}
