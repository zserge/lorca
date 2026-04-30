package lorca

import "encoding/json"

// browserImpl is the internal interface each browser backend implements.
// ui holds a browserImpl and delegates all browser interactions through it.
type browserImpl interface {
	eval(expr string) (json.RawMessage, error)
	load(url string) error
	bounds() (Bounds, error)
	setBounds(Bounds) error
	injectScript(js string) error    // registers script for all future docs + runs on current page
	injectBinding(name string) error // like injectScript but avoids cross-realm calls for Firefox
	setBlockBackNavigation(enable bool)
	setAppUserModelID(id string)
	kill()
	done() <-chan struct{}
}

// Compile-time interface checks.
var (
	_ browserImpl = (*chrome)(nil)
	_ browserImpl = (*firefox)(nil)
)
