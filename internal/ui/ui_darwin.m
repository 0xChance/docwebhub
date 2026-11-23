
// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

#include <stdlib.h>

#include "form_ui.h"

extern void docwebhubRequestQuit(void);

static BOOL docwebhubTerminating = NO;
static BOOL docwebhubReplied = NO;

static NSWindow *docWebHubWindow = nil;
static NSStatusItem *docWebHubStatusItem = nil;

// Called by the Go side once the server has shut down; the reply has to happen on
// the main thread, otherwise AppKit waits forever.
void docwebhub_reply_terminate(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (docwebhubReplied) {
            return;
        }
        docwebhubReplied = YES;
        [NSApp replyToApplicationShouldTerminate:YES];
    });
}

@interface DocWebHubAppDelegate : NSObject <NSApplicationDelegate>
@end

@implementation DocWebHubAppDelegate

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    // Closing the window only puts the interface away; the server stays in the menu
    // bar, so quitting has to go through "Quit" in the menu.
    return NO;
}

- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
    if (!flag && docWebHubWindow != nil) {
        [docWebHubWindow makeKeyAndOrderFront:nil];
    }
    return YES;
}

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    if (docwebhubTerminating) {
        return NSTerminateNow;
    }
    docwebhubTerminating = YES;

    // Let the Go side shut the HTTP server down gracefully first, then allow the exit.
    docwebhubRequestQuit();

    // Safety net: wait at most 8 seconds in case cleanup misbehaves, so the user is
    // never trapped in an app that refuses to close.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(8 * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{
                       docwebhub_reply_terminate();
                   });

    return NSTerminateLater;
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [NSApp activateIgnoringOtherApps:YES];
}

- (BOOL)applicationSupportsSecureRestorableState:(NSApplication *)app {
    return YES;
}

@end

// A top-left origin makes the layout below read in the same direction as the
// Win32 and GTK renderers. AppKit normally uses bottom-left coordinates.
@interface DocWebHubFlippedView : NSView
@end

@implementation DocWebHubFlippedView
- (BOOL)isFlipped {
    return YES;
}
@end

@interface DocWebHubController : NSObject
- (void)formAction:(id)sender;
- (void)tabChanged:(id)sender;
- (void)menuOpenURL:(id)sender;
- (void)menuShowWindow:(id)sender;
- (void)menuQuit:(id)sender;
@end

@interface DocWebHubLinkButton : NSButton
@end

@implementation DocWebHubLinkButton
- (void)resetCursorRects {
    [super resetCursorRects];
    [self addCursorRect:[self bounds] cursor:[NSCursor pointingHandCursor]];
}
@end

@interface DocWebHubCard : NSView
@end

@implementation DocWebHubCard

- (instancetype)initWithFrame:(NSRect)frame {
    self = [super initWithFrame:frame];
    if (self != nil) {
        [self setWantsLayer:YES];
        [self docwebhub_applyColors];
    }
    return self;
}

- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    [self docwebhub_applyColors];
}

- (void)docwebhub_applyColors {
    BOOL dark = [[self effectiveAppearance]
                    bestMatchFromAppearancesWithNames:@[ NSAppearanceNameAqua, NSAppearanceNameDarkAqua ]] ==
                NSAppearanceNameDarkAqua;
    // A card is a surface, not a tint: white on the window's grey in light mode,
    // the raised panel colour in dark. The radius and the hairline come from the
    // same token list as the website.
    NSColor *fill = dark ? [NSColor colorWithSRGBRed:0.114 green:0.141 blue:0.180 alpha:1.0]
                         : [NSColor whiteColor];
    NSColor *edge = dark ? [NSColor colorWithWhite:1.0 alpha:0.10] : [NSColor colorWithWhite:0.0 alpha:0.08];
    CALayer *layer = [self layer];
    [layer setBackgroundColor:[fill CGColor]];
    [layer setBorderColor:[edge CGColor]];
    [layer setBorderWidth:1.0];
    [layer setCornerRadius:14.0];
    [layer setShadowColor:[NSColor blackColor].CGColor];
    [layer setShadowOpacity:dark ? 0.0 : 0.06];
    [layer setShadowRadius:12.0];
    [layer setShadowOffset:CGSizeMake(0, -2)];
}

@end

// Every widget created from the form is recorded here. Native code never knows
// what a control means; it asks Go for the current label/value/options by row
// and column, and dispatches actions by field ID.
static NSMutableArray *docWebHubFields = nil;
static NSMutableArray *docWebHubRowLabels = nil;
static NSMutableArray *docWebHubMenuItems = nil;
static NSMenuItem *docWebHubAppQuitItem = nil;
static NSWindow *docWebHubInfoWindow = nil;
static NSTextView *docWebHubInfoText = nil;

typedef char *(*WDFieldStringFn)(int, int);
typedef char *(*WDRowStringFn)(int);
typedef char *(*WDMenuStringFn)(int);

static NSString *WDString(char *raw) {
    NSString *value = raw != NULL ? [NSString stringWithUTF8String:raw] : @"";
    free(raw);
    return value;
}

static NSString *WDFieldString(int row, int field, WDFieldStringFn getter) {
    return WDString(getter(row, field));
}

static NSString *WDRowString(int row, WDRowStringFn getter) {
    return WDString(getter(row));
}

static NSString *WDMenuString(int index, WDMenuStringFn getter) {
    return WDString(getter(index));
}

static NSString *WDTitle(void) {
    char *raw = docwebhub_form_title();
    NSString *title = raw != NULL ? [NSString stringWithUTF8String:raw] : @"docWebHub";
    free(raw);
    return title;
}

// LWStr returns the translation of key. The Go side owns the translations, so
// every visible sentence is fetched rather than written here.
static NSString *LWStr(const char *key) {
    char *raw = docwebhub_text(key);
    NSString *text = raw ? [NSString stringWithUTF8String:raw] : @"";
    free(raw);
    return [text stringByReplacingOccurrencesOfString:@"%s" withString:@"%@"];
}

static NSColor *WDTextColor(NSString *style, NSString *kind, NSString *rowKind) {
    if ([kind isEqualToString:@"heading"]) {
        return [NSColor labelColor];
    }
    if ([kind isEqualToString:@"link"]) {
        return [NSColor linkColor];
    }
    if ([rowKind isEqualToString:@"header"] && [kind isEqualToString:@"status"]) {
        return [NSColor secondaryLabelColor];
    }
    if ([style isEqualToString:@"success"]) {
        return [NSColor systemGreenColor];
    }
    if ([style isEqualToString:@"warning"]) {
        return [NSColor systemOrangeColor];
    }
    if ([style isEqualToString:@"muted"]) {
        return [NSColor secondaryLabelColor];
    }
    if ([style isEqualToString:@"faint"] || [kind isEqualToString:@"hint"] ||
        [kind isEqualToString:@"description"]) {
        return [NSColor tertiaryLabelColor];
    }
    return [NSColor labelColor];
}

static void WDSetStatusDot(NSView *dot, NSString *style) {
    if (dot == nil) {
        return;
    }
    NSColor *color = [NSColor colorWithSRGBRed:0.157 green:0.804 blue:0.255 alpha:1.0];
    if ([style isEqualToString:@"warning"]) {
        color = [NSColor systemOrangeColor];
    }
    CALayer *layer = [dot layer];
    [layer setBackgroundColor:[color CGColor]];
    [layer setShadowColor:[color CGColor]];
    [layer setShadowOpacity:1.0];
    [layer setShadowRadius:5];
    [layer setShadowOffset:CGSizeZero];
}

static NSAttributedString *WDPlaceholder(NSString *text) {
    return [[NSAttributedString alloc] initWithString:text
                                           attributes:@{
                                               NSForegroundColorAttributeName: [NSColor tertiaryLabelColor],
                                               NSFontAttributeName: [NSFont systemFontOfSize:12],
                                           }];
}

static void WDSetButtonTitle(NSButton *button, NSString *title, NSString *style) {
    if ([style isEqualToString:@"primary"]) {
        NSMutableParagraphStyle *paragraph = [[NSMutableParagraphStyle alloc] init];
        [paragraph setAlignment:NSTextAlignmentCenter];
        [button setAttributedTitle:[[NSAttributedString alloc] initWithString:title
                                                                   attributes:@{
                                                                       NSFontAttributeName: [button font],
                                                                       NSForegroundColorAttributeName: [NSColor whiteColor],
                                                                       NSParagraphStyleAttributeName: paragraph,
                                                                   }]];
    } else {
        [button setTitle:title];
    }
}

static void WDSetLink(NSButton *button, NSString *url, NSString *accessibility) {
    NSString *text = [NSString stringWithFormat:@"%@  ↗", url];
    NSMutableAttributedString *attributed =
        [[NSMutableAttributedString alloc] initWithString:text
                                               attributes:@{
                                                   NSFontAttributeName: [NSFont systemFontOfSize:14
                                                                                           weight:NSFontWeightMedium],
                                                   NSForegroundColorAttributeName: [NSColor linkColor],
                                                   NSUnderlineStyleAttributeName: @(NSUnderlineStyleSingle),
                                               }];
    [button setAttributedTitle:attributed];
    [button setAccessibilityLabel:accessibility];
}

static void WDAlert(NSString *title, NSString *message) {
    NSAlert *alert = [[NSAlert alloc] init];
    [alert setMessageText:title];
    [alert setInformativeText:message];
    [alert setAlertStyle:NSAlertStyleWarning];
    [alert runModal];
}

static NSImage *WDSymbol(NSString *name) {
    if (name.length == 0 ||
        ![NSImage respondsToSelector:@selector(imageWithSystemSymbolName:accessibilityDescription:)]) {
        return nil;
    }
    NSImage *image = [NSImage imageWithSystemSymbolName:name accessibilityDescription:nil];
    [image setSize:NSMakeSize(13, 13)];
    return image;
}

static void WDStyleButton(NSButton *button, NSString *style, NSString *icon) {
    [button setButtonType:NSButtonTypeMomentaryPushIn];
    [button setBezelStyle:NSBezelStyleRounded];
    if ([style isEqualToString:@"primary"]) {
        [button setFont:[NSFont systemFontOfSize:12 weight:NSFontWeightSemibold]];
        [button setBezelColor:[NSColor colorWithSRGBRed:0.0 green:0.478 blue:1.0 alpha:1.0]];
        [button setContentTintColor:[NSColor whiteColor]];
        return;
    }

    [button setFont:[NSFont systemFontOfSize:12 weight:NSFontWeightMedium]];
    NSString *symbol = nil;
    if ([icon isEqualToString:@"copy"]) {
        symbol = @"doc.on.doc";
    } else if ([icon isEqualToString:@"folder"]) {
        symbol = @"folder";
    }
    NSImage *image = WDSymbol(symbol);
    if (image != nil) {
        [button setImage:image];
        [button setImagePosition:NSImageLeading];
    }
}

static NSTextField *WDLabel(NSString *text, NSRect frame, CGFloat size, NSColor *color, BOOL semibold) {
    NSTextField *label = [[NSTextField alloc] initWithFrame:frame];
    [label setStringValue:text];
    [label setBezeled:NO];
    [label setDrawsBackground:NO];
    [label setEditable:NO];
    [label setSelectable:NO];
    [label setFont:semibold ? [NSFont systemFontOfSize:size weight:NSFontWeightSemibold]
                            : [NSFont systemFontOfSize:size]];
    [label setTextColor:color];
    [label setLineBreakMode:NSLineBreakByTruncatingMiddle];
    return label;
}

static CGFloat WDFieldHeight(NSString *kind) {
    if ([kind isEqualToString:@"button"]) {
        return 30;
    }
    if ([kind isEqualToString:@"select"]) {
        return 28;
    }
    if ([kind isEqualToString:@"input"] || [kind isEqualToString:@"number"] ||
        [kind isEqualToString:@"password"]) {
        return 24;
    }
    if ([kind isEqualToString:@"link"]) {
        return 26;
    }
    if ([kind isEqualToString:@"heading"]) {
        return 32;
    }
    if ([kind isEqualToString:@"hint"]) {
        return 16;
    }
    return 18;
}

static NSString *WDFieldKind(int row, int column) {
    return WDFieldString(row, column, docwebhub_form_field_kind);
}

static NSDictionary *WDFieldByID(NSString *wanted) {
    for (NSDictionary *field in docWebHubFields) {
        if ([field[@"id"] isEqualToString:wanted]) {
            return field;
        }
    }
    return nil;
}

static NSArray<NSNumber *> *WDWidths(NSArray<NSDictionary *> *fields, CGFloat total, CGFloat gap) {
    NSUInteger count = [fields count];
    if (count == 0) {
        return @[];
    }

    CGFloat fixed = 0;
    NSInteger stretch = 0;
    for (NSDictionary *field in fields) {
        CGFloat width = [field[@"width"] doubleValue];
        if (width < 0) {
            stretch++;
        } else {
            fixed += width;
        }
    }

    CGFloat available = MAX(0, total - gap * (count - 1));
    CGFloat stretchWidth = stretch > 0 ? MAX(0, (available - fixed) / stretch) : 0;
    NSMutableArray<NSNumber *> *widths = [NSMutableArray arrayWithCapacity:count];
    for (NSDictionary *field in fields) {
        CGFloat width = [field[@"width"] doubleValue];
        if (width < 0) {
            width = stretchWidth;
        }
        [widths addObject:@(width)];
    }
    return widths;
}

static NSString *WDMenuLabelForAction(NSString *wanted) {
    int count = docwebhub_form_menu_count();
    for (int i = 0; i < count; i++) {
        NSString *action = WDMenuString(i, docwebhub_form_menu_action);
        if ([action isEqualToString:wanted]) {
            return WDMenuString(i, docwebhub_form_menu_label);
        }
    }
    return @"";
}

// The tab machinery and the two drawn overview widgets live further down; the
// controller and the field update path need them before their definitions.
static void WDRebuild(NSString *group);
static void WDLayoutChrome(void);
static void WDRefreshTabLabels(void);
static NSString *WDGroupAtIndex(NSInteger index);
static void WDApplyBadgeView(id badge, NSString *value, NSString *caption);
static void WDApplySparkline(id spark, NSString *value);

static void WDApplyField(NSDictionary *field, BOOL updateValue);
static void WDApplyChanges(void);
static void WDRefreshLabels(void);

static NSDictionary *WDCreateField(int row, int column, NSString *rowKind, NSRect frame,
                                  DocWebHubController *controller, NSView *content) {
    NSString *kind = WDFieldKind(row, column);
    NSString *fieldID = WDFieldString(row, column, docwebhub_form_field_id);
    NSString *action = WDFieldString(row, column, docwebhub_form_field_action);
    NSString *style = WDFieldString(row, column, docwebhub_form_field_style);
    NSString *icon = WDFieldString(row, column, docwebhub_form_field_icon);
    NSNumber *width = @(docwebhub_form_field_width(row, column));

    NSControl *control = nil;
    NSView *dot = nil;

    if ([kind isEqualToString:@"spacer"]) {
        // A spacer has no widget; it only consumes horizontal space.
    } else if ([kind isEqualToString:@"status"]) {
        if ([rowKind isEqualToString:@"header"]) {
            NSRect dotFrame = NSMakeRect(frame.origin.x,
                                         frame.origin.y + (frame.size.height - 10) / 2,
                                         10, 10);
            dot = [[NSView alloc] initWithFrame:dotFrame];
            [dot setWantsLayer:YES];
            [[dot layer] setCornerRadius:5.0];
            [content addSubview:dot];
            frame.origin.x += 18;
            frame.size.width -= 18;
        }
        control = WDLabel(@"", frame, 12, [NSColor secondaryLabelColor], NO);
        [(NSTextField *)control setLineBreakMode:NSLineBreakByTruncatingTail];
    } else if ([kind isEqualToString:@"heading"]) {
        control = WDLabel(@"", frame, 22, [NSColor labelColor], YES);
    } else if ([kind isEqualToString:@"description"]) {
        control = WDLabel(@"", frame, 12, [NSColor secondaryLabelColor], NO);
        [(NSTextField *)control setLineBreakMode:NSLineBreakByTruncatingTail];
    } else if ([kind isEqualToString:@"value"]) {
        control = WDLabel(@"", frame, 12, [NSColor labelColor], NO);
        [(NSTextField *)control setLineBreakMode:NSLineBreakByTruncatingMiddle];
    } else if ([kind isEqualToString:@"hint"]) {
        control = WDLabel(@"", frame, 11, [NSColor tertiaryLabelColor], NO);
        [(NSTextField *)control setLineBreakMode:NSLineBreakByTruncatingTail];
    } else if ([kind isEqualToString:@"link"]) {
        DocWebHubLinkButton *link = [[DocWebHubLinkButton alloc] initWithFrame:frame];
        [link setButtonType:NSButtonTypeMomentaryChange];
        [link setBordered:NO];
        [link setAlignment:NSTextAlignmentLeft];
        [link setFocusRingType:NSFocusRingTypeNone];
        control = link;
    } else if ([kind isEqualToString:@"button"]) {
        NSButton *button = [[NSButton alloc] initWithFrame:frame];
        WDStyleButton(button, style, icon);
        control = button;
    } else if ([kind isEqualToString:@"input"] || [kind isEqualToString:@"number"]) {
        NSTextField *text = [[NSTextField alloc] initWithFrame:frame];
        [text setFont:[NSFont systemFontOfSize:12]];
        [text setEditable:YES];
        [text setSelectable:YES];
        control = text;
    } else if ([kind isEqualToString:@"password"]) {
        NSSecureTextField *text = [[NSSecureTextField alloc] initWithFrame:frame];
        [text setFont:[NSFont systemFontOfSize:12]];
        control = text;
    } else if ([kind isEqualToString:@"select"]) {
        NSPopUpButton *popup = [[NSPopUpButton alloc] initWithFrame:frame pullsDown:NO];
        [popup setFont:[NSFont systemFontOfSize:12]];
        control = popup;
    }

    NSInteger index = [docWebHubFields count];
    NSMutableDictionary *record = [NSMutableDictionary dictionary];
    record[@"row"] = @(row);
    record[@"field"] = @(column);
    record[@"id"] = fieldID;
    record[@"kind"] = kind;
    record[@"rowKind"] = rowKind;
    record[@"action"] = action;
    record[@"width"] = width;
    if (control != nil) {
        [control setTag:index];
        record[@"control"] = control;
        [content addSubview:control];
        if (action.length > 0) {
            [control setTarget:controller];
            [control setAction:@selector(formAction:)];
        }
    }
    if (dot != nil) {
        record[@"dot"] = dot;
    }
    [docWebHubFields addObject:record];

    WDApplyField(record, YES);
    return record;
}

static void WDApplyField(NSDictionary *field, BOOL updateValue) {
    int row = [field[@"row"] intValue];
    int column = [field[@"field"] intValue];
    NSString *kind = field[@"kind"];
    NSString *rowKind = field[@"rowKind"];
    NSString *style = WDFieldString(row, column, docwebhub_form_field_style);
    NSString *label = WDFieldString(row, column, docwebhub_form_field_label);
    NSString *value = WDFieldString(row, column, docwebhub_form_field_value);
    NSString *placeholder = WDFieldString(row, column, docwebhub_form_field_placeholder);
    NSString *tooltip = WDFieldString(row, column, docwebhub_form_field_tooltip);
    NSString *accessibility = WDFieldString(row, column, docwebhub_form_field_accessibility);
    id control = field[@"control"];

    if ([kind isEqualToString:@"button"]) {
        NSButton *button = (NSButton *)control;
        WDSetButtonTitle(button, label, style);
        [button setToolTip:tooltip.length > 0 ? tooltip : nil];
    } else if ([kind isEqualToString:@"link"]) {
        NSButton *button = (NSButton *)control;
        WDSetLink(button, value, accessibility);
        [button setToolTip:tooltip.length > 0 ? tooltip : nil];
    } else if ([kind isEqualToString:@"select"]) {
        NSPopUpButton *popup = (NSPopUpButton *)control;
        [popup removeAllItems];
        int count = docwebhub_form_field_option_count(row, column);
        for (int i = 0; i < count; i++) {
            char *rawLabel = docwebhub_form_field_option_label(row, column, i);
            char *rawValue = docwebhub_form_field_option_value(row, column, i);
            NSString *itemLabel = rawLabel ? [NSString stringWithUTF8String:rawLabel] : @"";
            NSString *itemValue = rawValue ? [NSString stringWithUTF8String:rawValue] : @"";
            free(rawLabel);
            free(rawValue);
            [popup addItemWithTitle:itemLabel];
            [[popup lastItem] setRepresentedObject:itemValue];
        }
        for (NSMenuItem *item in [popup itemArray]) {
            if ([[item representedObject] isEqualToString:value]) {
                [popup selectItem:item];
                break;
            }
        }
        [popup setToolTip:tooltip.length > 0 ? tooltip : nil];
    } else if ([kind isEqualToString:@"input"] || [kind isEqualToString:@"number"] ||
               [kind isEqualToString:@"password"]) {
        NSTextField *text = (NSTextField *)control;
        if (updateValue) {
            [text setStringValue:value];
        }
        if (placeholder.length > 0) {
            [text setPlaceholderAttributedString:WDPlaceholder(placeholder)];
        } else {
            [text setPlaceholderString:@""];
        }
        [text setToolTip:tooltip.length > 0 ? tooltip : nil];
    } else if ([kind isEqualToString:@"status"]) {
        NSTextField *text = (NSTextField *)control;
        [text setStringValue:value];
        [text setTextColor:WDTextColor(style, kind, rowKind)];
        NSView *dot = field[@"dot"];
        WDSetStatusDot(dot, style);
    } else if ([kind isEqualToString:@"description"] || [kind isEqualToString:@"hint"] ||
               [kind isEqualToString:@"value"] || [kind isEqualToString:@"heading"]) {
        NSTextField *text = (NSTextField *)control;
        [text setStringValue:([kind isEqualToString:@"hint"] ? label : value)];
        [text setTextColor:WDTextColor(style, kind, rowKind)];
        [text setToolTip:tooltip.length > 0 ? tooltip : nil];
    } else if ([kind isEqualToString:@"stat"]) {
        [(NSTextField *)control setStringValue:value];
    } else if ([kind isEqualToString:@"badge"]) {
        WDApplyBadgeView(control, value, label);
    } else if ([kind isEqualToString:@"device-list"] || [kind isEqualToString:@"file-list"]) {
        NSTextField *text = (NSTextField *)control;
        [text setStringValue:(value.length > 0 ? value : LWStr("ui.overview.empty"))];
    } else if ([kind isEqualToString:@"sparkline"]) {
        WDApplySparkline(control, value);
    }
}

static void WDApplyChanges(void) {
    int count = docwebhub_changed_count();
    for (int i = 0; i < count; i++) {
        char *raw = docwebhub_changed_id(i);
        NSString *fieldID = raw ? [NSString stringWithUTF8String:raw] : @"";
        free(raw);
        NSDictionary *field = WDFieldByID(fieldID);
        if (field != nil) {
            WDApplyField(field, YES);
        }
    }
}

static void WDRefreshMenus(void) {
    int count = docwebhub_form_menu_count();
    for (int i = 0; i < count && i < (int)[docWebHubMenuItems count]; i++) {
        NSMenuItem *item = docWebHubMenuItems[i];
        if ([item isSeparatorItem]) {
            continue;
        }
        [item setTitle:WDMenuString(i, docwebhub_form_menu_label)];
    }

    // Keep the application menu's Quit item in the active language too.
    if (docWebHubAppQuitItem != nil) {
        NSString *quitLabel = WDMenuLabelForAction(@"quit");
        if (quitLabel.length == 0) {
            quitLabel = LWStr("menu.quit");
        }
        [docWebHubAppQuitItem setTitle:quitLabel];
    }
}

static void WDRefreshLabels(void) {
    if (docWebHubWindow != nil) {
        [docWebHubWindow setTitle:WDTitle()];
    }
    WDRefreshTabLabels();

    for (NSDictionary *field in docWebHubFields) {
        WDApplyField(field, NO);
    }
    for (NSDictionary *rowLabel in docWebHubRowLabels) {
        int row = [rowLabel[@"row"] intValue];
        NSTextField *label = rowLabel[@"control"];
        [label setStringValue:WDRowString(row, docwebhub_form_row_label)];
    }
    if (docWebHubInfoWindow != nil) {
        [docWebHubInfoWindow setTitle:LWStr("ui.access.title")];
    }
    WDRefreshMenus();
}

static void WDCollectRowValues(NSDictionary *trigger, NSString **value1, NSString **value2) {
    *value1 = @"";
    *value2 = @"";
    int row = [trigger[@"row"] intValue];
    BOOL first = YES;
    for (NSDictionary *field in docWebHubFields) {
        if ([field[@"row"] intValue] != row) {
            continue;
        }
        NSString *kind = field[@"kind"];
        if (![kind isEqualToString:@"input"] && ![kind isEqualToString:@"number"] &&
            ![kind isEqualToString:@"password"]) {
            continue;
        }
        NSTextField *text = field[@"control"];
        NSString *value = [text stringValue];
        if (first) {
            *value1 = value;
            first = NO;
        } else {
            *value2 = value;
        }
    }
}

static void WDShowAccessInfo(NSString *text) {
    if (docWebHubInfoWindow == nil) {
        NSRect frame = NSMakeRect(0, 0, 640, 440);
        NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
                                                       styleMask:(NSWindowStyleMaskTitled |
                                                                  NSWindowStyleMaskClosable |
                                                                  NSWindowStyleMaskResizable |
                                                                  NSWindowStyleMaskMiniaturizable)
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
        [window setTitle:LWStr("ui.access.title")];
        [window setReleasedWhenClosed:NO];

        NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:frame];
        [scroll setHasVerticalScroller:YES];
        [scroll setHasHorizontalScroller:YES];
        [scroll setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];

        NSTextView *view = [[NSTextView alloc] initWithFrame:frame];
        [view setEditable:NO];
        [view setSelectable:YES];
        [view setRichText:NO];
        [view setFont:[NSFont monospacedSystemFontOfSize:12 weight:NSFontWeightRegular]];
        [view setTextContainerInset:NSMakeSize(14, 12)];
        [view setHorizontallyResizable:YES];
        [view setVerticallyResizable:YES];
        [view setAutoresizingMask:NSViewWidthSizable];
        [[view textContainer] setWidthTracksTextView:NO];
        [[view textContainer] setContainerSize:NSMakeSize(CGFLOAT_MAX, CGFLOAT_MAX)];
        [scroll setDocumentView:view];
        [window setContentView:scroll];
        [window center];

        docWebHubInfoWindow = window;
        docWebHubInfoText = view;
    }

    [docWebHubInfoText setString:text];
    [docWebHubInfoWindow makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

@implementation DocWebHubController

- (void)formAction:(id)sender {
    NSInteger index = [sender tag];
    if (index < 0 || index >= (NSInteger)[docWebHubFields count]) {
        return;
    }
    NSDictionary *field = docWebHubFields[index];
    NSString *action = field[@"action"];
    if (action.length == 0) {
        return;
    }

    if ([action isEqualToString:@"copy-url"]) {
        NSDictionary *link = WDFieldByID(@"url");
        if (link == nil) {
            return;
        }
        int row = [link[@"row"] intValue];
        int column = [link[@"field"] intValue];
        NSString *url = WDFieldString(row, column, docwebhub_form_field_value);
        if (url.length == 0) {
            return;
        }

        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];
        [pasteboard setString:url forType:NSPasteboardTypeString];

        NSButton *copy = field[@"control"];
        [copy setTitle:LWStr("ui.url.copied")];
        NSDictionary *copyField = field;
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.2 * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
                           int copyRow = [copyField[@"row"] intValue];
                           int copyColumn = [copyField[@"field"] intValue];
                           NSString *label = WDFieldString(copyRow, copyColumn,
                                                           docwebhub_form_field_label);
                           [(NSButton *)copyField[@"control"] setTitle:label];
                       });
        return;
    }

    NSString *value1 = @"";
    NSString *value2 = @"";
    if ([action isEqualToString:@"choose-root"]) {
        NSOpenPanel *panel = [NSOpenPanel openPanel];
        [panel setCanChooseFiles:NO];
        [panel setCanChooseDirectories:YES];
        [panel setAllowsMultipleSelection:NO];
        [panel setCanCreateDirectories:YES];
        [panel setPrompt:LWStr("dialog.choose.prompt")];
        [panel setMessage:LWStr("dialog.choose.message")];
        if ([panel runModal] != NSModalResponseOK) {
            return;
        }
        NSURL *url = [[panel URLs] firstObject];
        if (url == nil) {
            return;
        }
        value1 = [url path];
    } else if ([field[@"kind"] isEqualToString:@"select"]) {
        NSPopUpButton *popup = field[@"control"];
        value1 = [[popup selectedItem] representedObject] ?: @"";
    } else {
        WDCollectRowValues(field, &value1, &value2);
    }

    char *error = docwebhub_action([action UTF8String], [value1 UTF8String], [value2 UTF8String]);
    if (error != NULL) {
        NSString *message = [NSString stringWithUTF8String:error];
        free(error);
        char *rawTitle = docwebhub_action_error_title();
        NSString *title = rawTitle != NULL ? [NSString stringWithUTF8String:rawTitle] : @"";
        free(rawTitle);
        WDAlert(title, message);
        WDApplyChanges();
        return;
    }

    if (docwebhub_labels_changed()) {
        WDRefreshLabels();
    }
    WDApplyChanges();

    char *rawText = docwebhub_action_text();
    if (rawText != NULL && rawText[0] != '\0') {
        WDShowAccessInfo([NSString stringWithUTF8String:rawText]);
    }
    free(rawText);
}

- (void)tabChanged:(id)sender {
    NSInteger index = [sender selectedSegment];
    NSString *group = WDGroupAtIndex(index);
    if (group.length > 0) {
        WDRebuild(group);
    }
}

- (void)windowDidResize:(NSNotification *)notification {
    (void)notification;
    WDLayoutChrome();
}

- (void)menuOpenURL:(id)sender {
    docwebhub_action("open-url", "", "");
}

- (void)menuShowWindow:(id)sender {
    if (docWebHubWindow != nil) {
        [docWebHubWindow makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    }
}

- (void)menuQuit:(id)sender {
    [NSApp terminate:nil];
}

@end

static void WDBuildMenu(NSMenu *menu, DocWebHubController *controller) {
    docWebHubMenuItems = [[NSMutableArray alloc] init];
    int count = docwebhub_form_menu_count();
    for (int i = 0; i < count; i++) {
        int separator = docwebhub_form_menu_separator(i);
        if (separator) {
            NSMenuItem *item = [NSMenuItem separatorItem];
            [menu addItem:item];
            [docWebHubMenuItems addObject:item];
            continue;
        }

        NSString *label = WDMenuString(i, docwebhub_form_menu_label);
        NSString *action = WDMenuString(i, docwebhub_form_menu_action);
        SEL selector = @selector(menuOpenURL:);
        if ([action isEqualToString:@"show-window"]) {
            selector = @selector(menuShowWindow:);
        } else if ([action isEqualToString:@"quit"]) {
            selector = @selector(menuQuit:);
        }
        NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:label action:selector keyEquivalent:@""];
        [item setTarget:controller];
        [menu addItem:item];
        [docWebHubMenuItems addObject:item];
    }
}

// ---- Tabs ----------------------------------------------------------------
// The window is four tabs, not one long form: the overview answers "what is
// the server doing", and everything that is only changed occasionally lives
// behind its own tab. Rows tagged "chrome" (the address and the note under it)
// are drawn once in the footer and survive a tab switch.

#define WD_WINDOW_WIDTH 760.0
#define WD_WINDOW_MIN_WIDTH 720.0
#define WD_INSET 28.0
#define WD_TAB_TOP 14.0
#define WD_PAGE_TOP 52.0
#define WD_FOOTER_HEIGHT 78.0

static NSString *const WDGroups[4] = {@"overview", @"settings", @"security", @"language"};
static NSString *const WDTabKeys[4] = {@"ui.tab.overview", @"ui.tab.settings",
                                       @"ui.tab.security", @"ui.tab.language"};

static NSSegmentedControl *WDTabControl = nil;
static NSView *WDPageView = nil;
static NSView *WDFooterView = nil;

// WDIsDark resolves the appearance the window is drawn in. Every colour goes
// through it, so the palette has exactly two definitions.
static BOOL WDIsDark(NSView *view) {
    NSAppearanceName name = [[view effectiveAppearance]
        bestMatchFromAppearancesWithNames:@[ NSAppearanceNameAqua, NSAppearanceNameDarkAqua ]];
    return [name isEqualToString:NSAppearanceNameDarkAqua];
}

// WDAccent is the one brand colour: the orange the website and the GTK window
// already use. The dark variant is lighter so it holds up on a dark surface.
static NSColor *WDAccent(NSView *view) {
    if (WDIsDark(view)) {
        return [NSColor colorWithSRGBRed:1.00 green:0.478 blue:0.239 alpha:1.0];
    }
    return [NSColor colorWithSRGBRed:0.886 green:0.314 blue:0.055 alpha:1.0];
}

static NSColor *WDSectionColor(void) {
    return [NSColor tertiaryLabelColor];
}

// WDWindowBackground is the page behind the cards: a little cooler than the
// system default, which is what makes a white card read as a surface rather
// than as the window itself.
static NSColor *WDWindowBackground(void) {
    return [NSColor colorWithName:@"WDWindowBackground"
                 dynamicProvider:^NSColor *(NSAppearance *appearance) {
                     NSAppearanceName name = [appearance
                         bestMatchFromAppearancesWithNames:@[ NSAppearanceNameAqua,
                                                              NSAppearanceNameDarkAqua ]];
                     if ([name isEqualToString:NSAppearanceNameDarkAqua]) {
                         return [NSColor colorWithSRGBRed:0.086 green:0.106 blue:0.133 alpha:1.0];
                     }
                     return [NSColor colorWithSRGBRed:0.965 green:0.973 blue:0.980 alpha:1.0];
                 }];
}

// WDMinHeightForGroup is the smallest window each tab is allowed to ask for.
// The real height comes from the page that was just built, so a tab with two
// rows does not inherit the overview's height; the floor keeps a one-row page
// from collapsing into a strip.
static CGFloat WDMinHeightForGroup(NSString *group) {
    if ([group isEqualToString:@"overview"]) {
        return 460.0;
    }
    if ([group isEqualToString:@"settings"]) {
        return 380.0;
    }
    if ([group isEqualToString:@"security"]) {
        return 340.0;
    }
    return 330.0;
}

// WDContentHeightForPage turns the height the page reported into a window
// height: page top, the page itself, a bottom margin and the footer.
static CGFloat WDContentHeightForPage(NSString *group, CGFloat pageHeight) {
    CGFloat needed = WD_PAGE_TOP + pageHeight + 26.0 + WD_FOOTER_HEIGHT;
    return MAX(needed, WDMinHeightForGroup(group));
}

// ---- Overview widgets ----------------------------------------------------

// DocWebHubSparkline draws the 24 hourly request counts. A row of NSViews with
// fixed heights would fight the layout, and Core Graphics is already here.
@interface DocWebHubSparkline : NSView
@property (nonatomic, copy) NSArray<NSNumber *> *values;
@end

@implementation DocWebHubSparkline

- (BOOL)isFlipped {
    return YES;
}

- (void)drawRect:(NSRect)dirtyRect {
    (void)dirtyRect;
    NSRect bounds = [self bounds];
    NSUInteger count = [self.values count];
    if (count == 0 || bounds.size.width <= 0 || bounds.size.height <= 0) {
        return;
    }

    NSInteger peak = 0;
    for (NSNumber *value in self.values) {
        if ([value integerValue] > peak) {
            peak = [value integerValue];
        }
    }
    if (peak <= 0) {
        // Nothing has been requested yet: a flat row of marks would claim data
        // that does not exist. Draw the axis instead.
        [[NSColor tertiaryLabelColor] setStroke];
        NSBezierPath *base = [NSBezierPath bezierPath];
        [base moveToPoint:NSMakePoint(0, bounds.size.height - 0.5)];
        [base lineToPoint:NSMakePoint(bounds.size.width, bounds.size.height - 0.5)];
        [base setLineWidth:1.0];
        [base stroke];
        return;
    }

    CGFloat gap = 3.0;
    CGFloat bar = (bounds.size.width - gap * (CGFloat)(count - 1)) / (CGFloat)count;
    if (bar < 2.0) {
        bar = 2.0;
    }
    [WDAccent(self) setFill];
    for (NSUInteger i = 0; i < count; i++) {
        CGFloat height = bounds.size.height * (CGFloat)[self.values[i] doubleValue] / (CGFloat)peak;
        if (height < 3.0) {
            height = 3.0; // an empty hour still shows a mark, so the axis reads
        }
        NSRect barRect = NSMakeRect((CGFloat)i * (bar + gap), bounds.size.height - height, bar, height);
        [[NSBezierPath bezierPathWithRoundedRect:barRect xRadius:1.5 yRadius:1.5] fill];
    }
}

- (void)setValues:(NSArray<NSNumber *> *)values {
    _values = [values copy];
    [self setNeedsDisplay:YES];
}

@end

// DocWebHubBadge is one pill: a bold number and a quiet caption on a tinted
// capsule, the same shape the website uses for the three headline figures.
@interface DocWebHubBadge : NSView
@property (nonatomic, strong) NSTextField *text;
- (instancetype)initWithFrame:(NSRect)frame value:(NSString *)value caption:(NSString *)caption;
- (void)setValue:(NSString *)value caption:(NSString *)caption;
@end

@implementation DocWebHubBadge

- (instancetype)initWithFrame:(NSRect)frame value:(NSString *)value caption:(NSString *)caption {
    self = [super initWithFrame:frame];
    if (self != nil) {
        [self setWantsLayer:YES];
        _text = WDLabel(@"", NSMakeRect(12, 4, 10, 16), 11.5, [NSColor secondaryLabelColor], NO);
        [_text setLineBreakMode:NSLineBreakByClipping];
        [self addSubview:_text];
        [self setValue:value caption:caption];
        [self docwebhub_applyColors];
    }
    return self;
}

// setValue lays the capsule out around its own text. The width the builder
// reads afterwards is the width the text needs, so a longer translation can
// never be clipped.
- (void)sizeToText {
    [self.text sizeToFit];
    NSRect textFrame = [self.text frame];
    CGFloat width = ceil(NSWidth(textFrame)) + 24;
    NSRect frame = [self frame];
    frame.size.width = width;
    [self setFrame:frame];
    textFrame.origin = NSMakePoint(12, (NSHeight(frame) - NSHeight(textFrame)) / 2.0);
    [self.text setFrame:textFrame];
    [[self layer] setCornerRadius:NSHeight(frame) / 2.0];
}

- (BOOL)isFlipped {
    return YES;
}

- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    [self docwebhub_applyColors];
}

- (void)docwebhub_applyColors {
    NSColor *fill = WDIsDark(self) ? [NSColor colorWithWhite:1.0 alpha:0.07]
                                   : [NSColor colorWithWhite:0.0 alpha:0.05];
    CALayer *layer = [self layer];
    [layer setBackgroundColor:[fill CGColor]];
    [layer setCornerRadius:NSHeight([self bounds]) / 2.0];
}

- (void)setValue:(NSString *)value caption:(NSString *)caption {
    NSString *joined = caption.length > 0 ? [NSString stringWithFormat:@"%@  %@", value, caption] : value;
    NSMutableAttributedString *text = [[NSMutableAttributedString alloc]
        initWithString:joined
            attributes:@{
                NSFontAttributeName : [NSFont systemFontOfSize:11.5],
                NSForegroundColorAttributeName : [NSColor secondaryLabelColor],
            }];
    NSUInteger valueLength = MIN([value length], [joined length]);
    if (valueLength > 0) {
        [text addAttribute:NSFontAttributeName
                     value:[NSFont systemFontOfSize:12.5 weight:NSFontWeightSemibold]
                     range:NSMakeRange(0, valueLength)];
        [text addAttribute:NSForegroundColorAttributeName
                     value:[NSColor labelColor]
                     range:NSMakeRange(0, valueLength)];
    }
    [self.text setAttributedStringValue:text];
    [self sizeToText];
}

@end

// The footer carries the address and the "keeps running" note. It sits on a
// tinted band with a hairline on top so the content above reads as one pane.
@interface DocWebHubFooterView : NSView
@end

@implementation DocWebHubFooterView

- (BOOL)isFlipped {
    return YES;
}

- (instancetype)initWithFrame:(NSRect)frame {
    self = [super initWithFrame:frame];
    if (self != nil) {
        [self setWantsLayer:YES];
        [self docwebhub_applyColors];
    }
    return self;
}

- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    [self docwebhub_applyColors];
}

- (void)docwebhub_applyColors {
    NSColor *fill = WDIsDark(self) ? [NSColor colorWithWhite:1.0 alpha:0.03]
                                   : [NSColor colorWithWhite:0.0 alpha:0.025];
    [[self layer] setBackgroundColor:[fill CGColor]];
    [self setNeedsDisplay:YES];
}

- (void)drawRect:(NSRect)dirtyRect {
    (void)dirtyRect;
    NSColor *line = WDIsDark(self) ? [NSColor colorWithWhite:1.0 alpha:0.10]
                                   : [NSColor colorWithWhite:0.0 alpha:0.08];
    [line setFill];
    NSRectFill(NSMakeRect(0, 0, NSWidth([self bounds]), 1));
}

@end

// WDCreateBadge starts narrow and lets the view size itself from its text.
static DocWebHubBadge *WDCreateBadge(NSString *value, NSString *caption, NSPoint origin, CGFloat height) {
    DocWebHubBadge *badge = [[DocWebHubBadge alloc] initWithFrame:NSMakeRect(origin.x, origin.y, 10, height)
                                                           value:value
                                                         caption:caption];
    return badge;
}

// WDParseValues turns the comma separated hour counts into numbers. Go formats
// them; the renderer never computes anything.
static NSArray<NSNumber *> *WDParseValues(NSString *text) {
    if (text.length == 0) {
        return @[];
    }
    NSMutableArray<NSNumber *> *values = [NSMutableArray array];
    for (NSString *part in [text componentsSeparatedByString:@","]) {
        if ([part length] == 0) {
            continue;
        }
        [values addObject:@([part integerValue])];
        if ([values count] >= 24) {
            break;
        }
    }
    return values;
}

// The controller and WDApplyField only see these through id, so the class
// methods stay out of their translation unit.
static void WDApplyBadgeView(id badge, NSString *value, NSString *caption) {
    [(DocWebHubBadge *)badge setValue:value caption:caption];
}

static void WDApplySparkline(id spark, NSString *value) {
    [(DocWebHubSparkline *)spark setValues:WDParseValues(value)];
}

// The tab names live in an NSString table, so they cannot go through LWStr,
// which takes the C string keys the form uses.
static NSString *WDTextKey(NSString *key) {
    char *raw = docwebhub_text([key UTF8String]);
    NSString *text = raw != NULL ? [NSString stringWithUTF8String:raw] : @"";
    free(raw);
    return text;
}

static void WDRefreshTabLabels(void) {
    for (int i = 0; i < 4 && WDTabControl != nil; i++) {
        [WDTabControl setLabel:WDTextKey(WDTabKeys[i]) forSegment:i];
    }
}

static NSString *WDGroupAtIndex(NSInteger index) {
    if (index < 0 || index >= 4) {
        return @"";
    }
    return WDGroups[index];
}

static NSInteger WDRowWithKind(NSString *group, NSString *kind) {
    int count = docwebhub_form_row_count();
    for (int row = 0; row < count; row++) {
        NSString *rowGroup = WDRowString(row, docwebhub_form_row_group);
        if (![rowGroup isEqualToString:group]) {
            continue;
        }
        NSString *rowKind = WDRowString(row, docwebhub_form_row_kind);
        if ([rowKind isEqualToString:kind]) {
            return row;
        }
    }
    return -1;
}

// WDRegisterView records a hand-built widget so the refresh path can find it
// the same way it finds a text field created from the form.
static void WDRegisterView(int row, int column, NSString *kind, id control) {
    NSMutableDictionary *record = [NSMutableDictionary dictionary];
    record[@"row"] = @(row);
    record[@"field"] = @(column);
    record[@"id"] = WDFieldString(row, column, docwebhub_form_field_id);
    record[@"kind"] = kind;
    record[@"rowKind"] = @"overview";
    record[@"control"] = control;
    if ([control isKindOfClass:[NSControl class]]) {
        [(NSControl *)control setTag:(NSInteger)[docWebHubFields count]];
    }
    [docWebHubFields addObject:record];
}

// ---- Overview page -------------------------------------------------------

// WDLineCount counts the lines a multi-line value will need, so the frame is
// tall enough rather than clipped.
static CGFloat WDTextHeight(NSString *text) {
    NSInteger lines = 0;
    for (NSString *line in [text componentsSeparatedByString:@"\n"]) {
        if ([line length] > 0) {
            lines++;
        }
    }
    if (lines == 0) {
        lines = 1;
    }
    return MAX(18.0, (CGFloat)lines * 18.0);
}

static NSTextField *WDSectionTitle(NSView *parent, NSString *title, NSPoint origin) {
    NSTextField *label = WDLabel(title, NSMakeRect(origin.x, origin.y, 320, 15), 11, WDSectionColor(), NO);
    [label setLineBreakMode:NSLineBreakByClipping];
    [parent addSubview:label];
    return label;
}

static NSTextField *WDMultilineBody(NSView *parent, NSString *body, NSString *empty,
                                    NSRect frame, CGFloat size, NSColor *color) {
    NSString *shown = body.length > 0 ? body : empty;
    NSTextField *label = WDLabel(shown, frame, size, color, NO);
    [label setLineBreakMode:NSLineBreakByWordWrapping];
    [label setMaximumNumberOfLines:0];
    [parent addSubview:label];
    return label;
}

static CGFloat WDBuildOverview(NSView *page, CGFloat width, DocWebHubController *controller) {
    NSString *empty = LWStr("ui.overview.empty");
    CGFloat y = 0;

    NSInteger headerRow = WDRowWithKind(@"overview", @"header");
    if (headerRow >= 0) {
        WDCreateField((int)headerRow, 0, @"header", NSMakeRect(0, 0, width, 17), controller, page);
        WDCreateField((int)headerRow, 1, @"header", NSMakeRect(0, 20, width, 30), controller, page);
        WDCreateField((int)headerRow, 2, @"header", NSMakeRect(0, 54, width, 17), controller, page);
        y = 92;
    }

    NSInteger statsRow = WDRowWithKind(@"overview", @"stat");
    if (statsRow >= 0) {
        int count = docwebhub_form_field_count((int)statsRow);
        CGFloat x = 0;
        for (int column = 0; column < count; column++) {
            NSString *value = WDFieldString((int)statsRow, column, docwebhub_form_field_value);
            NSString *caption = WDFieldString((int)statsRow, column, docwebhub_form_field_label);
            NSTextField *big = WDLabel(value, NSMakeRect(x, y, 220, 38), 30, [NSColor labelColor], NO);
            [big setFont:[NSFont systemFontOfSize:30 weight:NSFontWeightLight]];
            [big setLineBreakMode:NSLineBreakByTruncatingTail];
            [page addSubview:big];
            NSTextField *small = WDLabel(caption, NSMakeRect(x, y + 40, 220, 15), 11,
                                         [NSColor secondaryLabelColor], NO);
            [page addSubview:small];
            WDRegisterView((int)statsRow, column, @"stat", big);
            x += 220;
        }
        y += 66;
    }

    NSInteger badgeRow = WDRowWithKind(@"overview", @"badge");
    if (badgeRow >= 0) {
        int count = docwebhub_form_field_count((int)badgeRow);
        CGFloat x = 0;
        for (int column = 0; column < count; column++) {
            NSString *value = WDFieldString((int)badgeRow, column, docwebhub_form_field_value);
            NSString *caption = WDFieldString((int)badgeRow, column, docwebhub_form_field_label);
            DocWebHubBadge *badge = WDCreateBadge(value, caption, NSMakePoint(x, y), 24);
            [page addSubview:badge];
            WDRegisterView((int)badgeRow, column, @"badge", badge);
            x += NSWidth([badge frame]) + 8;
        }
        y += 24 + 22;
    }

    // Two columns: devices on the left, the request chart and the recent files
    // on the right, the way the website lays the same data out.
    CGFloat leftWidth = 280.0;
    CGFloat rightX = leftWidth + 32.0;
    CGFloat rightWidth = width - rightX;
    CGFloat columnTop = y;
    CGFloat leftHeight = 0;
    CGFloat rightHeight = 0;

    NSInteger devicesRow = WDRowWithKind(@"overview", @"device-list");
    if (devicesRow >= 0) {
        NSString *title = WDFieldString((int)devicesRow, 0, docwebhub_form_field_label);
        NSString *body = WDFieldString((int)devicesRow, 0, docwebhub_form_field_value);
        WDSectionTitle(page, title, NSMakePoint(0, columnTop));
        CGFloat bodyHeight = WDTextHeight(body.length > 0 ? body : empty);
        NSTextField *label = WDMultilineBody(page, body, empty,
                                             NSMakeRect(0, columnTop + 20, leftWidth, bodyHeight),
                                             13, [NSColor labelColor]);
        WDRegisterView((int)devicesRow, 0, @"device-list", label);
        leftHeight = 20 + bodyHeight;
    }

    NSInteger hourlyRow = WDRowWithKind(@"overview", @"sparkline");
    if (hourlyRow >= 0) {
        NSString *title = WDFieldString((int)hourlyRow, 0, docwebhub_form_field_label);
        NSString *value = WDFieldString((int)hourlyRow, 0, docwebhub_form_field_value);
        WDSectionTitle(page, title, NSMakePoint(rightX, columnTop));
        DocWebHubSparkline *spark =
            [[DocWebHubSparkline alloc] initWithFrame:NSMakeRect(rightX, columnTop + 20, rightWidth, 48)];
        [spark setValues:WDParseValues(value)];
        [page addSubview:spark];
        WDRegisterView((int)hourlyRow, 0, @"sparkline", spark);
        rightHeight = 20 + 48;
    }

    NSInteger recentRow = WDRowWithKind(@"overview", @"file-list");
    if (recentRow >= 0) {
        CGFloat recentY = columnTop + rightHeight + 20;
        NSString *title = WDFieldString((int)recentRow, 0, docwebhub_form_field_label);
        NSString *body = WDFieldString((int)recentRow, 0, docwebhub_form_field_value);
        WDSectionTitle(page, title, NSMakePoint(rightX, recentY));
        CGFloat bodyHeight = WDTextHeight(body.length > 0 ? body : empty);
        NSTextField *label = WDMultilineBody(page, body, empty,
                                             NSMakeRect(rightX, recentY + 20, rightWidth, bodyHeight),
                                             13, [NSColor secondaryLabelColor]);
        WDRegisterView((int)recentRow, 0, @"file-list", label);
        rightHeight += 20 + 20 + bodyHeight;
    }

    return columnTop + MAX(leftHeight, rightHeight);
}

// ---- Settings, security and language pages -------------------------------

static CGFloat WDBuildSettingRows(NSView *page, CGFloat width, NSString *group,
                                  DocWebHubController *controller) {
    int rowCount = docwebhub_form_row_count();
    int settingCount = 0;
    for (int row = 0; row < rowCount; row++) {
        NSString *rowGroup = WDRowString(row, docwebhub_form_row_group);
        if (![rowGroup isEqualToString:group]) {
            continue;
        }
        NSString *kind = WDRowString(row, docwebhub_form_row_kind);
        if ([kind isEqualToString:@"setting"]) {
            settingCount++;
        }
    }
    if (settingCount == 0) {
        return 0;
    }

    CGFloat rowHeight = 44.0;
    CGFloat cardHeight = 20.0 + settingCount * rowHeight;
    DocWebHubCard *card = [[DocWebHubCard alloc] initWithFrame:NSMakeRect(0, 0, width, cardHeight)];
    [page addSubview:card];

    int settingIndex = 0;
    for (int row = 0; row < rowCount; row++) {
        NSString *rowGroup = WDRowString(row, docwebhub_form_row_group);
        if (![rowGroup isEqualToString:group]) {
            continue;
        }
        NSString *rowKind = WDRowString(row, docwebhub_form_row_kind);
        if (![rowKind isEqualToString:@"setting"]) {
            continue;
        }
        NSString *rowLabel = WDRowString(row, docwebhub_form_row_label);
        int fieldCount = docwebhub_form_field_count(row);
        CGFloat rowY = 10.0 + settingIndex * rowHeight;

        if (rowLabel.length > 0) {
            NSTextField *label = WDLabel(rowLabel, NSMakeRect(18, rowY + 12, 96, 16), 12,
                                         [NSColor secondaryLabelColor], NO);
            [label setLineBreakMode:NSLineBreakByTruncatingTail];
            [page addSubview:label];
            [docWebHubRowLabels addObject:@{@"row" : @(row), @"control" : label}];
        }

        NSMutableArray *fields = [NSMutableArray array];
        for (int column = 0; column < fieldCount; column++) {
            [fields addObject:@{
                @"kind" : WDFieldKind(row, column),
                @"width" : @(docwebhub_form_field_width(row, column)),
            }];
        }
        CGFloat controlX = rowLabel.length > 0 ? 124.0 : 18.0;
        NSArray *widths = WDWidths(fields, width - 18 - controlX, 12);
        CGFloat x = controlX;
        for (int column = 0; column < fieldCount; column++) {
            CGFloat fieldWidth = [widths[column] doubleValue];
            NSString *kind = WDFieldKind(row, column);
            CGFloat height = WDFieldHeight(kind);
            CGFloat fieldY = rowY + (rowHeight - height) / 2.0;
            if ([kind isEqualToString:@"spacer"]) {
                x += fieldWidth + 12;
                continue;
            }
            WDCreateField(row, column, rowKind,
                          NSMakeRect(x, fieldY, fieldWidth, height), controller, page);
            x += fieldWidth + 12;
        }
        settingIndex++;
    }
    return cardHeight;
}

static CGFloat WDBuildLanguageRow(NSView *page, CGFloat width, DocWebHubController *controller) {
    NSInteger row = WDRowWithKind(@"language", @"footer");
    if (row < 0) {
        return 0;
    }
    NSString *rowLabel = WDRowString((int)row, docwebhub_form_row_label);
    int fieldCount = docwebhub_form_field_count((int)row);
    if (rowLabel.length > 0) {
        NSTextField *label = WDLabel(rowLabel, NSMakeRect(0, 6, 200, 18), 13,
                                     [NSColor secondaryLabelColor], NO);
        [page addSubview:label];
        [docWebHubRowLabels addObject:@{@"row" : @(row), @"control" : label}];
    }
    CGFloat x = 104;
    for (int column = 0; column < fieldCount; column++) {
        NSString *kind = WDFieldKind((int)row, column);
        if ([kind isEqualToString:@"spacer"]) {
            continue;
        }
        CGFloat fieldWidth = docwebhub_form_field_width((int)row, column);
        if (fieldWidth <= 0) {
            fieldWidth = width - x;
        }
        CGFloat height = WDFieldHeight(kind);
        WDCreateField((int)row, column, @"footer", NSMakeRect(x, 0, fieldWidth, height), controller, page);
        x += fieldWidth + 12;
    }
    return 32;
}

// ---- Chrome --------------------------------------------------------------

static CGFloat WDBuildChrome(NSView *footer, CGFloat width, DocWebHubController *controller) {
    int rowCount = docwebhub_form_row_count();
    CGFloat y = 14;
    for (int row = 0; row < rowCount; row++) {
        NSString *group = WDRowString(row, docwebhub_form_row_group);
        if (![group isEqualToString:@"chrome"]) {
            continue;
        }
        NSString *kind = WDRowString(row, docwebhub_form_row_kind);
        int fieldCount = docwebhub_form_field_count(row);

        if ([kind isEqualToString:@"url"]) {
            NSMutableArray *fields = [NSMutableArray array];
            for (int column = 0; column < fieldCount; column++) {
                [fields addObject:@{
                    @"kind" : WDFieldKind(row, column),
                    @"width" : @(docwebhub_form_field_width(row, column)),
                }];
            }
            NSArray *widths = WDWidths(fields, width - 2 * WD_INSET, 10);
            CGFloat x = WD_INSET;
            for (int column = 0; column < fieldCount; column++) {
                CGFloat fieldWidth = [widths[column] doubleValue];
                CGFloat height = WDFieldHeight(WDFieldKind(row, column));
                WDCreateField(row, column, kind,
                              NSMakeRect(x, y + (26 - height) / 2, fieldWidth, height), controller, footer);
                x += fieldWidth + 10;
            }
            y += 30;
        } else if ([kind isEqualToString:@"hint"]) {
            for (int column = 0; column < fieldCount; column++) {
                WDCreateField(row, column, kind,
                              NSMakeRect(WD_INSET, y, width - 2 * WD_INSET, 16), controller, footer);
            }
        }
    }
    return y;
}

// ---- Layout and rebuild --------------------------------------------------

static void WDLayoutChrome(void) {
    if (docWebHubWindow == nil) {
        return;
    }
    NSRect bounds = [[docWebHubWindow contentView] bounds];
    CGFloat width = bounds.size.width;
    CGFloat height = bounds.size.height;
    [WDTabControl setFrame:NSMakeRect(WD_INSET, WD_TAB_TOP, 340, 26)];
    [WDPageView setFrame:NSMakeRect(WD_INSET, WD_PAGE_TOP, width - 2 * WD_INSET,
                                    height - WD_PAGE_TOP - WD_FOOTER_HEIGHT)];
    [WDFooterView setFrame:NSMakeRect(0, height - WD_FOOTER_HEIGHT, width, WD_FOOTER_HEIGHT)];
}

static void WDSetContentHeight(CGFloat height) {
    if (docWebHubWindow == nil) {
        return;
    }
    NSRect frame = [docWebHubWindow frame];
    NSRect content = [[docWebHubWindow contentView] frame];
    CGFloat chrome = frame.size.height - content.size.height;
    CGFloat top = NSMaxY(frame);
    frame.size.height = height + chrome;
    frame.origin.y = top - frame.size.height;
    [docWebHubWindow setFrame:frame display:YES animate:NO];
}

// WDRebuild replaces the page for one group. Only the visible page exists, so a
// short tab does not inherit the overview's height, and the widgets always
// match the current language.
static void WDRebuild(NSString *group) {
    if (WDPageView == nil) {
        return;
    }

    for (NSView *view in [WDPageView subviews]) {
        [view removeFromSuperview];
    }
    for (NSView *view in [WDFooterView subviews]) {
        [view removeFromSuperview];
    }
    [docWebHubFields removeAllObjects];
    [docWebHubRowLabels removeAllObjects];

    CGFloat width = NSWidth([WDPageView bounds]);
    CGFloat height = 0;
    if ([group isEqualToString:@"overview"]) {
        height = WDBuildOverview(WDPageView, width, nil);
    } else if ([group isEqualToString:@"language"]) {
        height = WDBuildLanguageRow(WDPageView, width, nil);
    } else {
        height = WDBuildSettingRows(WDPageView, width, group, nil);
    }

    WDBuildChrome(WDFooterView, NSWidth([WDFooterView bounds]), nil);
    WDSetContentHeight(WDContentHeightForPage(group, height));
    WDLayoutChrome();
    WDRefreshLabels();
}

void docwebhub_run_gui(void) {
    @autoreleasepool {
        docWebHubFields = [[NSMutableArray alloc] init];
        docWebHubRowLabels = [[NSMutableArray alloc] init];

        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];

        DocWebHubAppDelegate *delegate = [[DocWebHubAppDelegate alloc] init];
        [app setDelegate:delegate];


        DocWebHubController *controller = [[DocWebHubController alloc] init];

        NSMenu *mainMenu = [[NSMenu alloc] init];
        NSMenuItem *appMenuItem = [[NSMenuItem alloc] init];
        [mainMenu addItem:appMenuItem];
        [app setMainMenu:mainMenu];

        NSMenu *appMenu = [[NSMenu alloc] initWithTitle:WDTitle()];
        NSString *quitLabel = WDMenuLabelForAction(@"quit");
        if (quitLabel.length == 0) {
            quitLabel = LWStr("menu.quit");
        }
        docWebHubAppQuitItem =
            [[NSMenuItem alloc] initWithTitle:quitLabel
                                       action:@selector(terminate:)
                                keyEquivalent:@"q"];
        [appMenu addItem:docWebHubAppQuitItem];
        [appMenuItem setSubmenu:appMenu];

        NSRect contentRect = NSMakeRect(0, 0, WD_WINDOW_WIDTH, WDMinHeightForGroup(@"overview"));
        NSWindow *window = [[NSWindow alloc] initWithContentRect:contentRect
                                                       styleMask:(NSWindowStyleMaskTitled |
                                                                  NSWindowStyleMaskClosable |
                                                                  NSWindowStyleMaskMiniaturizable |
                                                                  NSWindowStyleMaskResizable)
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
        [window setTitle:WDTitle()];
        [window setBackgroundColor:WDWindowBackground()];
        [window setReleasedWhenClosed:NO];
        [window setMinSize:NSMakeSize(WD_WINDOW_MIN_WIDTH, 380)];
        [window setDelegate:(id<NSWindowDelegate>)controller];
        [window center];
        docWebHubWindow = window;

        DocWebHubFlippedView *content =
            [[DocWebHubFlippedView alloc] initWithFrame:NSMakeRect(0, 0, WD_WINDOW_WIDTH,
                                                                   WDMinHeightForGroup(@"overview"))];
        [content setAutoresizesSubviews:NO];
        [window setContentView:content];

        WDTabControl = [[NSSegmentedControl alloc] initWithFrame:NSMakeRect(WD_INSET, WD_TAB_TOP, 340, 26)];
        [WDTabControl setSegmentCount:4];
        [WDTabControl setSegmentStyle:NSSegmentStyleRounded];
        [WDTabControl setTrackingMode:NSSegmentSwitchTrackingSelectOne];
        [WDTabControl setFont:[NSFont systemFontOfSize:12.5 weight:NSFontWeightMedium]];
        [WDTabControl setSelectedSegment:0];
        [WDTabControl setTarget:controller];
        [WDTabControl setAction:@selector(tabChanged:)];
        [content addSubview:WDTabControl];

        WDPageView = [[DocWebHubFlippedView alloc] initWithFrame:NSMakeRect(WD_INSET, WD_PAGE_TOP, 600, 400)];
        [content addSubview:WDPageView];

        WDFooterView = [[DocWebHubFooterView alloc] initWithFrame:NSMakeRect(0, 400, WD_WINDOW_WIDTH,
                                                                             WD_FOOTER_HEIGHT)];
        [content addSubview:WDFooterView];

        WDLayoutChrome();
        WDRebuild(@"overview");

        // The overview keeps itself current: two seconds is fast enough that a
        // colleague opening a page shows up while the window is still open, and
        // cheap enough to run forever.
        [NSTimer scheduledTimerWithTimeInterval:2.0
                                        repeats:YES
                                          block:^(NSTimer *timer) {
                                              (void)timer;
                                              if (docWebHubWindow == nil || ![docWebHubWindow isVisible]) {
                                                  return;
                                              }
                                              docwebhub_form_refresh_stats();
                                              for (NSDictionary *field in [docWebHubFields copy]) {
                                                  NSString *kind = field[@"kind"];
                                                  if ([kind isEqualToString:@"stat"] ||
                                                      [kind isEqualToString:@"badge"] ||
                                                      [kind isEqualToString:@"device-list"] ||
                                                      [kind isEqualToString:@"file-list"] ||
                                                      [kind isEqualToString:@"sparkline"]) {
                                                      WDApplyField(field, YES);
                                                  }
                                              }
                                          }];

        // Menu bar icon: after the window is closed the user can restore it or quit from here.
        docWebHubStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        NSImage *statusImage = nil;
        BOOL statusIsTemplate = NO;
        if ([NSImage respondsToSelector:@selector(imageWithSystemSymbolName:accessibilityDescription:)]) {
            statusImage = [NSImage imageWithSystemSymbolName:@"globe" accessibilityDescription:WDTitle()];
            statusIsTemplate = (statusImage != nil);
        }
        if (statusImage == nil) {
            statusImage = [NSImage imageNamed:NSImageNameApplicationIcon];
            [statusImage setSize:NSMakeSize(18, 18)];
        }
        if (statusIsTemplate) {
            [statusImage setTemplate:YES];
        }
        [[docWebHubStatusItem button] setImage:statusImage];
        [[docWebHubStatusItem button] setToolTip:WDTitle()];

        NSMenu *statusMenu = [[NSMenu alloc] init];
        WDBuildMenu(statusMenu, controller);
        [docWebHubStatusItem setMenu:statusMenu];

        [window makeKeyAndOrderFront:nil];
        // Nothing should start focused: the port field would otherwise show a
        // focus ring before the user has touched anything.
        [window makeFirstResponder:nil];
        [app run];
    }
}
