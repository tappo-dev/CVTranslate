//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>
#include <stdatomic.h>
#include <stdbool.h>

static atomic_bool reopenPending = false;
static atomic_bool reopenAccepting = false;

@interface CVReopenDelegate : NSObject <NSApplicationDelegate> {
    id previousDelegate;
}
- (instancetype)initWithPrevious:(id)previous;
- (id)previous;
@end

@implementation CVReopenDelegate
- (instancetype)initWithPrevious:(id)previous {
    self = [super init];
    if (self) previousDelegate = [previous retain];
    return self;
}
- (id)previous { return previousDelegate; }
- (void)dealloc {
    [previousDelegate release];
    [super dealloc];
}
- (BOOL)respondsToSelector:(SEL)selector {
    return [super respondsToSelector:selector] || [previousDelegate respondsToSelector:selector];
}
- (id)forwardingTargetForSelector:(SEL)selector {
    if ([previousDelegate respondsToSelector:selector]) return previousDelegate;
    return [super forwardingTargetForSelector:selector];
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)application hasVisibleWindows:(BOOL)visible {
    if (atomic_load(&reopenAccepting)) atomic_store(&reopenPending, true);
    if ([previousDelegate respondsToSelector:_cmd]) {
        return [previousDelegate applicationShouldHandleReopen:application hasVisibleWindows:visible];
    }
    return YES;
}
@end

static CVReopenDelegate *reopenDelegate = nil;

static void CVReopenMain(void (^operation)(void)) {
    if ([NSThread isMainThread]) { @autoreleasepool { operation(); } }
    else { dispatch_sync(dispatch_get_main_queue(), ^{ @autoreleasepool { operation(); } }); }
}

void cv_reopen_install(void) {
    CVReopenMain(^{
        if (reopenDelegate) return;
        NSApplication *application = [NSApplication sharedApplication];
        reopenDelegate = [[CVReopenDelegate alloc] initWithPrevious:[application delegate]];
        atomic_store(&reopenPending, false);
        atomic_store(&reopenAccepting, true);
        [application setDelegate:reopenDelegate];
    });
}

int cv_reopen_consume(void) {
    return atomic_exchange(&reopenPending, false) && atomic_load(&reopenAccepting);
}

void cv_reopen_disable(void) {
    atomic_store(&reopenAccepting, false);
    atomic_store(&reopenPending, false);
}

void cv_reopen_uninstall(void) {
    cv_reopen_disable();
    CVReopenMain(^{
        if (!reopenDelegate) return;
        // Do not overwrite a delegate installed by someone else after us.
        if ([NSApp delegate] == reopenDelegate) [NSApp setDelegate:[reopenDelegate previous]];
        [reopenDelegate release];
        reopenDelegate = nil;
    });
}
