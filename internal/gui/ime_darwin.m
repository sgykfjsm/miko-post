//go:build darwin && !ci

#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include <stdint.h>

// GLFW answers an input method's "where is the caret?" question with the
// origin of the whole view, so the candidate window opens at the window's
// top-left corner, and it keeps the text being composed to itself, so nothing
// shows until the conversion is confirmed. GLFW has no API for either, so
// GLFWContentView's text-input methods are replaced with ones that report the
// caret rectangle Go last stored and tell Go what is being composed.
//
// The caret store is a plain struct behind a lock, written from the UI
// goroutine and read by AppKit. The composition callback runs synchronously on
// the main thread inside the key event Go is already dispatching (the same
// place GLFW's own key and character callbacks run), never from a block that
// outlives it, and the text is copied before it returns.
// cursor is the caret offset inside text in UTF-16 units, or -1 for its end.
extern void mpPreeditChanged(char *text, long cursor);

static NSRect (*mpOriginalFirstRect)(id, SEL, NSRange, NSRangePointer);
static void (*mpOriginalSetMarked)(id, SEL, id, NSRange, NSRange);
static void (*mpOriginalUnmark)(id, SEL);
static void (*mpOriginalInsert)(id, SEL, id, NSRange);
static NSObject *mpLock;
static NSWindow *mpWindow; // compared by identity only, never messaged
static NSRect mpCaret;     // window content coordinates, origin top-left
static BOOL mpCaretValid;

static BOOL mpOurs(id self) {
    BOOL ours = NO;
    @synchronized(mpLock) { ours = ((NSView *)self).window == mpWindow; }
    return ours;
}

static void mpSetMarked(id self, SEL command, id string, NSRange selected, NSRange replacement) {
    mpOriginalSetMarked(self, command, string, selected, replacement);
    if (!mpOurs(self)) return;
    NSString *text = [string isKindOfClass:[NSAttributedString class]] ? [string string] : string;
    long cursor = selected.location == NSNotFound ? -1 : (long)selected.location;
    mpPreeditChanged((char *)(text ? [text UTF8String] : ""), cursor);
}

static void mpUnmark(id self, SEL command) {
    mpOriginalUnmark(self, command);
    if (mpOurs(self)) mpPreeditChanged("", 0);
}

// Committing the conversion arrives as insertText, so the composition ends here
// whether or not unmarkText is also sent.
static void mpInsert(id self, SEL command, id string, NSRange replacement) {
    mpOriginalInsert(self, command, string, replacement);
    if (mpOurs(self)) mpPreeditChanged("", 0);
}

static NSRect mpFirstRect(id self, SEL command, NSRange range, NSRangePointer actual) {
    NSView *view = (NSView *)self;
    NSRect caret = NSZeroRect;
    BOOL known = NO;
    @synchronized(mpLock) {
        known = mpCaretValid && view.window == mpWindow;
        caret = mpCaret;
    }
    if (!known) return mpOriginalFirstRect(self, command, range, actual);
    // The content view is not flipped, so y is measured from its bottom edge.
    NSRect local = NSMakeRect(caret.origin.x, view.bounds.size.height - caret.origin.y - caret.size.height,
                              caret.size.width, caret.size.height);
    return [view.window convertRectToScreen:[view convertRect:local toView:nil]];
}

static BOOL mpReplace(Class view, SEL selector, IMP replacement, IMP *original) {
    Method method = class_getInstanceMethod(view, selector);
    if (!method) return NO;
    if (!*original) {
        *original = method_getImplementation(method);
        method_setImplementation(method, replacement);
    }
    return YES;
}

int mpInstallInputMethod(uintptr_t handle) {
    __block int installed = 0;
    void (^install)(void) = ^{
        Class view = objc_getClass("GLFWContentView");
        if (!view) return;
        if (!mpLock) mpLock = [NSObject new];
        installed = mpReplace(view, @selector(firstRectForCharacterRange:actualRange:), (IMP)mpFirstRect, (IMP *)&mpOriginalFirstRect) &&
            mpReplace(view, @selector(setMarkedText:selectedRange:replacementRange:), (IMP)mpSetMarked, (IMP *)&mpOriginalSetMarked) &&
            mpReplace(view, @selector(unmarkText), (IMP)mpUnmark, (IMP *)&mpOriginalUnmark) &&
            mpReplace(view, @selector(insertText:replacementRange:), (IMP)mpInsert, (IMP *)&mpOriginalInsert);
        if (installed) { @synchronized(mpLock) { mpWindow = (NSWindow *)handle; } }
    };
    if ([NSThread isMainThread]) install(); else dispatch_sync(dispatch_get_main_queue(), install);
    return installed;
}

void mpSetCaretRect(double x, double y, double width, double height) {
    if (!mpLock) return;
    @synchronized(mpLock) {
        mpCaret = NSMakeRect(x, y, width, height);
        mpCaretValid = YES;
    }
}
