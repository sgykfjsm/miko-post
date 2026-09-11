//go:build darwin && !ci

#import <AppKit/AppKit.h>
#include <stdatomic.h>

// Installation/removal run on AppKit's main thread. The UI goroutine reads only
// the atomic counter; Cocoa blocks never call Go or retain a Go pointer.
@interface MPActivity : NSObject {
@public
    atomic_uint_fast64_t count;
    id monitor;
    id focus;
    NSEventModifierFlags modifiers;
}
@end
@implementation MPActivity
@end

void *mpObserve(uintptr_t handle) {
    __block MPActivity *observer = nil;
    void (^install)(void) = ^{
        NSWindow *window = (NSWindow *)handle;
        observer = [MPActivity new];
        atomic_init(&observer->count, 0);
        NSEventMask mask = NSEventMaskKeyDown | NSEventMaskFlagsChanged |
            NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown | NSEventMaskOtherMouseDown |
            NSEventMaskScrollWheel;
        MPActivity *state = observer;
        state->modifiers = [NSEvent modifierFlags];
        observer->monitor = [[NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *event) {
            BOOL pressed = YES;
            if (event.type == NSEventTypeFlagsChanged) {
                pressed = (event.modifierFlags & ~state->modifiers) != 0;
                state->modifiers = event.modifierFlags;
            }
            // Releasing the submission chord is not a new interaction.
            if (pressed && event.window == window) atomic_fetch_add(&state->count, 1);
            return event;
        }] retain];
        observer->focus = [[[NSNotificationCenter defaultCenter]
            addObserverForName:NSWindowDidBecomeKeyNotification object:window queue:nil
            usingBlock:^(NSNotification *note) { atomic_fetch_add(&state->count, 1); }] retain];
    };
    if ([NSThread isMainThread]) install(); else dispatch_sync(dispatch_get_main_queue(), install);
    return observer;
}

uint64_t mpActivity(void *observer) {
    return atomic_load(&((MPActivity *)observer)->count);
}

void mpRelease(void *pointer) {
    MPActivity *observer = pointer;
    void (^remove)(void) = ^{
        [NSEvent removeMonitor:observer->monitor];
        [[NSNotificationCenter defaultCenter] removeObserver:observer->focus];
        [observer->monitor release];
        [observer->focus release];
        [observer release];
    };
    if ([NSThread isMainThread]) remove(); else dispatch_sync(dispatch_get_main_queue(), remove);
}
