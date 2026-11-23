// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

// BindOption is one entry in the listen-address popup.
type BindOption struct {
	Label string
	Value string
}

// LangOption is one entry in the language picker. An empty Code means
// "follow the system language".
type LangOption struct {
	Code string
	Name string
}

// Options describes what the window shows and which callbacks run on user action.
// StatDevice is one address shown on the overview tab. Seen is already
// formatted in the active language, so the platform code only lays it out.
type StatDevice struct {
	Addr string
	Seen string
	Hits int64
}

// Stats is everything the overview tab displays. All text arrives formatted:
// this package has no clock, no locale rules and no filesystem access, which is
// what keeps the three renderers free of business logic.
type Stats struct {
	Uptime    string
	Devices   []StatDevice
	Hourly    []int64
	Recent    []string
	Requests  int64
	Files     int
	Dirs      int
	Truncated bool
}

type Options struct {
	// URL is the address of the local web server.
	URL string
	// Port is the port currently being listened on, shown in the input field.
	Port int
	// Bind is the current listen address, such as 127.0.0.1, 0.0.0.0 or an interface IP.
	Bind string
	// BindOptions lists the selectable listen addresses for the popup.
	BindOptions []BindOption
	// Root is the folder currently used as the site root.
	Root string
	// RootMissing means Root does not exist yet and the user should pick a folder.
	RootMissing bool
	// Stats feeds the overview tab. The zero value renders an empty state.
	Stats Stats
	// RefreshStats is called by the window on a timer and whenever the overview
	// tab becomes visible. nil leaves the window showing its first snapshot.
	RefreshStats func() Stats
	// AuthEnabled says whether the browser must send a username and password.
	AuthEnabled bool
	// AuthUsername is the current username, used to prefill the input field.
	AuthUsername string

	// ChooseRoot runs after the user picks a new root folder.
	// On error the UI should show the error and keep displaying the previous folder.
	ChooseRoot func(dir string) error
	// SetPort runs when the user submits a new port; on success it returns the new URL.
	// On error the UI should show the error and keep the previous port.
	SetPort func(port int) (string, error)
	// SetBind runs when the user switches the listen address; on success it returns the new URL.
	SetBind func(bind string) (string, error)
	// SetAuth runs when the user submits the access password.
	// An empty username and password turn the password off; on error the UI should
	// report it and keep the previous state.
	SetAuth func(username, password string) error
	// AccessInfo returns the current access log text for the "Active connections" window.
	AccessInfo func() string

	// Languages lists the entries of the language picker, the system default
	// first, in the order they should appear.
	Languages []LangOption
	// Language is the active language code; empty means "follow the system".
	Language string
	// SetLanguage runs after the user picks another language. It switches the
	// active translation and returns the listen-address entries with their
	// labels retranslated.
	SetLanguage func(code string) []BindOption
}
