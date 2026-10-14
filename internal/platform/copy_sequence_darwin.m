//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <Carbon/Carbon.h>
#import <dispatch/dispatch.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <stdlib.h>
#include "native_darwin.h"

// Matches the normalized event kinds in copy_events_mac.go.
enum { CVCopyTimer, CVCopyKeyDown, CVCopyKeyUp, CVCopyFlagsChanged, CVCopyInterrupted, CVCopyListenerFailed };

typedef struct {
    uintptr_t handle;
    CFMachPortRef tap;
    CFRunLoopSourceRef source;
    CFRunLoopTimerRef timer;
    CFRunLoopRef runLoop;
    CFAbsoluteTime previousTick;
    atomic_bool stopping;
    BOOL failed;
} CVCopyListener;

static void CVCopyMain(void (^operation)(void)) {
    if ([NSThread isMainThread]) { @autoreleasepool { operation(); } }
    else { dispatch_sync(dispatch_get_main_queue(), ^{ @autoreleasepool { operation(); } }); }
}

int cv_copy_permitted(void) {
    // Preflight only: never request or change the user's privacy permissions.
    if (@available(macOS 10.15, *)) return CGPreflightListenEventAccess() ? 1 : 0;
    return 0;
}

void cv_copy_snapshot(uintptr_t *foreground, uint64_t *version) {
    @autoreleasepool {
        *foreground = 0;
        *version = 0;
        if (!AXIsProcessTrusted() || IsSecureEventInputEnabled()) return;
        pid_t process = [[[NSWorkspace sharedWorkspace] frontmostApplication] processIdentifier];
        if (process <= 0) return;
        // Only window identity/layer/owner metadata is inspected, never titles,
        // pixels, selected text or contents. This needs no screen-recording grant.
        CFArrayRef windowList = CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
        if (!windowList) return;
        for (NSDictionary *window in (NSArray *)windowList) {
            if ([window[(id)kCGWindowOwnerPID] intValue] != process || [window[(id)kCGWindowLayer] intValue] != 0) continue;
            uint32_t windowNumber = [window[(id)kCGWindowNumber] unsignedIntValue];
            if (windowNumber) *foreground = ((uint64_t)(uint32_t)process << 32) | windowNumber;
            break;
        }
        CFRelease(windowList);
        if ([[[NSWorkspace sharedWorkspace] frontmostApplication] processIdentifier] != process) { *foreground = 0; return; }
        if (*foreground) *version = (uint64_t)[[NSPasteboard generalPasteboard] changeCount];
    }
}

static void CVCopyFail(CVCopyListener *listener) {
    if (listener->failed) return;
    listener->failed = YES;
    CGEventTapEnable(listener->tap, false);
    cv_go_copy_event(listener->handle, CVCopyListenerFailed, 0, 0, 0, 0);
}

static CGEventRef CVCopyEvent(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *opaque) {
    CVCopyListener *listener = opaque;
    @autoreleasepool {
        if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
            CVCopyFail(listener);
            return event;
        }
        if (listener->failed || !event) return event;
        int kind = CVCopyInterrupted;
        if (type == kCGEventKeyDown) kind = CVCopyKeyDown;
        else if (type == kCGEventKeyUp) kind = CVCopyKeyUp;
        else if (type == kCGEventFlagsChanged) kind = CVCopyFlagsChanged;
        BOOL keyboard = kind != CVCopyInterrupted;
        int key = keyboard ? (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode) : 0;
        CGEventFlags flags = CGEventGetFlags(event);
        CGEventFlags conflicting = kCGEventFlagMaskControl | kCGEventFlagMaskAlternate | kCGEventFlagMaskShift | kCGEventFlagMaskSecondaryFn;
        BOOL commandOnly = (flags & kCGEventFlagMaskCommand) && !(flags & conflicting);
        BOOL repeated = keyboard && CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat) != 0;
        BOOL injected = CGEventGetIntegerValueField(event, kCGEventSourceUnixProcessID) != 0;
        cv_go_copy_event(listener->handle, kind, key, commandOnly, repeated, injected);
    }
    // Always pass the original event through. A synchronous tap samples the
    // generation before this copy reaches the application; listen-only delivery
    // can race publication and mistake the new generation for the old baseline.
    return event;
}

static void CVCopyTick(CFRunLoopTimerRef timer, void *opaque) {
    CVCopyListener *listener = opaque;
    @autoreleasepool {
        if (listener->failed) return;
        if (!AXIsProcessTrusted() || !cv_copy_permitted() || !CGEventTapIsEnabled(listener->tap)) {
            CVCopyFail(listener);
            return;
        }
        CFAbsoluteTime now = CFAbsoluteTimeGetCurrent();
        BOOL interrupted = IsSecureEventInputEnabled() || now - listener->previousTick > 1.0 || now < listener->previousTick;
        listener->previousTick = now;
        cv_go_copy_event(listener->handle, interrupted ? CVCopyInterrupted : CVCopyTimer, 0, 0, 0, 0);
    }
}

static void CVCopyRemoveSources(CVCopyListener *listener) {
    if (listener->timer) { CFRunLoopTimerInvalidate(listener->timer); CFRelease(listener->timer); }
    if (listener->tap) { CGEventTapEnable(listener->tap, false); CFMachPortInvalidate(listener->tap); }
    if (listener->source) { CFRunLoopRemoveSource(listener->runLoop, listener->source, kCFRunLoopCommonModes); CFRelease(listener->source); }
    if (listener->tap) CFRelease(listener->tap);
}

int cv_copy_run(uintptr_t handle) {
    @autoreleasepool {
        int result = !AXIsProcessTrusted() ? 1 : (!cv_copy_permitted() ? 9 : 0);
        if (result) { cv_go_copy_ready(handle, NULL, result); return result; }
        CVCopyListener *listener = calloc(1, sizeof(CVCopyListener));
        if (!listener) { cv_go_copy_ready(handle, NULL, 10); return 10; }
        listener->handle = handle;
        listener->runLoop = (CFRunLoopRef)CFRetain(CFRunLoopGetCurrent());
        atomic_init(&listener->stopping, false);
        CGEventMask events = CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventKeyUp) | CGEventMaskBit(kCGEventFlagsChanged)
            | CGEventMaskBit(kCGEventLeftMouseDown) | CGEventMaskBit(kCGEventRightMouseDown) | CGEventMaskBit(kCGEventOtherMouseDown);
        listener->tap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault, events, CVCopyEvent, listener);
        if (listener->tap) listener->source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, listener->tap, 0);
        CFRunLoopTimerContext timerContext = {0, listener, NULL, NULL, NULL};
        listener->previousTick = CFAbsoluteTimeGetCurrent();
        if (listener->source) listener->timer = CFRunLoopTimerCreate(kCFAllocatorDefault, listener->previousTick + 0.025, 0.025, 0, 0, CVCopyTick, &timerContext);
        if (!listener->tap || !listener->source || !listener->timer) {
            CVCopyRemoveSources(listener);
            cv_copy_dispose(listener);
            cv_go_copy_ready(handle, NULL, 10);
            return 10;
        }
        if (CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState, 8)) cv_go_copy_event(handle, CVCopyKeyDown, 8, 0, 0, 0);
        CFRunLoopAddSource(listener->runLoop, listener->source, kCFRunLoopCommonModes);
        CFRunLoopAddTimer(listener->runLoop, listener->timer, kCFRunLoopCommonModes);
        CGEventTapEnable(listener->tap, true);
        cv_go_copy_ready(handle, listener, 0);
        // A dedicated run loop cannot be delayed by main-thread AX inspection.
        // The atomic flag also covers close arriving before the loop starts.
        while (!atomic_load(&listener->stopping) && !listener->failed) {
            SInt32 reason = CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.25, false);
            if (reason == kCFRunLoopRunFinished) CVCopyFail(listener);
        }
        CVCopyRemoveSources(listener);
        return listener->failed ? 10 : 0;
    }
}

void cv_copy_stop(void *opaque) {
    CVCopyListener *listener = opaque;
    atomic_store(&listener->stopping, true);
    CFRunLoopStop(listener->runLoop);
    CFRunLoopWakeUp(listener->runLoop);
}

void cv_copy_dispose(void *opaque) {
    CVCopyListener *listener = opaque;
    CFRelease(listener->runLoop);
    free(listener);
}

int cv_validate_clipboard(uint64_t version) {
    __block int result = 0;
    CVCopyMain(^{
        if (!AXIsProcessTrusted()) { result = 1; return; }
        if (!version || (uint64_t)[[NSPasteboard generalPasteboard] changeCount] != version) result = 4;
    });
    return result;
}

int cv_dispatch_direct(uint64_t version, uintptr_t context) {
    __block int result = 0;
    CVCopyMain(^{
        result = cv_validate_clipboard(version);
        if (result) return;
        if (!cv_released()) { result = 6; return; }
        CGEventRef keydown = CGEventCreateKeyboardEvent(NULL, 9, true);
        CGEventRef keyup = CGEventCreateKeyboardEvent(NULL, 9, false);
        if (!keydown || !keyup) {
            if (keydown) CFRelease(keydown);
            if (keyup) CFRelease(keyup);
            result = 7;
            return;
        }
        CGEventSetFlags(keydown, kCGEventFlagMaskCommand);
        CGEventSetFlags(keyup, kCGEventFlagMaskCommand);
        result = cv_validate_clipboard(version);
        if (!result && !cv_released()) result = 6;
        if (!result && cv_go_paste_cancelled(context)) result = 8;
        if (!result) {
            // Explicit three-copy policy: no target inspection or activation.
            CGEventPost(kCGSessionEventTap, keydown);
            CGEventPost(kCGSessionEventTap, keyup);
        }
        CFRelease(keydown);
        CFRelease(keyup);
    });
    return result;
}
