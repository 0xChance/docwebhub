
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
    NSColor *fill = dark ? [NSColor colorWithWhite:1.0 alpha:0.06] : [NSColor colorWithWhite:0.0 alpha:0.04];
    NSColor *edge = dark ? [NSColor colorWithWhite:1.0 alpha:0.10] : [NSColor colorWithWhite:0.0 alpha:0.08];
    CALayer *layer = [self layer];
    [layer setBackgroundColor:[fill CGColor]];
    [layer setBorderColor:[edge CGColor]];
    [layer setBorderWidth:1.0];
    [layer setCornerRadius:8.0];
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
    NSControl *control = field[@"control"];

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

        // The settings form is one description rendered natively. Its rows are
        // laid out top-down; the card is drawn behind the setting rows.
        int rowCount = docwebhub_form_row_count();
        int settingCount = 0;
        for (int row = 0; row < rowCount; row++) {
            NSString *kind = WDRowString(row, docwebhub_form_row_kind);
            if ([kind isEqualToString:@"setting"]) {
                settingCount++;
            }
        }

        CGFloat headerY = 24;
        CGFloat urlY = 130;
        CGFloat cardY = 174;
        CGFloat cardHeight = 32 + settingCount * 36;
        CGFloat hintY = cardY + cardHeight + 22;
        CGFloat footerY = hintY + 32;
        CGFloat contentHeight = MAX(500, footerY + 60);

        NSRect contentRect = NSMakeRect(0, 0, 600, contentHeight);
        NSWindow *window = [[NSWindow alloc] initWithContentRect:contentRect
                                                       styleMask:(NSWindowStyleMaskTitled |
                                                                  NSWindowStyleMaskClosable |
                                                                  NSWindowStyleMaskMiniaturizable)
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
        [window setTitle:WDTitle()];
        [window setReleasedWhenClosed:NO];
        [window center];
        docWebHubWindow = window;

        DocWebHubFlippedView *content =
            [[DocWebHubFlippedView alloc] initWithFrame:NSMakeRect(0, 0, 600, contentHeight)];
        [window setContentView:content];

        if (settingCount > 0) {
            DocWebHubCard *card =
                [[DocWebHubCard alloc] initWithFrame:NSMakeRect(24, cardY, 552, cardHeight)];
            [content addSubview:card];
        }

        int settingIndex = 0;
        for (int row = 0; row < rowCount; row++) {
            NSString *rowKind = WDRowString(row, docwebhub_form_row_kind);
            NSString *rowLabel = WDRowString(row, docwebhub_form_row_label);
            int fieldCount = docwebhub_form_field_count(row);

            if ([rowKind isEqualToString:@"header"]) {
                for (int column = 0; column < fieldCount; column++) {
                    NSString *kind = WDFieldKind(row, column);
                    CGFloat y = headerY;
                    CGFloat height = WDFieldHeight(kind);
                    if ([kind isEqualToString:@"heading"]) {
                        y = headerY + 32;
                    } else if ([kind isEqualToString:@"description"]) {
                        y = headerY + 70;
                    }
                    WDCreateField(row, column, rowKind, NSMakeRect(24, y, 552, height),
                                  controller, content);
                }
                continue;
            }

            if ([rowKind isEqualToString:@"url"]) {
                NSMutableArray *fields = [NSMutableArray array];
                for (int column = 0; column < fieldCount; column++) {
                    NSString *kind = WDFieldKind(row, column);
                    [fields addObject:@{
                        @"kind": kind,
                        @"width": @(docwebhub_form_field_width(row, column)),
                    }];
                }
                NSArray *widths = WDWidths(fields, 552, 10);
                CGFloat x = 24;
                for (int column = 0; column < fieldCount; column++) {
                    CGFloat width = [widths[column] doubleValue];
                    CGFloat height = WDFieldHeight(WDFieldKind(row, column));
                    CGFloat y = urlY + (30 - height) / 2;
                    WDCreateField(row, column, rowKind, NSMakeRect(x, y, width, height),
                                  controller, content);
                    x += width + 10;
                }
                continue;
            }

            if ([rowKind isEqualToString:@"setting"]) {
                CGFloat rowY = cardY + 16 + settingIndex * 36;
                if (rowLabel.length > 0) {
                    NSTextField *label =
                        WDLabel(rowLabel, NSMakeRect(40, rowY + 9, 76, 18), 12,
                                [NSColor secondaryLabelColor], NO);
                    [label setLineBreakMode:NSLineBreakByClipping];
                    [content addSubview:label];
                    [docWebHubRowLabels addObject:@{@"row": @(row), @"control": label}];
                }

                NSMutableArray *fields = [NSMutableArray array];
                for (int column = 0; column < fieldCount; column++) {
                    NSString *kind = WDFieldKind(row, column);
                    [fields addObject:@{
                        @"kind": kind,
                        @"width": @(docwebhub_form_field_width(row, column)),
                    }];
                }
                NSArray *widths = WDWidths(fields, 428, 12);
                CGFloat x = 132;
                for (int column = 0; column < fieldCount; column++) {
                    CGFloat width = [widths[column] doubleValue];
                    NSString *kind = WDFieldKind(row, column);
                    CGFloat height = WDFieldHeight(kind);
                    CGFloat y = rowY + (36 - height) / 2;
                    if ([kind isEqualToString:@"spacer"]) {
                        x += width + 12;
                        continue;
                    }
                    WDCreateField(row, column, rowKind, NSMakeRect(x, y, width, height),
                                  controller, content);
                    x += width + 12;
                }
                settingIndex++;
                continue;
            }

            if ([rowKind isEqualToString:@"hint"]) {
                CGFloat x = 24;
                for (int column = 0; column < fieldCount; column++) {
                    CGFloat width = 552;
                    WDCreateField(row, column, rowKind, NSMakeRect(x, hintY, width, 16),
                                  controller, content);
                }
                continue;
            }

            if ([rowKind isEqualToString:@"footer"]) {
                if (rowLabel.length > 0) {
                    NSTextField *label =
                        WDLabel(rowLabel, NSMakeRect(24, footerY + 6, 76, 18), 12,
                                [NSColor secondaryLabelColor], NO);
                    [label setLineBreakMode:NSLineBreakByTruncatingTail];
                    [content addSubview:label];
                    [docWebHubRowLabels addObject:@{@"row": @(row), @"control": label}];
                }

                NSMutableArray *fields = [NSMutableArray array];
                for (int column = 0; column < fieldCount; column++) {
                    [fields addObject:@{
                        @"kind": WDFieldKind(row, column),
                        @"width": @(docwebhub_form_field_width(row, column)),
                    }];
                }
                NSArray *widths = WDWidths(fields, 472, 12);
                CGFloat x = 104;
                for (int column = 0; column < fieldCount; column++) {
                    CGFloat width = [widths[column] doubleValue];
                    NSString *kind = WDFieldKind(row, column);
                    if ([kind isEqualToString:@"spacer"]) {
                        x += width + 12;
                        continue;
                    }
                    CGFloat height = WDFieldHeight(kind);
                    CGFloat y = footerY + (30 - height) / 2;
                    WDCreateField(row, column, rowKind, NSMakeRect(x, y, width, height),
                                  controller, content);
                    x += width + 12;
                }
            }
        }

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

        WDRefreshLabels();

        [window makeKeyAndOrderFront:nil];
        // Nothing should start focused: the port field would otherwise show a
        // focus ring before the user has touched anything.
        [window makeFirstResponder:nil];
        [app run];
    }
}
