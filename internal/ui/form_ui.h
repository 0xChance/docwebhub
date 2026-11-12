/*
 * Copyright (C) 2026 docWebHub contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
 * Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.
 */

#ifndef WEBDOCAST_FORM_UI_H
#define WEBDOCAST_FORM_UI_H

/*
 * The native renderers get the window description from Go through this small
 * ABI. Every function that returns char * returns newly allocated memory; the
 * caller owns it and must release it with free().
 *
 * Field kinds: status, heading, description, link, value, input, number,
 * password, button, select, hint, spacer.
 * Row kinds: header, url, setting, hint, footer.
 * Width: -1 takes the remaining space, 0 is natural, positive is logical points.
 */

char *docwebhub_form_title(void);

int docwebhub_form_row_count(void);
char *docwebhub_form_row_id(int row);
char *docwebhub_form_row_kind(int row);
char *docwebhub_form_row_label(int row);

int docwebhub_form_field_count(int row);
char *docwebhub_form_field_id(int row, int field);
char *docwebhub_form_field_kind(int row, int field);
char *docwebhub_form_field_label(int row, int field);
char *docwebhub_form_field_value(int row, int field);
char *docwebhub_form_field_placeholder(int row, int field);
char *docwebhub_form_field_tooltip(int row, int field);
char *docwebhub_form_field_accessibility(int row, int field);
char *docwebhub_form_field_action(int row, int field);
char *docwebhub_form_field_style(int row, int field);
char *docwebhub_form_field_icon(int row, int field);
int docwebhub_form_field_width(int row, int field);

int docwebhub_form_field_option_count(int row, int field);
char *docwebhub_form_field_option_label(int row, int field, int option);
char *docwebhub_form_field_option_value(int row, int field, int option);

int docwebhub_form_menu_count(void);
char *docwebhub_form_menu_label(int index);
char *docwebhub_form_menu_action(int index);
int docwebhub_form_menu_separator(int index);

char *docwebhub_text(const char *key);

char *docwebhub_action(const char *action, const char *value1, const char *value2);
char *docwebhub_action_error_title(void);
char *docwebhub_action_text(void);
int docwebhub_labels_changed(void);
int docwebhub_changed_count(void);
char *docwebhub_changed_id(int index);

#endif
