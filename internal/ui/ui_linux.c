
// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

#include <gtk/gtk.h>
#include <libayatana-appindicator/app-indicator.h>
#include <stdlib.h>
#include <string.h>

#include "form_ui.h"

// The window is rendered from the same form description AppKit and Win32 use.
// This file only knows how to turn field kinds into GTK widgets and how to lay
// those widgets out; labels, values, options and action semantics come from Go.

typedef struct {
    int row;
    int field;
    char *id;
    char *kind;
    char *action;
    char *style;
    char *row_kind;
    GtkWidget *widget;
    GtkWidget *dot;
    gboolean updating;
} WDField;

typedef struct {
    int row;
    GtkWidget *label;
} WDRowLabel;

typedef struct {
    int index;
    GtkWidget *item;
    char *action;
} WDMenuItem;

typedef char *(*WDFieldStringFn)(int, int);
typedef char *(*WDRowStringFn)(int);
typedef char *(*WDMenuStringFn)(int);

static GtkWidget *wd_window = NULL;
static GtkWidget *wd_access_dialog = NULL;
static AppIndicator *wd_indicator = NULL;
static GtkWidget *wd_tray_menu = NULL;
static gchar *wd_icon_file = NULL;
static gchar *wd_product = NULL;
static GPtrArray *wd_fields = NULL;
static GPtrArray *wd_row_labels = NULL;
static GPtrArray *wd_menu_items = NULL;

static void wd_apply_field(WDField *field, gboolean update_value);
static void wd_apply_changes(void);
static void wd_apply_labels(void);
static void wd_perform_action(WDField *field);
/* Defined next to the tab strip; apply_labels refreshes their text. */
static GtkWidget *wd_tab_buttons[4];
static const char *wd_tab_keys[4];
static void wd_show_access_info(const char *text);
static void wd_show_page(const char *group);
static const char *wd_active_group = "overview";
static gboolean wd_on_refresh_tick(gpointer data);

static gchar *wd_take_string(char *raw) {
    gchar *value = g_strdup(raw != NULL ? raw : "");
    free(raw);
    return value;
}

static gchar *wd_field_string(int row, int field, WDFieldStringFn getter) {
    return wd_take_string(getter(row, field));
}

static gchar *wd_row_string(int row, WDRowStringFn getter) {
    return wd_take_string(getter(row));
}

static gchar *wd_menu_string(int index, WDMenuStringFn getter) {
    return wd_take_string(getter(index));
}

static gchar *wd_text(const char *key) {
    return wd_take_string(docwebhub_text(key));
}

static void wd_add_class(GtkWidget *widget, const char *name) {
    gtk_style_context_add_class(gtk_widget_get_style_context(widget), name);
}

static void wd_remove_class(GtkWidget *widget, const char *name) {
    gtk_style_context_remove_class(gtk_widget_get_style_context(widget), name);
}

static void wd_set_label_style(GtkWidget *widget, const gchar *kind, const gchar *style,
                               const gchar *row_kind) {
    wd_remove_class(widget, "title");
    wd_remove_class(widget, "muted");
    wd_remove_class(widget, "faint");
    wd_remove_class(widget, "success");
    wd_remove_class(widget, "warning");

    if (g_strcmp0(kind, "heading") == 0) {
        wd_add_class(widget, "title");
        return;
    }
    if (g_strcmp0(kind, "description") == 0) {
        wd_add_class(widget, "muted");
        return;
    }
    if (g_strcmp0(kind, "hint") == 0) {
        wd_add_class(widget, "faint");
        return;
    }
    if (g_strcmp0(kind, "status") == 0 && g_strcmp0(row_kind, "header") == 0) {
        return;
    }
    if (g_strcmp0(style, "success") == 0) {
        wd_add_class(widget, "success");
    } else if (g_strcmp0(style, "warning") == 0) {
        wd_add_class(widget, "warning");
    } else if (g_strcmp0(style, "muted") == 0) {
        wd_add_class(widget, "muted");
    } else if (g_strcmp0(style, "faint") == 0) {
        wd_add_class(widget, "faint");
    }
}

static GtkWidget *wd_label(const char *text, const char *ellipsize) {
    GtkWidget *label = gtk_label_new(text != NULL ? text : "");
    gtk_widget_set_halign(label, GTK_ALIGN_FILL);
    gtk_widget_set_hexpand(label, TRUE);
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    if (ellipsize != NULL) {
        gtk_label_set_ellipsize(GTK_LABEL(label), PANGO_ELLIPSIZE_MIDDLE);
    }
    return label;
}

/* wd_clear_records empties the widget tables. A page is destroyed and rebuilt
   whenever the tab changes, so every stored pointer becomes dangling at that
   moment; keeping them would turn the next label refresh into a use-after-free. */
static void wd_clear_records(void) {
    if (wd_fields != NULL) {
        /* Chrome rows (the address and its copy button) live outside the
           content area and survive a tab switch. Their widgets - and so their
           signal handlers - are still alive, so their records have to stay too;
           freeing them is what made the address and the copy button dead after
           the first tab change. */
        guint keep = 0;
        for (guint i = 0; i < wd_fields->len; i++) {
            WDField *field = g_ptr_array_index(wd_fields, i);
            if (g_strcmp0(field->row_kind, "url") == 0 || g_strcmp0(field->row_kind, "hint") == 0) {
                wd_fields->pdata[keep++] = field;
                continue;
            }
            g_free(field->id);
            g_free(field->kind);
            g_free(field->action);
            g_free(field->style);
            g_free(field->row_kind);
            g_free(field);
        }
        g_ptr_array_set_size(wd_fields, keep);
    }
    if (wd_row_labels != NULL) {
        for (guint i = 0; i < wd_row_labels->len; i++) {
            g_free(g_ptr_array_index(wd_row_labels, i));
        }
        g_ptr_array_set_size(wd_row_labels, 0);
    }
}

static WDField *wd_record(int row, int field, const char *row_kind) {
    WDField *record = g_new0(WDField, 1);
    record->row = row;
    record->field = field;
    record->id = wd_field_string(row, field, docwebhub_form_field_id);
    record->kind = wd_field_string(row, field, docwebhub_form_field_kind);
    record->action = wd_field_string(row, field, docwebhub_form_field_action);
    record->style = wd_field_string(row, field, docwebhub_form_field_style);
    record->row_kind = g_strdup(row_kind != NULL ? row_kind : "");
    g_ptr_array_add(wd_fields, record);
    return record;
}

static void wd_on_button_clicked(GtkButton *button, gpointer data) {
    (void)button;
    wd_perform_action((WDField *)data);
}

static void wd_on_entry_activate(GtkEntry *entry, gpointer data) {
    (void)entry;
    wd_perform_action((WDField *)data);
}

static void wd_on_combo_changed(GtkComboBox *combo, gpointer data) {
    (void)combo;
    WDField *field = (WDField *)data;
    if (!field->updating) {
        wd_perform_action(field);
    }
}

static gboolean wd_on_link_activate(GtkLinkButton *button, gpointer data) {
    (void)button;
    wd_perform_action((WDField *)data);
    // The Go action opens the browser; don't also let GtkLinkButton do it.
    return TRUE;
}

static gchar *wd_selected_value(WDField *field) {
    if (field == NULL || field->widget == NULL) {
        return g_strdup("");
    }
    int index = gtk_combo_box_get_active(GTK_COMBO_BOX(field->widget));
    int count = docwebhub_form_field_option_count(field->row, field->field);
    if (index < 0 || index >= count) {
        return g_strdup("");
    }
    char *raw = docwebhub_form_field_option_value(field->row, field->field, index);
    gchar *value = g_strdup(raw != NULL ? raw : "");
    free(raw);
    return value;
}

static WDField *wd_field_by_id(const char *field_id) {
    for (guint i = 0; i < wd_fields->len; i++) {
        WDField *field = g_ptr_array_index(wd_fields, i);
        if (g_strcmp0(field->id, field_id) == 0) {
            return field;
        }
    }
    return NULL;
}

static gboolean wd_restore_copy_label(gpointer data) {
    WDField *field = (WDField *)data;
    gchar *label = wd_field_string(field->row, field->field, docwebhub_form_field_label);
    gtk_button_set_label(GTK_BUTTON(field->widget), label);
    g_free(label);
    return G_SOURCE_REMOVE;
}

static void wd_copy_url_start_timer(WDField *copy_field) {
    WDField *link = wd_field_by_id("url");
    if (link == NULL) {
        return;
    }
    gchar *value = wd_field_string(link->row, link->field, docwebhub_form_field_value);
    if (value[0] != '\0') {
        gtk_clipboard_set_text(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD), value, -1);
        gchar *copied = wd_text("ui.url.copied");
        gtk_button_set_label(GTK_BUTTON(copy_field->widget), copied);
        g_free(copied);
        g_timeout_add(1200, wd_restore_copy_label, copy_field);
    }
    g_free(value);
}

static gchar *wd_choose_folder(void) {
    gchar *prompt = wd_text("dialog.choose.prompt");
    gchar *title = wd_text("dialog.choose.message");
    GtkWidget *dialog = gtk_file_chooser_dialog_new(
        title,
        GTK_WINDOW(wd_window),
        GTK_FILE_CHOOSER_ACTION_SELECT_FOLDER,
        "Cancel", GTK_RESPONSE_CANCEL,
        prompt, GTK_RESPONSE_ACCEPT,
        NULL);
    g_free(prompt);
    g_free(title);

    gchar *path = NULL;
    if (gtk_dialog_run(GTK_DIALOG(dialog)) == GTK_RESPONSE_ACCEPT) {
        path = gtk_file_chooser_get_filename(GTK_FILE_CHOOSER(dialog));
    }
    gtk_widget_destroy(dialog);
    return path;
}

static void wd_alert(const char *title, const char *message) {
    GtkWidget *dialog = gtk_message_dialog_new(
        GTK_WINDOW(wd_window),
        GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_WARNING,
        GTK_BUTTONS_OK,
        "%s", message != NULL ? message : "");
    gtk_window_set_title(GTK_WINDOW(dialog), title != NULL ? title : "");
    gtk_dialog_run(GTK_DIALOG(dialog));
    gtk_widget_destroy(dialog);
}

static void wd_show_access_info(const char *text) {
    if (wd_access_dialog != NULL) {
        gtk_widget_destroy(wd_access_dialog);
        wd_access_dialog = NULL;
    }

    gchar *title = wd_text("ui.access.title");
    GtkWidget *dialog = gtk_dialog_new_with_buttons(
        title, GTK_WINDOW(wd_window),
        GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        "Close", GTK_RESPONSE_CLOSE, NULL);
    g_free(title);
    gtk_window_set_default_size(GTK_WINDOW(dialog), 680, 460);
    gtk_window_set_resizable(GTK_WINDOW(dialog), TRUE);

    GtkWidget *scrolled = gtk_scrolled_window_new(NULL, NULL);
    gtk_scrolled_window_set_policy(GTK_SCROLLED_WINDOW(scrolled),
                                   GTK_POLICY_AUTOMATIC, GTK_POLICY_AUTOMATIC);
    GtkWidget *view = gtk_text_view_new();
    gtk_text_view_set_editable(GTK_TEXT_VIEW(view), FALSE);
    gtk_text_view_set_monospace(GTK_TEXT_VIEW(view), TRUE);
    gtk_text_view_set_left_margin(GTK_TEXT_VIEW(view), 12);
    gtk_text_view_set_top_margin(GTK_TEXT_VIEW(view), 10);
    GtkTextBuffer *buffer = gtk_text_view_get_buffer(GTK_TEXT_VIEW(view));
    gtk_text_buffer_set_text(buffer, text != NULL ? text : "", -1);

    gtk_container_add(GTK_CONTAINER(scrolled), view);
    gtk_box_pack_start(GTK_BOX(gtk_dialog_get_content_area(GTK_DIALOG(dialog))),
                       scrolled, TRUE, TRUE, 8);

    wd_access_dialog = dialog;
    gtk_widget_show_all(dialog);
    gtk_dialog_run(GTK_DIALOG(dialog));
    gtk_widget_destroy(dialog);
    wd_access_dialog = NULL;
}

static void wd_perform_action(WDField *field) {
    if (field == NULL || field->action == NULL || field->action[0] == '\0') {
        return;
    }

    if (g_strcmp0(field->action, "copy-url") == 0) {
        wd_copy_url_start_timer(field);
        return;
    }

    gchar *value1 = g_strdup("");
    gchar *value2 = g_strdup("");

    if (g_strcmp0(field->action, "choose-root") == 0) {
        gchar *path = wd_choose_folder();
        if (path == NULL) {
            g_free(value1);
            g_free(value2);
            return;
        }
        g_free(value1);
        value1 = path;
    } else if (g_strcmp0(field->kind, "select") == 0) {
        gchar *selected = wd_selected_value(field);
        g_free(value1);
        value1 = selected;
    } else {
        gboolean first = TRUE;
        for (guint i = 0; i < wd_fields->len; i++) {
            WDField *candidate = g_ptr_array_index(wd_fields, i);
            if (candidate->row != field->row) {
                continue;
            }
            if (g_strcmp0(candidate->kind, "input") != 0 &&
                g_strcmp0(candidate->kind, "number") != 0 &&
                g_strcmp0(candidate->kind, "password") != 0) {
                continue;
            }
            const gchar *text = gtk_entry_get_text(GTK_ENTRY(candidate->widget));
            if (first) {
                g_free(value1);
                value1 = g_strdup(text);
                first = FALSE;
            } else {
                g_free(value2);
                value2 = g_strdup(text);
            }
        }
    }

    char *message = docwebhub_action(field->action, value1, value2);
    if (message != NULL) {
        char *raw_title = docwebhub_action_error_title();
        gchar *title = g_strdup(raw_title != NULL ? raw_title : "");
        free(raw_title);
        wd_alert(title, message);
        g_free(title);
        free(message);
        wd_apply_changes();
        g_free(value1);
        g_free(value2);
        return;
    }

    /* Every successful action updates the description on the Go side; refresh
       the widgets unconditionally. The old check only repainted when the whole
       label set changed, so an action that touches one field (switching the
       bind address, say) left the window showing the previous value. */
    (void)docwebhub_labels_changed();
    wd_apply_labels();
    wd_apply_changes();

    char *text = docwebhub_action_text();
    if (text != NULL && text[0] != '\0') {
        wd_show_access_info(text);
    }
    free(text);

    g_free(value1);
    g_free(value2);
}

static GtkWidget *wd_create_widget(int row, int column, const char *row_kind, WDField **out) {
    WDField *field = wd_record(row, column, row_kind);
    gchar *kind = field->kind;
    gchar *label = wd_field_string(row, column, docwebhub_form_field_label);
    gchar *value = wd_field_string(row, column, docwebhub_form_field_value);
    gchar *placeholder = wd_field_string(row, column, docwebhub_form_field_placeholder);
    gchar *tooltip = wd_field_string(row, column, docwebhub_form_field_tooltip);
    gchar *style = field->style;
    gchar *icon = wd_field_string(row, column, docwebhub_form_field_icon);
    int width = docwebhub_form_field_width(row, column);

    GtkWidget *widget = NULL;

    if (g_strcmp0(kind, "status") == 0 && g_strcmp0(row_kind, "header") == 0) {
        GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
        GtkWidget *dot = gtk_label_new("\xe2\x97\x8f");
        wd_add_class(dot, "status-dot");
        /* The glyph sits on its own baseline; centring the two children is what
           makes the dot line up with the text instead of floating above it. */
        gtk_widget_set_valign(dot, GTK_ALIGN_CENTER);
        gtk_box_pack_start(GTK_BOX(row_box), dot, FALSE, FALSE, 0);
        GtkWidget *text = wd_label("", NULL);
        gtk_label_set_ellipsize(GTK_LABEL(text), PANGO_ELLIPSIZE_END);
        gtk_box_pack_start(GTK_BOX(row_box), text, TRUE, TRUE, 0);
        field->widget = text;
        field->dot = dot;
        widget = row_box;
    } else if (g_strcmp0(kind, "heading") == 0) {
        widget = wd_label("", NULL);
    } else if (g_strcmp0(kind, "description") == 0) {
        widget = wd_label("", NULL);
        gtk_label_set_ellipsize(GTK_LABEL(widget), PANGO_ELLIPSIZE_END);
    } else if (g_strcmp0(kind, "link") == 0) {
        widget = gtk_link_button_new_with_label(value, value);
        gtk_widget_set_hexpand(widget, TRUE);
        /* The button fills the row so the copy button stays right; its inner
           label still needs to sit at the left edge. gtk_button_set_alignment
           is deprecated and ignored by GtkLinkButton, so reach for the label. */
        GtkWidget *inner = gtk_bin_get_child(GTK_BIN(widget));
        if (inner != NULL && GTK_IS_LABEL(inner)) {
            gtk_label_set_xalign(GTK_LABEL(inner), 0.0);
        }
        g_signal_connect(widget, "activate-link", G_CALLBACK(wd_on_link_activate), field);
    } else if (g_strcmp0(kind, "value") == 0) {
        widget = wd_label("", "middle");
        gtk_label_set_ellipsize(GTK_LABEL(widget), PANGO_ELLIPSIZE_MIDDLE);
    } else if (g_strcmp0(kind, "device-list") == 0 || g_strcmp0(kind, "file-list") == 0 ||
               g_strcmp0(kind, "stat") == 0 || g_strcmp0(kind, "badge") == 0) {
        widget = wd_label("", NULL);
        gtk_label_set_ellipsize(GTK_LABEL(widget), PANGO_ELLIPSIZE_END);
    } else if (g_strcmp0(kind, "sparkline") == 0) {
        widget = gtk_drawing_area_new();
        gtk_widget_set_size_request(widget, -1, 44);
    } else if (g_strcmp0(kind, "hint") == 0) {
        widget = wd_label("", NULL);
        gtk_label_set_ellipsize(GTK_LABEL(widget), PANGO_ELLIPSIZE_END);
    } else if (g_strcmp0(kind, "input") == 0 || g_strcmp0(kind, "number") == 0 ||
               g_strcmp0(kind, "password") == 0) {
        widget = gtk_entry_new();
        gtk_entry_set_text(GTK_ENTRY(widget), value);
        gtk_entry_set_placeholder_text(GTK_ENTRY(widget), placeholder);
        if (g_strcmp0(kind, "password") == 0) {
            gtk_entry_set_visibility(GTK_ENTRY(widget), FALSE);
            gtk_entry_set_input_purpose(GTK_ENTRY(widget), GTK_INPUT_PURPOSE_PASSWORD);
        }
        if (g_strcmp0(kind, "number") == 0) {
            gtk_entry_set_max_length(GTK_ENTRY(widget), 5);
            gtk_entry_set_input_purpose(GTK_ENTRY(widget), GTK_INPUT_PURPOSE_NUMBER);
        }
        g_signal_connect(widget, "activate", G_CALLBACK(wd_on_entry_activate), field);
    } else if (g_strcmp0(kind, "button") == 0) {
        widget = gtk_button_new_with_label("");
        if (g_strcmp0(style, "primary") == 0) {
            wd_add_class(widget, "primary");
        } else {
            wd_add_class(widget, "secondary");
        }
        if (g_strcmp0(icon, "copy") == 0) {
            gtk_button_set_image(GTK_BUTTON(widget),
                                 gtk_image_new_from_icon_name("edit-copy-symbolic", GTK_ICON_SIZE_BUTTON));
            gtk_button_set_always_show_image(GTK_BUTTON(widget), TRUE);
        } else if (g_strcmp0(icon, "folder") == 0) {
            gtk_button_set_image(GTK_BUTTON(widget),
                                 gtk_image_new_from_icon_name("folder-symbolic", GTK_ICON_SIZE_BUTTON));
            gtk_button_set_always_show_image(GTK_BUTTON(widget), TRUE);
        }
        g_signal_connect(widget, "clicked", G_CALLBACK(wd_on_button_clicked), field);
    } else if (g_strcmp0(kind, "select") == 0) {
        widget = gtk_combo_box_text_new();
        g_signal_connect(widget, "changed", G_CALLBACK(wd_on_combo_changed), field);
    } else {
        widget = wd_label("", NULL);
    }

    field->widget = (g_strcmp0(kind, "status") == 0 && g_strcmp0(row_kind, "header") == 0)
                        ? field->widget
                        : widget;

    if (width > 0) {
        gtk_widget_set_size_request(widget, width, -1);
        gtk_widget_set_hexpand(widget, FALSE);
    } else if (width < 0) {
        gtk_widget_set_hexpand(widget, TRUE);
    }
    if (tooltip[0] != '\0') {
        gtk_widget_set_tooltip_text(widget, tooltip);
    }

    wd_apply_field(field, TRUE);

    g_free(label);
    g_free(value);
    g_free(placeholder);
    g_free(tooltip);
    g_free(icon);
    if (out != NULL) {
        *out = field;
    }
    return widget;
}

static void wd_apply_field(WDField *field, gboolean update_value) {
    if (field == NULL || field->widget == NULL) {
        return;
    }
    gchar *kind = field->kind;
    gchar *style = wd_field_string(field->row, field->field, docwebhub_form_field_style);
    gchar *label = wd_field_string(field->row, field->field, docwebhub_form_field_label);
    gchar *value = wd_field_string(field->row, field->field, docwebhub_form_field_value);
    gchar *placeholder = wd_field_string(field->row, field->field, docwebhub_form_field_placeholder);
    gchar *tooltip = wd_field_string(field->row, field->field, docwebhub_form_field_tooltip);

    if (g_strcmp0(kind, "button") == 0) {
        gtk_button_set_label(GTK_BUTTON(field->widget), label);
    } else if (g_strcmp0(kind, "link") == 0) {
        gtk_link_button_set_uri(GTK_LINK_BUTTON(field->widget), value);
        gtk_button_set_label(GTK_BUTTON(field->widget), value);
    } else if (g_strcmp0(kind, "input") == 0 || g_strcmp0(kind, "number") == 0 ||
               g_strcmp0(kind, "password") == 0) {
        if (update_value) {
            gtk_entry_set_text(GTK_ENTRY(field->widget), value);
        }
        gtk_entry_set_placeholder_text(GTK_ENTRY(field->widget), placeholder);
    } else if (g_strcmp0(kind, "select") == 0) {
        field->updating = TRUE;
        gtk_combo_box_text_remove_all(GTK_COMBO_BOX_TEXT(field->widget));
        int count = docwebhub_form_field_option_count(field->row, field->field);
        int active = 0;
        for (int i = 0; i < count; i++) {
            char *raw_label = docwebhub_form_field_option_label(field->row, field->field, i);
            char *raw_value = docwebhub_form_field_option_value(field->row, field->field, i);
            gtk_combo_box_text_append_text(GTK_COMBO_BOX_TEXT(field->widget),
                                           raw_label != NULL ? raw_label : "");
            if (g_strcmp0(raw_value, value) == 0) {
                active = i;
            }
            free(raw_label);
            free(raw_value);
        }
        if (count > 0) {
            gtk_combo_box_set_active(GTK_COMBO_BOX(field->widget), active);
        }
        field->updating = FALSE;
    } else if (g_strcmp0(kind, "status") == 0) {
        gtk_label_set_text(GTK_LABEL(field->widget), value);
        wd_set_label_style(field->widget, kind, style, field->row_kind);
        if (field->dot != NULL) {
            if (g_strcmp0(style, "warning") == 0) {
                wd_add_class(field->dot, "warn");
            } else {
                wd_remove_class(field->dot, "warn");
            }
        }
    } else if (g_strcmp0(kind, "hint") == 0) {
        gtk_label_set_text(GTK_LABEL(field->widget), label);
        wd_set_label_style(field->widget, kind, style, field->row_kind);
    } else if (g_strcmp0(kind, "sparkline") == 0) {
        /* A drawing area, not a label: the refresh timer redraws it. */
        gtk_widget_queue_draw(field->widget);
    } else if (g_strcmp0(kind, "device-list") == 0 || g_strcmp0(kind, "file-list") == 0) {
        if (value[0] == '\0') {
            gchar *empty_text = wd_text("ui.overview.empty");
            gtk_label_set_text(GTK_LABEL(field->widget), empty_text);
            g_free(empty_text);
            wd_set_label_style(field->widget, "faint", "faint", field->row_kind);
        } else {
            gtk_label_set_text(GTK_LABEL(field->widget), value);
            wd_set_label_style(field->widget, kind, style, field->row_kind);
        }
    } else {
        gtk_label_set_text(GTK_LABEL(field->widget), value);
        wd_set_label_style(field->widget, kind, style, field->row_kind);
    }

    if (tooltip[0] != '\0') {
        gtk_widget_set_tooltip_text(field->widget, tooltip);
    } else {
        gtk_widget_set_tooltip_text(field->widget, NULL);
    }

    g_free(style);
    g_free(label);
    g_free(value);
    g_free(placeholder);
    g_free(tooltip);
}

static void wd_apply_changes(void) {
    int count = docwebhub_changed_count();
    for (int i = 0; i < count; i++) {
        char *raw = docwebhub_changed_id(i);
        gchar *id = wd_take_string(raw);
        WDField *field = wd_field_by_id(id);
        if (field != NULL) {
            wd_apply_field(field, TRUE);
        }
        g_free(id);
    }
}

static void wd_apply_labels(void) {
    if (wd_window != NULL) {
        gchar *title = wd_take_string(docwebhub_form_title());
        gtk_window_set_title(GTK_WINDOW(wd_window), title);
        g_free(title);
    }
    for (guint i = 0; i < wd_fields->len; i++) {
        wd_apply_field(g_ptr_array_index(wd_fields, i), FALSE);
    }
    for (guint i = 0; i < wd_row_labels->len; i++) {
        WDRowLabel *row_label = g_ptr_array_index(wd_row_labels, i);
        gchar *text = wd_row_string(row_label->row, docwebhub_form_row_label);
        gtk_label_set_text(GTK_LABEL(row_label->label), text);
        g_free(text);
    }
    if (wd_access_dialog != NULL) {
        gchar *title = wd_text("ui.access.title");
        gtk_window_set_title(GTK_WINDOW(wd_access_dialog), title);
        g_free(title);
    }
    for (int i = 0; i < 4; i++) {
        if (wd_tab_buttons[i] == NULL) {
            continue;
        }
        gchar *label = wd_text(wd_tab_keys[i]);
        gtk_button_set_label(GTK_BUTTON(wd_tab_buttons[i]), label);
        g_free(label);
    }
    // The tray menu keeps its structure; only the labels change with the language.
    for (guint i = 0; i < wd_menu_items->len; i++) {
        WDMenuItem *item = g_ptr_array_index(wd_menu_items, i);
        if (item->item != NULL) {
            gchar *label = wd_menu_string(item->index, docwebhub_form_menu_label);
            gtk_menu_item_set_label(GTK_MENU_ITEM(item->item), label);
            g_free(label);
        }
    }
}

static void wd_add_row_label(GtkWidget *row_box, int row, const gchar *text) {
    GtkWidget *label = gtk_label_new(text != NULL ? text : "");
    gtk_widget_set_size_request(label, 76, -1);
    gtk_widget_set_halign(label, GTK_ALIGN_START);
    gtk_widget_set_valign(label, GTK_ALIGN_START);
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    gtk_label_set_ellipsize(GTK_LABEL(label), PANGO_ELLIPSIZE_END);
    wd_add_class(label, "row-title");
    gtk_box_pack_start(GTK_BOX(row_box), label, FALSE, FALSE, 0);
    WDRowLabel *record = g_new0(WDRowLabel, 1);
    record->row = row;
    record->label = label;
    g_ptr_array_add(wd_row_labels, record);
}

static GtkWidget *wd_fields_box(void) {
    GtkWidget *box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
    gtk_widget_set_hexpand(box, TRUE);
    return box;
}

static void wd_pack_field(GtkWidget *box, GtkWidget *widget, int width) {
    if (widget == NULL) {
        return;
    }
    gboolean expand = width < 0;
    gtk_box_pack_start(GTK_BOX(box), widget, expand, TRUE, 0);
}

static void wd_add_choice_picker(GtkWidget *box, int row, int column);

static void wd_add_setting_row(GtkWidget *card, int row) {
    /* A select becomes a block of its own: label on one line, choices below.
       The list needs the full width, and the row shape has to change with it. */
    int count = docwebhub_form_field_count(row);
    gboolean has_select = FALSE;
    for (int column = 0; column < count; column++) {
        gchar *kind = wd_field_string(row, column, docwebhub_form_field_kind);
        if (g_strcmp0(kind, "select") == 0) {
            has_select = TRUE;
        }
        g_free(kind);
    }
    if (has_select) {
        /* Label and collapsed summary stay on one line like every other row;
           only the expanded list needs the full width, so it goes underneath. */
        GtkWidget *block = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
        GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
        gtk_widget_set_valign(row_box, GTK_ALIGN_START);
        gchar *label = wd_row_string(row, docwebhub_form_row_label);
        wd_add_row_label(row_box, row, label);
        g_free(label);
        GtkWidget *picker = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
        gtk_widget_set_hexpand(picker, TRUE);
        gtk_box_pack_start(GTK_BOX(row_box), picker, TRUE, TRUE, 0);
        gtk_box_pack_start(GTK_BOX(block), row_box, FALSE, FALSE, 0);

        for (int column = 0; column < count; column++) {
            gchar *kind = wd_field_string(row, column, docwebhub_form_field_kind);
            if (g_strcmp0(kind, "select") == 0) {
                wd_add_choice_picker(picker, row, column);
            }
            g_free(kind);
        }
        gtk_box_pack_start(GTK_BOX(card), block, FALSE, FALSE, 0);
        return;
    }

    GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
    gchar *label = wd_row_string(row, docwebhub_form_row_label);
    wd_add_row_label(row_box, row, label);
    g_free(label);

    GtkWidget *fields = wd_fields_box();
    for (int column = 0; column < count; column++) {
        gchar *kind = wd_field_string(row, column, docwebhub_form_field_kind);
        int width = docwebhub_form_field_width(row, column);
        if (g_strcmp0(kind, "spacer") == 0) {
            GtkWidget *spacer = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 0);
            gtk_widget_set_hexpand(spacer, TRUE);
            gtk_box_pack_start(GTK_BOX(fields), spacer, TRUE, TRUE, 0);
        } else {
            GtkWidget *widget = wd_create_widget(row, column, "setting", NULL);
            wd_pack_field(fields, widget, width);
        }
        g_free(kind);
    }

    gtk_box_pack_start(GTK_BOX(row_box), fields, TRUE, TRUE, 0);
    gtk_box_pack_start(GTK_BOX(card), row_box, FALSE, FALSE, 0);
}

static void wd_add_url_row(GtkWidget *box, int row) {
    GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 10);
    gtk_widget_set_halign(row_box, GTK_ALIGN_FILL);
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        int width = docwebhub_form_field_width(row, column);
        GtkWidget *widget = wd_create_widget(row, column, "url", NULL);
        gtk_widget_set_halign(widget, column == 0 ? GTK_ALIGN_START : GTK_ALIGN_END);
        wd_pack_field(row_box, widget, width);
    }
    /* No extra start margin: the copy button needs the room on the right, but
       the address has to line up with the note underneath it. */
    gtk_box_pack_start(GTK_BOX(box), row_box, FALSE, FALSE, 0);
}

static void wd_add_hint_row(GtkWidget *box, int row) {
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        GtkWidget *widget = wd_create_widget(row, column, "hint", NULL);
        gtk_box_pack_start(GTK_BOX(box), widget, FALSE, TRUE, 0);
    }
}

static gboolean wd_rebuild_page_idle(gpointer data) {
    (void)data;
    wd_show_page(wd_active_group);
    return G_SOURCE_REMOVE;
}

static void wd_on_choice_item(GtkButton *item, gpointer data) {
    (void)data;
    const char *action = (const char *)g_object_get_data(G_OBJECT(item), "action");
    const char *value = (const char *)g_object_get_data(G_OBJECT(item), "value");
    GtkWidget *summary = (GtkWidget *)g_object_get_data(G_OBJECT(item), "summary");
    GtkWidget *revealer = (GtkWidget *)g_object_get_data(G_OBJECT(item), "revealer");
    GtkWidget *grid = (GtkWidget *)g_object_get_data(G_OBJECT(item), "grid");
    if (action == NULL || value == NULL || summary == NULL) {
        return;
    }

    gtk_button_set_label(GTK_BUTTON(summary), gtk_button_get_label(item));
    if (revealer != NULL) {
        gtk_revealer_set_reveal_child(GTK_REVEALER(revealer), FALSE);
    }
    gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(summary), FALSE);

    /* Move the highlight without rebuilding the row. */
    if (grid != NULL) {
        GList *items = gtk_container_get_children(GTK_CONTAINER(grid));
        for (GList *node = items; node != NULL; node = node->next) {
            GtkWidget *child = GTK_WIDGET(node->data);
            if (child == GTK_WIDGET(item)) {
                wd_add_class(child, "choice-active");
            } else {
                wd_remove_class(child, "choice-active");
            }
        }
        g_list_free(items);
    }

    gchar *message = docwebhub_action((gchar *)action, (gchar *)value, NULL);
    if (message != NULL) {
        free(message);
    }
    wd_apply_labels();
    wd_apply_changes();
    if (g_strcmp0(action, "set-language") == 0) {
        /* Every label changed language, including the collapsed summary this
           click came from, so rebuild the page from the new description.
           Deferred: destroying the widget that emitted the signal from inside
           its own handler is asking for trouble. */
        g_idle_add(wd_rebuild_page_idle, NULL);
    }
}

static void wd_on_choice_toggle(GtkToggleButton *toggle, gpointer data) {
    gtk_revealer_set_reveal_child(GTK_REVEALER(data),
                                  gtk_toggle_button_get_active(toggle));
}

static void wd_add_choice_picker(GtkWidget *box, int row, int column) {
    gchar *current = wd_field_string(row, column, docwebhub_form_field_value);
    gchar *action = wd_field_string(row, column, docwebhub_form_field_action);
    gchar *current_label = NULL;
    int count = docwebhub_form_field_option_count(row, column);

    GtkWidget *grid = gtk_grid_new();
    gtk_grid_set_column_spacing(GTK_GRID(grid), 6);
    gtk_grid_set_row_spacing(GTK_GRID(grid), 2);
    gtk_grid_set_column_homogeneous(GTK_GRID(grid), TRUE);

    for (int i = 0; i < count; i++) {
        gchar *label = wd_take_string(docwebhub_form_field_option_label(row, column, i));
        gchar *value = wd_take_string(docwebhub_form_field_option_value(row, column, i));

        if (g_strcmp0(value, current) == 0) {
            /* The summary shows whatever the manifest currently selects. */
            g_free(current_label);
            current_label = g_strdup(label);
        }

        GtkWidget *item = gtk_button_new_with_label(label);
        wd_add_class(item, "choice-item");
        if (g_strcmp0(value, current) == 0) {
            wd_add_class(item, "choice-active");
        }
        g_object_set_data_full(G_OBJECT(item), "action", g_strdup(action), g_free);
        g_object_set_data_full(G_OBJECT(item), "value", g_strdup(value), g_free);
        gtk_widget_set_halign(item, GTK_ALIGN_FILL);
        gtk_grid_attach(GTK_GRID(grid), item, i % 2, i / 2, 1, 1);

        g_free(label);
        g_free(value);
    }

    if (current_label == NULL) {
        current_label = g_strdup(current);
    }

    GtkWidget *revealer = gtk_revealer_new();
    gtk_revealer_set_transition_type(GTK_REVEALER(revealer),
                                     GTK_REVEALER_TRANSITION_TYPE_NONE);
    gtk_container_add(GTK_CONTAINER(revealer), grid);

    gchar *summary_text = g_strdup_printf("%s  \xe2\x96\xbe", current_label);
    GtkWidget *summary = gtk_toggle_button_new_with_label(summary_text);
    wd_add_class(summary, "choice-summary");
    gtk_widget_set_halign(summary, GTK_ALIGN_FILL);
    gtk_widget_set_hexpand(summary, TRUE);
    GtkWidget *summary_label = gtk_bin_get_child(GTK_BIN(summary));
    if (summary_label != NULL && GTK_IS_LABEL(summary_label)) {
        gtk_label_set_xalign(GTK_LABEL(summary_label), 0.0);
    }
    g_signal_connect(summary, "toggled", G_CALLBACK(wd_on_choice_toggle), revealer);

    /* Back-pointers so a click on a choice can update the summary and the row. */
    GList *items = gtk_container_get_children(GTK_CONTAINER(grid));
    for (GList *node = items; node != NULL; node = node->next) {
        GtkWidget *item = GTK_WIDGET(node->data);
        g_object_set_data(G_OBJECT(item), "summary", summary);
        g_object_set_data(G_OBJECT(item), "revealer", revealer);
        g_object_set_data(G_OBJECT(item), "grid", grid);
        g_signal_connect(item, "clicked", G_CALLBACK(wd_on_choice_item), NULL);
    }
    g_list_free(items);

    gtk_box_pack_start(GTK_BOX(box), summary, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(box), revealer, FALSE, FALSE, 0);

    g_free(summary_text);
    g_free(current_label);
    g_free(current);
    g_free(action);
}

static void wd_add_footer_row(GtkWidget *box, int row) {
    GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
    gtk_widget_set_valign(row_box, GTK_ALIGN_START);
    gchar *label = wd_row_string(row, docwebhub_form_row_label);
    wd_add_row_label(row_box, row, label);
    g_free(label);

    GtkWidget *fields = wd_fields_box();
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        gchar *kind = wd_field_string(row, column, docwebhub_form_field_kind);
        int width = docwebhub_form_field_width(row, column);
        if (g_strcmp0(kind, "spacer") == 0) {
            GtkWidget *spacer = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 0);
            gtk_widget_set_hexpand(spacer, TRUE);
            gtk_box_pack_start(GTK_BOX(fields), spacer, TRUE, TRUE, 0);
        } else {
            gchar *action = wd_field_string(row, column, docwebhub_form_field_action);
            if (g_strcmp0(action, "set-language") == 0) {
                /* Reuse the setting-row shape: the same collapsed picker in the
                   same box, so the layout maths that works for the bind row
                   applies here too. */
                GtkWidget *card = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
                wd_add_class(card, "card");
                gtk_box_pack_start(GTK_BOX(box), card, FALSE, FALSE, 0);
                wd_add_setting_row(card, row);
                g_free(action);
                g_free(kind);
                return;
            } else {
                GtkWidget *widget = wd_create_widget(row, column, "footer", NULL);
                wd_pack_field(fields, widget, width);
            }
            g_free(action);
        }
        g_free(kind);
    }
    gtk_box_pack_start(GTK_BOX(row_box), fields, TRUE, TRUE, 0);
    gtk_box_pack_start(GTK_BOX(box), row_box, FALSE, FALSE, 0);
}

static void wd_on_menu_open(GtkMenuItem *item, gpointer data) {
    (void)item;
    (void)data;
    char *message = docwebhub_action("open-url", "", "");
    if (message != NULL) {
        char *raw_title = docwebhub_action_error_title();
        gchar *title = g_strdup(raw_title != NULL ? raw_title : "");
        free(raw_title);
        wd_alert(title, message);
        g_free(title);
        free(message);
    }
    if (docwebhub_labels_changed()) {
        wd_apply_labels();
    }
    wd_apply_changes();
}

static void wd_on_menu_show(GtkMenuItem *item, gpointer data) {
    (void)item;
    (void)data;
    if (wd_window != NULL) {
        gtk_widget_show_all(wd_window);
        gtk_window_present(GTK_WINDOW(wd_window));
    }
}

static void wd_on_menu_quit(GtkMenuItem *item, gpointer data) {
    (void)item;
    (void)data;
    gtk_main_quit();
}

static void wd_on_menu_activate(GtkMenuItem *item, gpointer data) {
    const gchar *action = (const gchar *)data;
    if (g_strcmp0(action, "show-window") == 0) {
        wd_on_menu_show(item, NULL);
    } else if (g_strcmp0(action, "quit") == 0) {
        wd_on_menu_quit(item, NULL);
    } else if (g_strcmp0(action, "open-url") == 0) {
        wd_on_menu_open(item, NULL);
    }
}

static void wd_build_menu(void) {
    wd_tray_menu = gtk_menu_new();
    wd_menu_items = g_ptr_array_new();
    int count = docwebhub_form_menu_count();
    for (int i = 0; i < count; i++) {
        if (docwebhub_form_menu_separator(i)) {
            GtkWidget *separator = gtk_separator_menu_item_new();
            gtk_menu_shell_append(GTK_MENU_SHELL(wd_tray_menu), separator);
            WDMenuItem *record = g_new0(WDMenuItem, 1);
            record->index = i;
            record->item = NULL;
            g_ptr_array_add(wd_menu_items, record);
            continue;
        }
        gchar *label = wd_menu_string(i, docwebhub_form_menu_label);
        gchar *action = wd_menu_string(i, docwebhub_form_menu_action);
        GtkWidget *item = gtk_menu_item_new_with_label(label);
        g_signal_connect(item, "activate", G_CALLBACK(wd_on_menu_activate), action);
        gtk_menu_shell_append(GTK_MENU_SHELL(wd_tray_menu), item);
        WDMenuItem *record = g_new0(WDMenuItem, 1);
        record->index = i;
        record->item = item;
        record->action = action;
        g_ptr_array_add(wd_menu_items, record);
        g_free(label);
    }
    gtk_widget_show_all(wd_tray_menu);
}

static void wd_show_window(void) {
    if (wd_window != NULL) {
        gtk_widget_show_all(wd_window);
        gtk_window_present(GTK_WINDOW(wd_window));
    }
}

static void wd_on_indicator_activate(AppIndicator *indicator, gint x, gint y, gpointer data) {
    (void)indicator;
    (void)x;
    (void)y;
    (void)data;
    wd_show_window();
}

static void wd_setup_tray(void) {
    const char *icon_name = "web-browser";
    const char *theme_path = "";
    gchar *owned_dir = NULL;
    if (wd_icon_file != NULL && wd_icon_file[0] != '\0') {
        icon_name = wd_icon_file;
        owned_dir = g_path_get_dirname(wd_icon_file);
        theme_path = owned_dir;
    }

    wd_indicator = g_object_new(APP_INDICATOR_TYPE,
                                "id", "docwebhub",
                                "category", "ApplicationStatus",
                                "icon-name", icon_name,
                                "icon-desc", wd_product,
                                "icon-theme-path", theme_path,
                                "title", wd_product,
                                "status", "Active",
                                NULL);
    g_free(owned_dir);

    app_indicator_set_menu(wd_indicator, GTK_MENU(wd_tray_menu));
    g_signal_connect(wd_indicator, APP_INDICATOR_SIGNAL_ACTIVATE_EVENT,
                     G_CALLBACK(wd_on_indicator_activate), NULL);
}

void docwebhub_set_tray_icon(const char *path) {
    g_free(wd_icon_file);
    wd_icon_file = g_strdup(path != NULL ? path : "");
}

static gboolean wd_on_delete(GtkWidget *widget, GdkEvent *event, gpointer data) {
    (void)event;
    (void)data;
    gtk_widget_hide(widget);
    return TRUE;
}

/* Design tokens, light theme first. GTK 3 has no CSS variables, so the values
   are written out; the two theme blocks below are the only place to change
   them. Sizes follow the 4/8/12/16/24/32 grid used on the website. */
static const char *wd_css =
    "window { background-color: #f6f8fa; }"

    /* Tab strip */
    ".tabs { background-color: transparent; padding: 8px 12px; }"
    ".tab { background-image: none; background-color: transparent; border: none;"
    " border-radius: 999px; padding: 6px 14px; box-shadow: none;"
    " color: #4a5768; font-size: 12px; font-weight: 500; }"
    ".tab:hover { background-color: rgba(16,22,40,0.05); color: #16202e; }"
    ".tab.tab-active { background-color: #e2500e; }"
    /* The fill sits on the button; the text lives in its inner label, which the
       global label colour would otherwise win over. */
    ".tab.tab-active, .tab.tab-active label { color: #ffffff; text-shadow: none; }"
    /* GTK's own focus handling paints an inner highlight on top of the label,
       which is what makes text look doubled while the window has focus. Drop
       the theme's decoration and keep a single outer ring instead. */
    "button { text-shadow: none; -gtk-icon-shadow: none; outline: none; }"
    "button:focus, button:focus-visible { outline: 2px solid rgba(226,80,14,0.55);"
    " outline-offset: 2px; box-shadow: none; }"
    /* Focus must not repaint the fill: the theme otherwise draws its own
       highlight behind the label, which is what reads as doubled text. */
    ".tab:focus, .tab:focus-visible, .lang-item:focus {"
    " background-color: transparent; background-image: none; box-shadow: none;"
    " text-shadow: none; -gtk-icon-shadow: none; }"
    ".tab.tab-active:focus, .tab.tab-active:focus-visible,"
    " .tab.tab-active:focus label {"
    " background-color: #e2500e; color: #ffffff; }"
    ".lang-item.lang-active:focus { background-color: rgba(226,80,14,0.10);"
    " color: #e2500e; }"

    /* Cards */
    ".card { background-color: #ffffff; border: 1px solid rgba(16,22,40,0.10);"
    " border-radius: 14px; padding: 4px 16px; }"
    ".page { padding: 14px 22px 10px; }"
    ".footer { background-color: rgba(16,22,40,0.035);"
    " border-top: 1px solid rgba(16,22,40,0.10); padding: 12px 22px; }"

    /* Typography. A base colour on every label: without it the desktop theme
       supplies its own, and a dark theme paints near-white text on this light
       window. */
    "label { color: #16202e; }"
    /* A link button carries the theme's button padding, which pushed the
       address in from the footer edge and out of line with the note below. */
    "button.link { padding: 0; border: none; background: none; box-shadow: none; }"
    "button.link, button.link label { color: #e2500e; }"
    ".headline { font-size: 22px; font-weight: 600; color: #16202e; letter-spacing: -0.015em; }"
    ".lead { font-size: 13px; color: #4a5768; }"
    ".row-title { color: #7b8798; font-size: 12px; }"
    ".muted { color: #4a5768; }"
    ".faint { color: #7b8798; font-size: 12px; }"
    ".success { color: #1f9d55; }"
    ".warning { color: #b8760a; }"

    /* Overview numbers */
    ".stat-value { font-size: 38px; font-weight: 300; color: #16202e; letter-spacing: -0.02em; }"
    ".stat-label { font-size: 11px; color: #7b8798; }"
    ".badge { background-color: rgba(16,22,40,0.05); border-radius: 999px;"
    " padding: 4px 12px; color: #4a5768; font-size: 11px; }"
    ".badge-value { font-weight: 600; color: #16202e; font-size: 12px; }"
    ".device-addr { font-size: 13px; color: #16202e; }"
    ".device-seen { font-size: 12px; color: #7b8798; }"
    ".file-name { font-size: 13px; color: #4a5768; }"

    /* Buttons */
    "button.primary { background-image: none; background-color: #e2500e; color: #ffffff;"
    " border: 1px solid #e2500e; border-radius: 8px; padding: 5px 14px;"
    " text-shadow: none; box-shadow: none; }"
    "button.primary:hover { background-color: #c94409; }"
    "button.secondary { background-image: none; background-color: #ffffff;"
    " border: 1px solid rgba(16,22,40,0.14); border-radius: 8px; padding: 5px 12px;"
    " box-shadow: none; }"
    "button.secondary:hover { background-color: #f1f4f7; }"

    /* Entries and selects. Every property is set explicitly: inheriting from
       the desktop theme is how a light window ends up with black input boxes. */
    "entry { background-image: none; background-color: #ffffff; color: #16202e;"
    " border: 1px solid rgba(16,22,40,0.14); border-radius: 8px;"
    " padding: 4px 8px; box-shadow: none; caret-color: #16202e; }"
    "entry:focus { border-color: #e2500e; box-shadow: 0 0 0 3px rgba(226,80,14,0.16); }"
    "combobox button { background-image: none; background-color: #ffffff; color: #16202e;"
    " border: 1px solid rgba(16,22,40,0.14); border-radius: 8px;"
    " padding: 4px 10px; box-shadow: none; text-shadow: none; }"
    "combobox button:hover { background-color: #f1f4f7; }"
    "combobox button:focus { border-color: #e2500e; }"
    "combobox window, combobox menu, menu, popover {"
    " background-color: #ffffff; color: #16202e; }"
    "menu menuitem:hover, popover list row:hover { background-color: #f1f4f7; }"
    "selection { background-color: rgba(226,80,14,0.20); }"

    /* The collapsed row has to look like the select it replaced. */
    ".choice-summary { background-image: none; background-color: #ffffff;"
    " border: 1px solid rgba(16,22,40,0.14); border-radius: 8px;"
    " padding: 6px 10px; box-shadow: none; color: #16202e; font-size: 13px; }"
    ".choice-summary:hover { background-color: #f1f4f7; }"
    ".choice-summary:checked { border-color: #e2500e; }"
    ".choice-item { background-image: none; background-color: transparent;"
    " border: 1px solid transparent; border-radius: 8px; padding: 6px 12px;"
    " box-shadow: none; color: #4a5768; font-size: 13px; }"
    ".choice-item:hover { background-color: rgba(16,22,40,0.05); color: #16202e; }"
    ".choice-item.choice-active { background-color: rgba(226,80,14,0.10);"
    " border-color: transparent; }"
    ".choice-item.choice-active, .choice-item.choice-active label {"
    " color: #e2500e; font-weight: 600; }"
    ".lang-item { background-image: none; background-color: transparent;"
    " border: 1px solid transparent; border-radius: 8px; padding: 6px 12px;"
    " box-shadow: none; color: #4a5768; font-size: 13px; }"
    ".lang-item:hover { background-color: rgba(16,22,40,0.05); color: #16202e; }"
    ".lang-item.lang-active { background-color: rgba(226,80,14,0.10);"
    " border-color: transparent; }"
    ".lang-item.lang-active, .lang-item.lang-active label {"
    " color: #e2500e; font-weight: 600; }"
    ".status-dot { color: #1f9d55; font-size: 11px;"
    " text-shadow: 0 0 5px rgba(31,157,85,0.5); }"
    ".status-dot.warn { color: #b8760a; text-shadow: 0 0 6px rgba(184,118,10,0.55); }"
    ".title { font-size: 22px; font-weight: 600; color: #16202e; }";

/* Dark theme: same structure, different values. Applied by wd_install_css when
   the desktop reports a dark preference, and refreshed when it changes. */
static const char *wd_css_dark =
    "window { background-color: #161b22; }"
    "label { color: #e6edf5; }"
    "button.link { padding: 0; border: none; background: none; box-shadow: none; }"
    "button.link, button.link label { color: #ff7a3d; }"
    ".footer { background-color: rgba(240,244,250,0.04);"
    " border-top: 1px solid rgba(240,244,250,0.10); }"
    ".choice-summary { background-color: #242c38; color: #e6edf5;"
    " border-color: rgba(240,244,250,0.14); }"
    ".choice-summary:hover { background-color: #2c3644; }"
    ".choice-item { color: #a3b0c2; }"
    ".choice-item:hover { background-color: rgba(240,244,250,0.08); color: #e6edf5; }"
    ".choice-item.choice-active { background-color: rgba(255,122,61,0.18); }"
    ".choice-item.choice-active, .choice-item.choice-active label { color: #ff7a3d; }"
    ".lang-item { color: #a3b0c2; }"
    ".lang-item:hover { background-color: rgba(240,244,250,0.08); color: #e6edf5; }"
    ".lang-item.lang-active { background-color: rgba(255,122,61,0.18); }"
    ".lang-item.lang-active, .lang-item.lang-active label { color: #ff7a3d; }"
    ".tab { color: #a3b0c2; }"
    ".tab:hover { background-color: rgba(240,244,250,0.08); color: #e6edf5; }"
    ".tab.tab-active { background-color: #ff7a3d; }"
    ".tab.tab-active, .tab.tab-active label { color: #1a1008; }"
    "button:focus, button:focus-visible { outline: 2px solid rgba(255,122,61,0.6);"
    " outline-offset: 2px; box-shadow: none; }"
    ".tab:focus, .tab:focus-visible, .lang-item:focus {"
    " background-color: transparent; background-image: none; }"
    ".tab.tab-active:focus, .tab.tab-active:focus-visible,"
    " .tab.tab-active:focus label {"
    " background-color: #ff7a3d; color: #1a1008; }"
    ".card { background-color: #1d242e; border-color: rgba(240,244,250,0.10); }"
    ".headline { color: #e6edf5; }"
    ".lead { color: #a3b0c2; }"
    ".row-title { color: #6e7c8f; }"
    ".muted { color: #a3b0c2; }"
    ".faint { color: #6e7c8f; }"
    ".stat-value { color: #e6edf5; }"
    ".stat-label { color: #6e7c8f; }"
    ".badge { background-color: rgba(240,244,250,0.07); color: #a3b0c2; }"
    ".badge-value { color: #e6edf5; }"
    ".device-addr { color: #e6edf5; }"
    ".device-seen { color: #6e7c8f; }"
    ".file-name { color: #a3b0c2; }"
    "button.secondary { background-color: #1d242e; color: #e6edf5;"
    " border-color: rgba(240,244,250,0.14); }"
    "button.secondary:hover { background-color: #242c38; }"
    ".title { color: #e6edf5; }"
    "entry { background-color: #242c38; color: #e6edf5;"
    " border-color: rgba(240,244,250,0.14); caret-color: #e6edf5; }"
    "combobox button { background-color: #242c38; color: #e6edf5;"
    " border-color: rgba(240,244,250,0.14); }"
    "combobox button:hover { background-color: #2c3644; }"
    "combobox window, combobox menu, menu, popover {"
    " background-color: #1d242e; color: #e6edf5; }"
    "menu menuitem:hover, popover list row:hover { background-color: #242c38; }";

static GtkCssProvider *wd_css_provider = NULL;

/* wd_prefers_dark asks the desktop whether it wants a dark theme. GTK exposes
   this as a setting rather than a media query, so it has to be read and then
   watched for changes. */
static gboolean wd_prefers_dark(void) {
    GtkSettings *settings = gtk_settings_get_default();
    gboolean dark = FALSE;
    if (settings == NULL) {
        return FALSE;
    }
    g_object_get(settings, "gtk-application-prefer-dark-theme", &dark, NULL);
    return dark;
}

static void wd_apply_css(void) {
    if (wd_css_provider == NULL) {
        return;
    }
    /* Light first, then dark on top: the dark block only overrides colours, so
       appending keeps one provider instead of swapping two. */
    gtk_css_provider_load_from_data(wd_css_provider, wd_css, -1, NULL);
    if (wd_prefers_dark()) {
        GtkCssProvider *dark = gtk_css_provider_new();
        gtk_css_provider_load_from_data(dark, wd_css_dark, -1, NULL);
        gtk_style_context_add_provider_for_screen(gdk_screen_get_default(),
                                                  GTK_STYLE_PROVIDER(dark),
                                                  GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
        g_object_unref(dark);
    }
}

static void wd_on_theme_changed(GtkSettings *settings, GParamSpec *pspec, gpointer data) {
    (void)settings;
    (void)pspec;
    (void)data;
    wd_apply_css();
}

static void wd_install_css(void) {
    wd_css_provider = gtk_css_provider_new();
    gtk_style_context_add_provider_for_screen(gdk_screen_get_default(),
                                              GTK_STYLE_PROVIDER(wd_css_provider),
                                              GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
    wd_apply_css();

    GtkSettings *settings = gtk_settings_get_default();
    if (settings != NULL) {
        g_signal_connect(settings, "notify::gtk-application-prefer-dark-theme",
                         G_CALLBACK(wd_on_theme_changed), NULL);
    }
}

/* ---- Overview rows -------------------------------------------------------
   These do not fit the label/control shape, so each kind gets its own builder.
   Everything they need arrived pre-formatted from Go. */

static void wd_add_stat_row(GtkWidget *box, int row) {
    GtkWidget *line = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 32);
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        gchar *value = wd_field_string(row, column, docwebhub_form_field_value);
        gchar *label = wd_field_string(row, column, docwebhub_form_field_label);

        GtkWidget *cell = gtk_box_new(GTK_ORIENTATION_VERTICAL, 2);
        GtkWidget *big = gtk_label_new(value);
        WDField *record = wd_record(row, column, "stat");
        record->widget = big;
        wd_add_class(big, "stat-value");
        gtk_label_set_xalign(GTK_LABEL(big), 0.0);
        GtkWidget *caption = gtk_label_new(label);
        wd_add_class(caption, "stat-label");
        gtk_label_set_xalign(GTK_LABEL(caption), 0.0);
        gtk_box_pack_start(GTK_BOX(cell), big, FALSE, FALSE, 0);
        gtk_box_pack_start(GTK_BOX(cell), caption, FALSE, FALSE, 0);
        gtk_box_pack_start(GTK_BOX(line), cell, FALSE, FALSE, 0);

        g_free(value);
        g_free(label);
    }
    gtk_box_pack_start(GTK_BOX(box), line, FALSE, FALSE, 0);
}

static void wd_add_badge_row(GtkWidget *box, int row) {
    GtkWidget *line = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        gchar *value = wd_field_string(row, column, docwebhub_form_field_value);
        gchar *label = wd_field_string(row, column, docwebhub_form_field_label);

        GtkWidget *pill = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
        wd_add_class(pill, "badge");
        GtkWidget *big = gtk_label_new(value);
        WDField *record = wd_record(row, column, "badge");
        record->widget = big;
        wd_add_class(big, "badge-value");
        GtkWidget *caption = gtk_label_new(label);
        gtk_box_pack_start(GTK_BOX(pill), big, FALSE, FALSE, 0);
        gtk_box_pack_start(GTK_BOX(pill), caption, FALSE, FALSE, 0);
        gtk_box_pack_start(GTK_BOX(line), pill, FALSE, FALSE, 0);

        g_free(value);
        g_free(label);
    }
    gtk_box_pack_start(GTK_BOX(box), line, FALSE, FALSE, 0);
}

static void wd_add_section_title(GtkWidget *box, const char *text) {
    GtkWidget *label = gtk_label_new(text);
    wd_add_class(label, "row-title");
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    gtk_box_pack_start(GTK_BOX(box), label, FALSE, FALSE, 0);
}

/* One device per line, already formatted as "addr · seen" in Go. A single
   multi-line label keeps the refresh path identical to every other field. */
static void wd_add_device_list(GtkWidget *box, int row) {
    gchar *title = wd_field_string(row, 0, docwebhub_form_field_label);
    wd_add_section_title(box, title);
    g_free(title);

    gchar *data = wd_field_string(row, 0, docwebhub_form_field_value);
    gchar *shown = data;
    gchar *empty_text = NULL;
    if (data == NULL || data[0] == '\0') {
        empty_text = wd_text("ui.overview.empty");
        shown = empty_text;
    }

    GtkWidget *label = gtk_label_new(shown);
    WDField *record = wd_record(row, 0, "device-list");
    record->widget = label;
    wd_add_class(label, "device-addr");
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    gtk_box_pack_start(GTK_BOX(box), label, FALSE, FALSE, 0);
    g_free(data);
    g_free(empty_text);
}

static void wd_add_file_list(GtkWidget *box, int row) {
    gchar *title = wd_field_string(row, 0, docwebhub_form_field_label);
    wd_add_section_title(box, title);
    g_free(title);

    gchar *data = wd_field_string(row, 0, docwebhub_form_field_value);
    gchar *shown = data;
    gchar *empty_text = NULL;
    if (data == NULL || data[0] == '\0') {
        empty_text = wd_text("ui.overview.empty");
        shown = empty_text;
    }

    GtkWidget *label = gtk_label_new(shown);
    WDField *record = wd_record(row, 0, "file-list");
    record->widget = label;
    wd_add_class(label, "file-name");
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    gtk_box_pack_start(GTK_BOX(box), label, FALSE, FALSE, 0);
    g_free(data);
    g_free(empty_text);
}

/* Request bars. Drawn rather than assembled from widgets: 24 boxes with fixed
   heights would fight the layout, and cairo is already linked for GTK. */
typedef struct {
    int values[24];
    int count;
} WDSparkData;

static gboolean wd_on_draw_spark(GtkWidget *widget, cairo_t *cr, gpointer data) {
    WDSparkData *spark = (WDSparkData *)data;
    int width = gtk_widget_get_allocated_width(widget);
    int height = gtk_widget_get_allocated_height(widget);
    if (spark == NULL || spark->count <= 0 || width <= 0 || height <= 0) {
        return FALSE;
    }

    int peak = 0;
    for (int i = 0; i < spark->count; i++) {
        if (spark->values[i] > peak) {
            peak = spark->values[i];
        }
    }
    if (peak == 0) {
        /* Nothing has been requested yet: a flat row of marks would claim data
           that does not exist. Draw a baseline instead. */
        cairo_set_source_rgba(cr, 0.5, 0.55, 0.6, 0.45);
        cairo_set_line_width(cr, 1.0);
        cairo_move_to(cr, 0, height - 0.5);
        cairo_line_to(cr, width, height - 0.5);
        cairo_stroke(cr);
        return FALSE;
    }

    double gap = 3.0;
    double bar = (width - gap * (spark->count - 1)) / spark->count;
    if (bar < 2.0) {
        bar = 2.0;
    }

    cairo_set_source_rgb(cr, 0.886, 0.314, 0.055);
    for (int i = 0; i < spark->count; i++) {
        double h = (double)height * spark->values[i] / peak;
        if (h < 3.0) {
            h = 3.0; /* an empty hour still shows a mark, so the axis reads */
        }
        cairo_rectangle(cr, i * (bar + gap), height - h, bar, h);
    }
    cairo_fill(cr);
    return FALSE;
}

static void wd_add_sparkline(GtkWidget *box, int row) {
    gchar *title = wd_field_string(row, 0, docwebhub_form_field_label);
    wd_add_section_title(box, title);
    g_free(title);

    WDSparkData *spark = g_new0(WDSparkData, 1);
    gchar *data = wd_field_string(row, 0, docwebhub_form_field_value);
    if (data != NULL && data[0] != '\0') {
        gchar **parts = g_strsplit(data, ",", -1);
        for (int i = 0; parts[i] != NULL && spark->count < 24; i++) {
            spark->values[spark->count++] = atoi(parts[i]);
        }
        g_strfreev(parts);
    }
    g_free(data);

    WDField *record = wd_record(row, 0, "sparkline");
    GtkWidget *area = gtk_drawing_area_new();
    gtk_widget_set_size_request(area, -1, 44);
    record->widget = area;
    g_object_set_data(G_OBJECT(area), "spark", spark);
    g_signal_connect(area, "draw", G_CALLBACK(wd_on_draw_spark), spark);
    g_signal_connect_swapped(area, "destroy", G_CALLBACK(g_free), spark);
    gtk_box_pack_start(GTK_BOX(box), area, FALSE, FALSE, 0);
}

/* Two seconds: fast enough that opening the window and loading a page shows up
   while the user is still looking, slow enough that it costs nothing. */
static void wd_start_refresh_timer(void) {
    g_timeout_add_seconds(2, wd_on_refresh_tick, NULL);
}

static gboolean wd_overview_kind(const char *kind) {
    return g_strcmp0(kind, "stat") == 0 || g_strcmp0(kind, "badge") == 0 ||
           g_strcmp0(kind, "device-list") == 0 || g_strcmp0(kind, "file-list") == 0;
}

static gboolean wd_on_refresh_tick(gpointer data) {
    (void)data;
    docwebhub_form_refresh_stats();
    for (guint i = 0; i < wd_fields->len; i++) {
        WDField *field = g_ptr_array_index(wd_fields, i);
        if (field->widget == NULL) {
            continue;
        }
        if (wd_overview_kind(field->kind)) {
            wd_apply_field(field, FALSE);
        } else if (g_strcmp0(field->kind, "sparkline") == 0) {
            WDSparkData *spark = g_object_get_data(G_OBJECT(field->widget), "spark");
            gchar *value = wd_field_string(field->row, field->field, docwebhub_form_field_value);
            if (spark != NULL) {
                spark->count = 0;
                if (value != NULL && value[0] != '\0') {
                    gchar **parts = g_strsplit(value, ",", -1);
                    for (int i = 0; parts[i] != NULL && spark->count < 24; i++) {
                        spark->values[spark->count++] = atoi(parts[i]);
                    }
                    g_strfreev(parts);
                }
                gtk_widget_queue_draw(field->widget);
            }
            g_free(value);
        }
    }
    return G_SOURCE_CONTINUE;
}

/* ---- Tab strip ---------------------------------------------------------- */

/* One content area, rebuilt on every tab change. Four stacked pages made the
   box report the sum of their heights as its natural size, which forced the
   window taller than any single page needed. */
static GtkWidget *wd_content = NULL;
static GtkWidget *wd_main_box = NULL;
static GtkWidget *wd_window_widget = NULL;
static const char *wd_tab_groups[4] = {"overview", "settings", "security", "language"};


static void wd_tab_init_keys(void) {
    wd_tab_keys[0] = "ui.tab.overview";
    wd_tab_keys[1] = "ui.tab.settings";
    wd_tab_keys[2] = "ui.tab.security";
    wd_tab_keys[3] = "ui.tab.language";
}

/* wd_fit_window shaves the window down to the page on screen. GTK keeps the
   height from the first layout, so without this every shorter tab sits above a
   gap. */
static void wd_fit_window(void) {
    if (wd_window_widget == NULL || wd_main_box == NULL) {
        return;
    }
    /* Only the visible page is in the tree now, so its preferred height is what
       the window needs - no sum of hidden siblings. */
    int min_h = 0, nat_h = 0;
    gtk_widget_get_preferred_height(wd_main_box, &min_h, &nat_h);
    if (nat_h <= 0) {
        return;
    }
    int width = 0, height = 0;
    gtk_window_get_size(GTK_WINDOW(wd_window_widget), &width, &height);
    if (width <= 0) {
        width = 720;
    }
    gtk_window_resize(GTK_WINDOW(wd_window_widget), width, nat_h);
}

/* wd_fit_on_idle shrinks the window to what the visible page needs. It runs
   once, after the first layout, because a resize requested before that is
   discarded when GTK allocates the window for the first time. */
static gboolean wd_fit_on_idle(gpointer data) {
    GtkWindow *window = GTK_WINDOW(data);
    if (wd_main_box == NULL) {
        return G_SOURCE_REMOVE;
    }
    int nat_h = 0, min_h = 0;
    gtk_widget_get_preferred_height(wd_main_box, &min_h, &nat_h);
    if (nat_h > 0) {
        gtk_window_resize(window, 720, nat_h);
    }
    return G_SOURCE_REMOVE;
}

static void wd_render_page(const char *group);

/* wd_show_page replaces the content area with the requested tab. Destroying the
   old widgets is the whole point: only the visible page contributes to the
   window's height, so the window can shrink to fit a short tab. */
static void wd_show_page(const char *group) {
    if (wd_content == NULL || group == NULL) {
        return;
    }
    wd_active_group = group;

    GList *children = gtk_container_get_children(GTK_CONTAINER(wd_content));
    for (GList *item = children; item != NULL; item = item->next) {
        gtk_widget_destroy(GTK_WIDGET(item->data));
    }
    g_list_free(children);

    wd_clear_records();
    wd_render_page(group);

    /* The widgets were created empty; filling values and options is the job of
       the same pass the language switch uses. Without it a rebuild leaves every
       combo box blank and every input stale. */
    wd_apply_labels();

    gtk_widget_show_all(wd_content);
    wd_fit_window();
}

static void wd_select_tab(GtkWidget *button) {
    for (int i = 0; i < 4; i++) {
        if (wd_tab_buttons[i] == NULL) {
            continue;
        }
        if (wd_tab_buttons[i] == button) {
            wd_add_class(wd_tab_buttons[i], "tab-active");
        } else {
            wd_remove_class(wd_tab_buttons[i], "tab-active");
        }
    }

    const char *group = (const char *)g_object_get_data(G_OBJECT(button), "page-group");
    if (group != NULL) {
        wd_show_page(group);
    }
}

static void wd_on_tab_clicked(GtkToggleButton *button, gpointer data) {
    (void)data;
    wd_select_tab(GTK_WIDGET(button));
}

/* ---- Overview page -------------------------------------------------------
   Built as one block, not row by row: the top band (status, numbers, badges)
   spans the width, and the two lists sit side by side underneath it, the way
   the website lays them out. Splitting this per row would only ever produce a
   single column. */

static void wd_add_text_block(GtkWidget *box, const char *title, const char *body,
                              const char *body_class) {
    GtkWidget *wrap = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
    GtkWidget *heading = gtk_label_new(title);
    wd_add_class(heading, "row-title");
    gtk_label_set_xalign(GTK_LABEL(heading), 0.0);
    gtk_box_pack_start(GTK_BOX(wrap), heading, FALSE, FALSE, 0);

    gchar *shown = (body != NULL && body[0] != '\0') ? g_strdup(body) : wd_text("ui.overview.empty");
    GtkWidget *label = gtk_label_new(shown);
    wd_add_class(label, body_class);
    gtk_label_set_xalign(GTK_LABEL(label), 0.0);
    gtk_label_set_line_wrap(GTK_LABEL(label), TRUE);
    gtk_box_pack_start(GTK_BOX(wrap), label, FALSE, FALSE, 0);
    g_free(shown);
    gtk_box_pack_start(GTK_BOX(box), wrap, FALSE, FALSE, 0);
}

static gboolean wd_overview_kind(const char *kind);

static void wd_build_overview(GtkWidget *page) {
    GtkWidget *top = gtk_box_new(GTK_ORIENTATION_VERTICAL, 8);
    GtkWidget *columns = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 28);
    GtkWidget *left = gtk_box_new(GTK_ORIENTATION_VERTICAL, 8);
    GtkWidget *right = gtk_box_new(GTK_ORIENTATION_VERTICAL, 12);

    int row_count = docwebhub_form_row_count();
    for (int row = 0; row < row_count; row++) {
        gchar *group = wd_row_string(row, docwebhub_form_row_group);
        if (g_strcmp0(group, "overview") != 0) {
            g_free(group);
            continue;
        }
        g_free(group);

        gchar *kind = wd_row_string(row, docwebhub_form_row_kind);
        if (g_strcmp0(kind, "header") == 0) {
            GtkWidget *header = gtk_box_new(GTK_ORIENTATION_VERTICAL, 2);
            int count = docwebhub_form_field_count(row);
            for (int column = 0; column < count; column++) {
                GtkWidget *widget = wd_create_widget(row, column, "header", NULL);
                gtk_widget_set_halign(widget, GTK_ALIGN_FILL);
                gtk_box_pack_start(GTK_BOX(header), widget, FALSE, TRUE, 0);
            }
            gtk_box_pack_start(GTK_BOX(top), header, FALSE, FALSE, 0);
        } else if (g_strcmp0(kind, "stat") == 0) {
            wd_add_stat_row(top, row);
        } else if (g_strcmp0(kind, "badge") == 0) {
            wd_add_badge_row(top, row);
        } else if (g_strcmp0(kind, "device-list") == 0) {
            wd_add_device_list(left, row);
        } else if (g_strcmp0(kind, "sparkline") == 0) {
            wd_add_sparkline(right, row);
        } else if (g_strcmp0(kind, "file-list") == 0) {
            wd_add_file_list(right, row);
        }
        g_free(kind);
    }

    gtk_widget_set_size_request(left, 220, -1);
    gtk_widget_set_valign(left, GTK_ALIGN_START);
    gtk_widget_set_valign(right, GTK_ALIGN_START);
    gtk_box_pack_start(GTK_BOX(columns), left, TRUE, TRUE, 0);
    gtk_box_pack_start(GTK_BOX(columns), right, TRUE, TRUE, 0);

    gtk_box_pack_start(GTK_BOX(page), top, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(page), columns, FALSE, FALSE, 0);
}

/* wd_render_page rebuilds the content area for one tab. Rows tagged "chrome"
   (the address line, the footer hint) are rendered once into the window rather
   than into the page, so they survive a tab switch. */
static void wd_render_page(const char *group) {
    GtkWidget *page = gtk_box_new(GTK_ORIENTATION_VERTICAL, 12);
    wd_add_class(page, "page");
    gtk_widget_set_valign(page, GTK_ALIGN_START);
    gtk_box_pack_start(GTK_BOX(wd_content), page, FALSE, FALSE, 0);

    if (g_strcmp0(group, "overview") == 0) {
        wd_build_overview(page);
        return;
    }

    GtkWidget *card = NULL;
    int row_count = docwebhub_form_row_count();
    for (int row = 0; row < row_count; row++) {
        gchar *row_group = wd_row_string(row, docwebhub_form_row_group);
        gchar *kind = wd_row_string(row, docwebhub_form_row_kind);

        if (g_strcmp0(row_group, group) != 0) {
            g_free(row_group);
            g_free(kind);
            continue;
        }

        if (g_strcmp0(kind, "setting") == 0) {
            if (card == NULL) {
                card = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
                wd_add_class(card, "card");
                gtk_box_pack_start(GTK_BOX(page), card, FALSE, FALSE, 0);
            }
            wd_add_setting_row(card, row);
        } else if (g_strcmp0(kind, "footer") == 0) {
            wd_add_footer_row(page, row);
        }
        g_free(row_group);
        g_free(kind);
    }
}

/* wd_build_chrome renders the rows that never move: the address row and the
   quiet note under it. Built once, outside the content area. */
static void wd_build_chrome(GtkWidget *main_box) {
    /* The address and the "keeps running" note sit in a bar of their own, with a
       tinted fill and a rule above it, so the content above reads as one pane
       and this as its footer. */
    GtkWidget *footer = gtk_box_new(GTK_ORIENTATION_VERTICAL, 2);
    wd_add_class(footer, "footer");
    gtk_box_pack_start(GTK_BOX(main_box), footer, FALSE, FALSE, 0);

    int row_count = docwebhub_form_row_count();
    for (int row = 0; row < row_count; row++) {
        gchar *group = wd_row_string(row, docwebhub_form_row_group);
        gchar *kind = wd_row_string(row, docwebhub_form_row_kind);
        if (g_strcmp0(group, "chrome") != 0) {
            g_free(group);
            g_free(kind);
            continue;
        }
        if (g_strcmp0(kind, "url") == 0) {
            wd_add_url_row(footer, row);
        } else if (g_strcmp0(kind, "hint") == 0) {
            wd_add_hint_row(footer, row);
        }
        g_free(group);
        g_free(kind);
    }
}

static void wd_build_form(GtkWidget *main_box) {
    wd_tab_init_keys();
    wd_main_box = main_box;

    GtkWidget *tabs = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 4);
    wd_add_class(tabs, "tabs");
    gtk_box_pack_start(GTK_BOX(main_box), tabs, FALSE, FALSE, 0);

    wd_content = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
    /* The content pane takes the spare height. Without expanding, a window
       taller than its page leaves a white strip between the page and the
       footer, which is what made the footer look like it floated. */
    gtk_box_pack_start(GTK_BOX(main_box), wd_content, TRUE, TRUE, 0);

    for (int i = 0; i < 4; i++) {
        /* A toggle button, not a radio: GTK draws a radio indicator that no
           amount of CSS makes invisible on every theme, and the tab strip only
           needs "exactly one is active". */
        /* A plain button, not a toggle: keeping the "which tab is active" state
           in a CSS class means selecting a tab never emits a signal of its own,
           which a toggle button does and which made the selection loop. */
        GtkWidget *button = gtk_button_new_with_label("");
        wd_add_class(button, "tab");
        g_object_set_data(G_OBJECT(button), "page-group", (gpointer)wd_tab_groups[i]);
        g_signal_connect(button, "clicked", G_CALLBACK(wd_on_tab_clicked), NULL);
        gtk_box_pack_start(GTK_BOX(tabs), button, FALSE, FALSE, 0);
        wd_tab_buttons[i] = button;
    }

    wd_build_chrome(main_box);

    /* Tab labels come from the translation table and are refreshed with the
       rest of the window when the language changes. */
    for (int i = 0; i < 4; i++) {
        gchar *label = wd_text(wd_tab_keys[i]);
        gtk_button_set_label(GTK_BUTTON(wd_tab_buttons[i]), label);
        g_free(label);
    }
}

int docwebhub_run_gui(void) {
    if (!gtk_init_check(NULL, NULL)) {
        return 1;
    }

    wd_fields = g_ptr_array_new();
    wd_row_labels = g_ptr_array_new();
    wd_product = wd_take_string(docwebhub_form_title());

    wd_install_css();

    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    wd_window = window;
    gtk_window_set_title(GTK_WINDOW(window), wd_product);
    /* Taller and wider than the old form: the overview needs room for the
       device list and the request bars side by side, and the tab strip takes a
       row of its own. */
    gtk_window_set_default_size(GTK_WINDOW(window), 720, 430);
    gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
    gtk_window_set_resizable(GTK_WINDOW(window), TRUE);
    gtk_widget_set_size_request(window, 560, 400);
    gtk_container_set_border_width(GTK_CONTAINER(window), 0);
    g_signal_connect(window, "delete-event", G_CALLBACK(wd_on_delete), NULL);
    g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);

    GtkWidget *main_box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 10);
    gtk_container_add(GTK_CONTAINER(window), main_box);
    wd_build_form(main_box);

    wd_build_menu();
    wd_setup_tray();
    wd_apply_labels();
    wd_start_refresh_timer();

    gtk_widget_show_all(window);

    wd_window_widget = window;

    /* Render the first tab only after the window exists: wd_show_page measures
       the page it just built and resizes the window to match. */
    if (wd_tab_buttons[0] != NULL) {
        wd_select_tab(wd_tab_buttons[0]);
    }

    /* The resize above happens before the window is mapped, and a size request
       made then is discarded by the compositor. Queue one for the first idle
       moment instead, which is after the window has been shown. */
    g_idle_add(wd_fit_on_idle, window);
    gtk_main();
    return 0;
}
