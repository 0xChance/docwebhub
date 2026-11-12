
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
static void wd_show_access_info(const char *text);

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

    if (docwebhub_labels_changed()) {
        wd_apply_labels();
    }
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
        g_signal_connect(widget, "activate-link", G_CALLBACK(wd_on_link_activate), field);
    } else if (g_strcmp0(kind, "value") == 0) {
        widget = wd_label("", "middle");
        gtk_label_set_ellipsize(GTK_LABEL(widget), PANGO_ELLIPSIZE_MIDDLE);
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

static void wd_add_setting_row(GtkWidget *card, int row) {
    GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
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
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        int width = docwebhub_form_field_width(row, column);
        GtkWidget *widget = wd_create_widget(row, column, "url", NULL);
        wd_pack_field(row_box, widget, width);
    }
    gtk_box_pack_start(GTK_BOX(box), row_box, FALSE, FALSE, 0);
}

static void wd_add_hint_row(GtkWidget *box, int row) {
    int count = docwebhub_form_field_count(row);
    for (int column = 0; column < count; column++) {
        GtkWidget *widget = wd_create_widget(row, column, "hint", NULL);
        gtk_box_pack_start(GTK_BOX(box), widget, FALSE, TRUE, 0);
    }
}

static void wd_add_footer_row(GtkWidget *box, int row) {
    GtkWidget *row_box = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
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
            GtkWidget *widget = wd_create_widget(row, column, "footer", NULL);
            wd_pack_field(fields, widget, width);
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

static const char *wd_css =
    ".card { background-color: rgba(0,0,0,0.04); border: 1px solid rgba(0,0,0,0.08);"
    " border-radius: 8px; padding: 16px; }"
    "button.primary { background-image: none; background-color: #007AFF; color: #FFFFFF;"
    " border: 1px solid #007AFF; border-radius: 6px; padding: 5px 14px;"
    " text-shadow: none; box-shadow: none; }"
    "button.primary:hover { background-color: #0A84FF; }"
    "button.secondary { background-image: none; background-color: #FFFFFF;"
    " border: 1px solid rgba(0,0,0,0.14); border-radius: 6px; padding: 5px 12px;"
    " box-shadow: none; }"
    "button.secondary:hover { background-color: #F5F5F5; }"
    ".status-dot { color: #28CD41; font-size: 15px;"
    " text-shadow: 0 0 8px rgba(40,205,65,0.85); }"
    ".status-dot.warn { color: #FF9500; text-shadow: 0 0 8px rgba(255,149,0,0.85); }"
    ".title { font-size: 22px; font-weight: bold; }"
    ".row-title { color: #6E6E73; }"
    ".muted { color: #9A9AA0; }"
    ".faint { color: #9A9AA0; }"
    ".success { color: #28A745; }"
    ".warning { color: #FF9500; }";

static void wd_install_css(void) {
    GtkCssProvider *provider = gtk_css_provider_new();
    gtk_css_provider_load_from_data(provider, wd_css, -1, NULL);
    gtk_style_context_add_provider_for_screen(gdk_screen_get_default(),
                                              GTK_STYLE_PROVIDER(provider),
                                              GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
    g_object_unref(provider);
}

static void wd_build_form(GtkWidget *main_box) {
    GtkWidget *card = NULL;
    int row_count = docwebhub_form_row_count();
    for (int row = 0; row < row_count; row++) {
        gchar *kind = wd_row_string(row, docwebhub_form_row_kind);
        if (g_strcmp0(kind, "header") == 0) {
            GtkWidget *header = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
            int count = docwebhub_form_field_count(row);
            for (int column = 0; column < count; column++) {
                GtkWidget *widget = wd_create_widget(row, column, "header", NULL);
                gtk_widget_set_halign(widget, GTK_ALIGN_FILL);
                gtk_box_pack_start(GTK_BOX(header), widget, FALSE, TRUE, 0);
            }
            gtk_box_pack_start(GTK_BOX(main_box), header, FALSE, FALSE, 0);
        } else if (g_strcmp0(kind, "url") == 0) {
            wd_add_url_row(main_box, row);
        } else if (g_strcmp0(kind, "setting") == 0) {
            if (card == NULL) {
                card = gtk_box_new(GTK_ORIENTATION_VERTICAL, 12);
                wd_add_class(card, "card");
                gtk_box_pack_start(GTK_BOX(main_box), card, FALSE, FALSE, 0);
            }
            wd_add_setting_row(card, row);
        } else if (g_strcmp0(kind, "hint") == 0) {
            wd_add_hint_row(main_box, row);
        } else if (g_strcmp0(kind, "footer") == 0) {
            wd_add_footer_row(main_box, row);
        }
        g_free(kind);
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
    gtk_window_set_default_size(GTK_WINDOW(window), 600, 560);
    gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
    gtk_window_set_resizable(GTK_WINDOW(window), FALSE);
    gtk_container_set_border_width(GTK_CONTAINER(window), 24);
    g_signal_connect(window, "delete-event", G_CALLBACK(wd_on_delete), NULL);
    g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);

    GtkWidget *main_box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 10);
    gtk_container_add(GTK_CONTAINER(window), main_box);
    wd_build_form(main_box);

    wd_build_menu();
    wd_setup_tray();
    wd_apply_labels();

    gtk_widget_show_all(window);
    gtk_main();
    return 0;
}
