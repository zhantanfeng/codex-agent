package desktop

import (
	"context"
	"testing"
)

func TestMacSystemQuitIsNotTreatedAsWindowClose(t *testing.T) {
	a := &App{}
	a.trayReady.Store(true)

	// Dock quit requests do not go through App.Quit to set the quitting flag.
	if a.BeforeClose(context.Background()) {
		t.Fatal("system quit was prevented while the app was running in the background")
	}
}
