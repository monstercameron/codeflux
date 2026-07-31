package shell

import (
	"context"

	"github.com/monstercameron/GoWebComponents/v5/interop"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func copyTimelineText(value string) {
	ui.SafeGo("copy timeline text", func() {
		clipboard, err := interop.GetClipboard()
		if err != nil {
			return
		}
		_ = clipboard.WriteText(context.Background(), value)
	})
}
