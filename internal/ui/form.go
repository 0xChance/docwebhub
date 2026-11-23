// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"log"
	"path"
	"strconv"
	"strings"

	"github.com/0xChance/docwebhub/internal/i18n"
	"github.com/0xChance/docwebhub/internal/openurl"
)

// The main window is a small settings form. Its structure, labels, values and
// actions live here once; AppKit, Win32 and GTK each render the same description
// with their native widgets. Platform files only implement widget creation,
// layout and the few dialogs that are genuinely native.
//
// A field kind is one of:
//
//	status, heading, description, link, value, input, number, password,
//	button, select, hint, spacer
//
// The overview tab adds kinds that hold more than one value:
//
//	stat, badge, device-list, sparkline, file-list
//
// A field width is -1 for "take the remaining space", 0 for natural size, or a
// positive logical-point width.
//
// Row kinds are header, url, setting, hint and footer. Consecutive setting rows
// form the settings card.

const formProductName = "docWebHub"

// Row groups. The window shows one group at a time as a tab; "chrome" rows are
// shared by every tab. Renderers that do not implement tabs (the AppKit and
// Win32 ones for now) simply skip the overview group, which is the only one
// they have no widgets for.
const (
	groupOverview = "overview"
	groupSettings = "settings"
	groupSecurity = "security"
	groupLanguage = "language"
	groupChrome   = "chrome"
)

const (
	kindStatus      = "status"
	kindHeading     = "heading"
	kindDescription = "description"
	kindLink        = "link"
	kindValue       = "value"
	kindInput       = "input"
	kindNumber      = "number"
	kindPassword    = "password"
	kindButton      = "button"
	kindSelect      = "select"
	kindHint        = "hint"
	kindSpacer      = "spacer"

	// Overview rows. They carry several values each rather than a label and a
	// control, which is why they get their own kinds instead of being forced
	// into the setting shape.
	kindStat       = "stat"        // one big number with a caption
	kindBadge      = "badge"       // a row of small pills
	kindDeviceList = "device-list" // address, last seen, request count
	kindSparkline  = "sparkline"   // 24 request counts
	kindFileList   = "file-list"   // recently opened documents
)

const (
	styleDefault   = "default"
	styleMuted     = "muted"
	styleFaint     = "faint"
	stylePrimary   = "primary"
	styleSecondary = "secondary"
	styleSuccess   = "success"
	styleWarning   = "warning"
)

const (
	iconCopy   = "copy"
	iconFolder = "folder"
)

const (
	actionOpenURL     = "open-url"
	actionCopyURL     = "copy-url"
	actionChooseRoot  = "choose-root"
	actionApplyPort   = "apply-port"
	actionSetBind     = "set-bind"
	actionApplyAuth   = "apply-auth"
	actionSetLanguage = "set-language"
	actionAccessInfo  = "access-info"
)

const (
	menuActionShow = "show-window"
	menuActionQuit = "quit"
)

type formOption struct {
	label string
	value string
}

type formField struct {
	id            string
	kind          string
	label         string
	value         string
	placeholder   string
	tooltip       string
	accessibility string
	action        string
	style         string
	icon          string
	width         int
	options       []formOption
}

type formRow struct {
	id string
	// group is the tab this row belongs to; groupChrome means every tab.
	group  string
	kind   string
	label  string
	fields []formField
}

type formMenuItem struct {
	label     string
	action    string
	separator bool
}

type formModel struct {
	title string
	rows  []formRow
	menu  []formMenuItem
}

type formController struct {
	opts Options

	url         string
	port        int
	bind        string
	bindOptions []BindOption
	root        string
	rootMissing bool
	authEnabled bool
	authUser    string
	language    string

	form formModel

	// Native renderers use these after an action: changed lists field IDs whose
	// value/style must be re-read, labelsChanged asks them to re-read every
	// label, and actionText carries the access-log dialog body.
	changed       []string
	labelsChanged bool
	errorTitle    string
	actionText    string
}

// activeForm is the window currently shown. The app has exactly one GUI.
var activeForm *formController

func startForm(opts Options) *formController {
	activeForm = newFormController(opts)
	return activeForm
}

func newFormController(opts Options) *formController {
	c := &formController{
		opts:        opts,
		url:         opts.URL,
		port:        opts.Port,
		bind:        opts.Bind,
		bindOptions: append([]BindOption(nil), opts.BindOptions...),
		root:        opts.Root,
		rootMissing: opts.RootMissing,
		authEnabled: opts.AuthEnabled,
		authUser:    opts.AuthUsername,
		language:    opts.Language,
	}
	c.rebuild()
	return c
}

// field returns the field with id from the current form.
func (c *formController) field(id string) (formField, bool) {
	for _, row := range c.form.rows {
		for _, field := range row.fields {
			if field.id == id {
				return field, true
			}
		}
	}
	return formField{}, false
}

// fieldAt returns the field at a row/column position.
func (c *formController) fieldAt(row, column int) (formField, bool) {
	if row < 0 || row >= len(c.form.rows) {
		return formField{}, false
	}
	fields := c.form.rows[row].fields
	if column < 0 || column >= len(fields) {
		return formField{}, false
	}
	return fields[column], true
}

// rebuild recreates the description from the current state and translations.
// The row/field shape is stable so native widgets can keep their row and column
// indexes across updates.
func (c *formController) rebuild() {
	statusText := i18n.T("ui.status.running")
	statusStyle := styleSuccess
	introText := i18n.T("ui.intro.ready")
	rootText := c.root
	rootStyle := styleDefault
	rootTooltip := c.root
	if c.rootMissing {
		statusText = i18n.T("ui.status.noRoot")
		statusStyle = styleWarning
		introText = i18n.T("ui.intro.needRoot")
		rootText = i18n.T("ui.root.empty")
		rootStyle = styleFaint
		rootTooltip = ""
	}

	// Overview text. Everything is formatted here so the platform renderers only
	// place strings; they have no clock and no notion of "2 min ago".
	stats := c.opts.Stats
	uptimeText := stats.Uptime
	if uptimeText == "" {
		uptimeText = i18n.T("ui.overview.empty")
	}
	deviceCountText := strconv.Itoa(len(stats.Devices))
	badgeOnline := deviceCountText
	badgeRequests := humanCount(stats.Requests)
	badgeFiles := humanCount(int64(stats.Files))
	if stats.Truncated {
		badgeFiles += "+"
	}
	devicesText := formatDevices(stats.Devices)
	hourlyText := formatHourly(stats.Hourly)
	recentText := formatRecent(stats.Recent)

	authText := i18n.T("ui.auth.off")
	authStyle := styleMuted
	if c.authEnabled && c.authUser != "" {
		authText = i18n.Tf("ui.auth.on", c.authUser)
		authStyle = styleSuccess
	}

	bindOptions := make([]formOption, len(c.bindOptions))
	for i, option := range c.bindOptions {
		bindOptions[i] = formOption{label: option.Label, value: option.Value}
	}

	languageOptions := make([]formOption, 0, len(c.opts.Languages)+1)
	languageOptions = append(languageOptions, formOption{
		label: i18n.T("ui.language.system"),
		value: "",
	})
	for _, lang := range c.opts.Languages {
		languageOptions = append(languageOptions, formOption{label: lang.Name, value: lang.Code})
	}

	c.form = formModel{
		title: formProductName,
		rows: []formRow{
			{
				id:    "header",
				group: groupOverview,
				kind:  "header",
				fields: []formField{
					{id: "status", kind: kindStatus, value: statusText, style: statusStyle, width: -1},
					{id: "title", kind: kindHeading, value: formProductName, style: styleDefault, width: -1},
					{id: "intro", kind: kindDescription, value: introText, style: styleMuted, width: -1},
				},
			},
			{
				id:    "stats",
				group: groupOverview,
				kind:  "stat",
				fields: []formField{
					{
						id:    "uptime",
						kind:  kindStat,
						label: i18n.T("ui.overview.uptime"),
						value: uptimeText,
						width: -1,
					},
					{
						id:    "device-count",
						kind:  kindStat,
						label: i18n.T("ui.overview.devices"),
						value: deviceCountText,
						width: -1,
					},
				},
			},
			{
				id:    "badges",
				group: groupOverview,
				kind:  "badge",
				fields: []formField{
					{id: "badge-online", kind: kindBadge, value: badgeOnline, label: i18n.T("ui.overview.online")},
					{id: "badge-requests", kind: kindBadge, value: badgeRequests, label: i18n.T("ui.overview.requests")},
					{id: "badge-files", kind: kindBadge, value: badgeFiles, label: i18n.T("ui.overview.files")},
				},
			},
			{
				id:    "devices",
				group: groupOverview,
				kind:  "device-list",
				fields: []formField{
					{
						id:      "devices",
						kind:    kindDeviceList,
						label:   i18n.T("ui.overview.devices.title"),
						value:   devicesText,
						tooltip: i18n.T("ui.overview.devices.title"),
						width:   -1,
					},
				},
			},
			{
				id:    "hourly",
				group: groupOverview,
				kind:  "sparkline",
				fields: []formField{
					{
						id:    "hourly",
						kind:  kindSparkline,
						label: i18n.T("ui.overview.hourly"),
						value: hourlyText,
						width: -1,
					},
				},
			},
			{
				id:    "recent",
				group: groupOverview,
				kind:  "file-list",
				fields: []formField{
					{
						id:    "recent",
						kind:  kindFileList,
						label: i18n.T("ui.overview.recent"),
						value: recentText,
						width: -1,
					},
				},
			},
			{
				id:    "url",
				group: groupChrome,
				kind:  "url",
				fields: []formField{
					{
						id:            "url",
						kind:          kindLink,
						value:         c.url,
						tooltip:       i18n.T("ui.link.tooltip"),
						accessibility: i18n.Tf("ui.link.accessibility", c.url),
						action:        actionOpenURL,
						style:         "link",
						width:         -1,
					},
					{
						id:      "copy",
						kind:    kindButton,
						label:   i18n.T("ui.url.copy"),
						tooltip: i18n.T("ui.url.copy"),
						action:  actionCopyURL,
						style:   styleSecondary,
						icon:    iconCopy,
						width:   124,
					},
				},
			},
			{
				id:    "root",
				group: groupSettings,
				kind:  "setting",
				label: i18n.T("ui.root.label"),
				fields: []formField{
					{
						id:      "root-value",
						kind:    kindValue,
						value:   rootText,
						tooltip: rootTooltip,
						style:   rootStyle,
						width:   -1,
					},
					{
						id:      "choose-root",
						kind:    kindButton,
						label:   i18n.T("ui.root.choose"),
						tooltip: i18n.T("ui.root.choose.tooltip"),
						action:  actionChooseRoot,
						style:   styleSecondary,
						icon:    iconFolder,
						width:   124,
					},
				},
			},
			{
				id:    "port",
				group: groupSettings,
				kind:  "setting",
				label: i18n.T("ui.port.label"),
				fields: []formField{
					{
						id:      "port",
						kind:    kindNumber,
						value:   strconv.Itoa(c.port),
						tooltip: i18n.T("ui.port.tooltip"),
						action:  actionApplyPort,
						style:   styleDefault,
						width:   90,
					},
					{
						id:      "port-apply",
						kind:    kindButton,
						label:   i18n.T("ui.port.apply"),
						tooltip: i18n.T("ui.port.apply.tooltip"),
						action:  actionApplyPort,
						style:   stylePrimary,
						width:   64,
					},
				},
			},
			{
				id:    "bind",
				group: groupSettings,
				kind:  "setting",
				label: i18n.T("ui.bind.label"),
				fields: []formField{
					{
						id:      "bind",
						kind:    kindSelect,
						value:   c.bind,
						tooltip: i18n.T("ui.bind.tooltip"),
						action:  actionSetBind,
						style:   styleDefault,
						width:   230,
						options: bindOptions,
					},
					{
						id:    "bind-hint",
						kind:  kindHint,
						label: i18n.T("ui.bind.hint"),
						style: styleFaint,
						width: -1,
					},
				},
			},
			{
				id:    "auth",
				group: groupSecurity,
				kind:  "setting",
				label: i18n.T("ui.auth.label"),
				fields: []formField{
					{
						id:          "auth-user",
						kind:        kindInput,
						value:       c.authUser,
						placeholder: i18n.T("ui.auth.user.placeholder"),
						tooltip:     i18n.T("ui.auth.user.tooltip"),
						action:      actionApplyAuth,
						style:       styleDefault,
						width:       118,
					},
					{
						id:          "auth-pass",
						kind:        kindPassword,
						value:       "",
						placeholder: i18n.T("ui.auth.pass.placeholder"),
						tooltip:     i18n.T("ui.auth.pass.tooltip"),
						action:      actionApplyAuth,
						style:       styleDefault,
						width:       118,
					},
					{
						id:      "auth-apply",
						kind:    kindButton,
						label:   i18n.T("ui.auth.apply"),
						tooltip: i18n.T("ui.auth.apply.tooltip"),
						action:  actionApplyAuth,
						style:   styleSecondary,
						width:   64,
					},
				},
			},
			{
				id:    "auth-status",
				group: groupSecurity,
				kind:  "setting",
				label: "",
				fields: []formField{
					{
						id:      "auth-status",
						kind:    kindValue,
						value:   authText,
						style:   authStyle,
						width:   -1,
						tooltip: "",
					},
				},
			},
			{
				id:    "hint",
				group: groupChrome,
				kind:  "hint",
				fields: []formField{
					{
						id:    "hint",
						kind:  kindHint,
						label: i18n.T("ui.hint"),
						style: styleFaint,
						width: -1,
					},
				},
			},
			{
				id:    "footer",
				group: groupLanguage,
				kind:  "footer",
				label: i18n.T("ui.language.label"),
				fields: []formField{
					{
						id:      "language",
						kind:    kindSelect,
						value:   c.language,
						tooltip: i18n.T("ui.language.tooltip"),
						action:  actionSetLanguage,
						style:   styleDefault,
						width:   232,
						options: languageOptions,
					},
				},
			},
		},
		menu: []formMenuItem{
			{label: i18n.T("menu.show"), action: menuActionShow},
			{label: i18n.T("menu.open"), action: actionOpenURL},
			{separator: true},
			{label: i18n.T("menu.quit"), action: menuActionQuit},
		},
	}
}

func (c *formController) resetAction() {
	c.changed = nil
	c.labelsChanged = false
	c.errorTitle = ""
	c.actionText = ""
}

func (c *formController) fail(titleKey, message string) string {
	c.errorTitle = i18n.T(titleKey)
	return message
}

// action performs one form action. value1/value2 are the input fields in the
// triggering row, in form order; selects pass their selected value as value1.
// The returned string is an error to show to the user, or empty on success.
func (c *formController) action(action, value1, value2 string) string {
	c.resetAction()

	switch action {
	case actionOpenURL:
		if err := openurl.Open(c.url); err != nil {
			// Opening a browser is best-effort, like the old platform code.
			log.Printf("failed to open browser: %v", err)
		}
		return ""

	case actionChooseRoot:
		if c.opts.ChooseRoot == nil || value1 == "" {
			return ""
		}
		if err := c.opts.ChooseRoot(value1); err != nil {
			return c.fail("alert.root.title", err.Error())
		}
		c.root = value1
		c.rootMissing = false
		c.rebuild()
		c.changed = []string{"root-value", "status", "intro"}
		return ""

	case actionApplyPort:
		port, err := strconv.Atoi(strings.TrimSpace(value1))
		if err != nil || port < 1 || port > 65535 {
			return c.fail("alert.port.invalid.title", i18n.T("alert.port.invalid.text"))
		}
		if c.opts.SetPort == nil {
			return ""
		}
		url, err := c.opts.SetPort(port)
		if err != nil {
			return c.fail("alert.port.busy.title", err.Error())
		}
		c.port = port
		c.url = url
		c.rebuild()
		c.changed = []string{"url", "port"}
		return ""

	case actionSetBind:
		if c.opts.SetBind == nil {
			return ""
		}
		url, err := c.opts.SetBind(value1)
		if err != nil {
			// Put the popup back on the address that is still being served.
			c.changed = []string{"bind"}
			return c.fail("alert.bind.title", err.Error())
		}
		c.bind = value1
		c.url = url
		c.rebuild()
		c.changed = []string{"url", "bind"}
		return ""

	case actionApplyAuth:
		if c.opts.SetAuth == nil {
			return ""
		}
		username := strings.TrimSpace(value1)
		if err := c.opts.SetAuth(username, value2); err != nil {
			return c.fail("alert.auth.title", err.Error())
		}
		c.authUser = username
		c.authEnabled = username != ""
		c.rebuild()
		// The password is deliberately not kept in the form state.
		c.changed = []string{"auth-user", "auth-pass", "auth-status"}
		return ""

	case actionSetLanguage:
		if c.opts.SetLanguage == nil {
			return ""
		}
		options := c.opts.SetLanguage(value1)
		if options != nil {
			c.bindOptions = append([]BindOption(nil), options...)
		}
		c.language = value1
		c.rebuild()
		c.labelsChanged = true
		c.changed = []string{"language", "bind"}
		return ""

	case actionAccessInfo:
		if c.opts.AccessInfo != nil {
			c.actionText = c.opts.AccessInfo()
		}
		return ""
	}

	return ""
}

// changedIDs returns the field IDs whose widgets need a value/style refresh.
func (c *formController) changedIDs() []string {
	return c.changed
}

// humanCount shortens large numbers the way a status window would: 1204 becomes
// "1 204" in the Latin scripts and stays exact below a thousand.
func humanCount(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	digits := strconv.FormatInt(n, 10)
	var groups []string
	for len(digits) > 3 {
		groups = append([]string{digits[len(digits)-3:]}, groups...)
		digits = digits[:len(digits)-3]
	}
	groups = append([]string{digits}, groups...)
	return strings.Join(groups, " ")
}

// formatDevices renders one device per line as "addr|seen|hits". The separator
// is a pipe rather than a tab so a path or a locale string cannot break it.
func formatDevices(devices []StatDevice) string {
	if len(devices) == 0 {
		return ""
	}
	var b strings.Builder
	for i, d := range devices {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(d.Addr)
		b.WriteString(" · ")
		b.WriteString(d.Seen)
	}
	return b.String()
}

// formatHourly renders the 24 request counts as a comma-separated list, oldest
// first. A renderer draws them; it never computes them.
func formatHourly(hourly []int64) string {
	if len(hourly) == 0 {
		return ""
	}
	parts := make([]string, len(hourly))
	for i, v := range hourly {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ",")
}

// formatRecent renders the recently opened documents as base names, one per
// line: a status window has no room for a full path, and the folder is already
// shown in the settings tab.
func formatRecent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, path.Base(p))
	}
	return strings.Join(names, "\n")
}
