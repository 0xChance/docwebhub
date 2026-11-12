// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"log"
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
// A field width is -1 for "take the remaining space", 0 for natural size, or a
// positive logical-point width.
//
// Row kinds are header, url, setting, hint and footer. Consecutive setting rows
// form the settings card.

const formProductName = "docWebHub"

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
	id     string
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
				id:   "header",
				kind: "header",
				fields: []formField{
					{id: "status", kind: kindStatus, value: statusText, style: statusStyle, width: -1},
					{id: "title", kind: kindHeading, value: formProductName, style: styleDefault, width: -1},
					{id: "intro", kind: kindDescription, value: introText, style: styleMuted, width: -1},
				},
			},
			{
				id:   "url",
				kind: "url",
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
				id:   "hint",
				kind: "hint",
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
					{id: "footer-spacer", kind: kindSpacer, width: -1},
					{
						id:      "access",
						kind:    kindButton,
						label:   i18n.T("ui.access.button"),
						tooltip: i18n.T("ui.access.tooltip"),
						action:  actionAccessInfo,
						style:   styleSecondary,
						width:   120,
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
