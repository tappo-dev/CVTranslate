//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>
#include <stdatomic.h>

enum { CVTrayIdle = 0, CVTrayOpen = 1, CVTrayQuit = 2 };
static atomic_int trayPending = CVTrayIdle;
// Cocoa objects and action acceptance belong exclusively to the main thread.
static BOOL trayAccepting = NO;
static NSStatusItem *trayStatusItem = nil;

@interface CVTrayTarget : NSObject
- (void)openManagement:(id)sender;
- (void)quitApplication:(id)sender;
@end

@implementation CVTrayTarget
- (void)openManagement:(id)sender {
    if (!trayAccepting) return;
    int expected = CVTrayIdle;
    atomic_compare_exchange_strong(&trayPending, &expected, CVTrayOpen);
}
- (void)quitApplication:(id)sender {
    if (!trayAccepting) return;
    trayAccepting = NO;
    atomic_store(&trayPending, CVTrayQuit);
    for (NSMenuItem *item in [[trayStatusItem menu] itemArray]) {
        [item setEnabled:NO];
    }
    // Do not call terminate: here: the Go owner must cancel and persist work.
}
@end

static CVTrayTarget *trayTarget = nil;

static void CVTrayMain(void (^operation)(void)) {
    if ([NSThread isMainThread]) { @autoreleasepool { operation(); } }
    else { dispatch_sync(dispatch_get_main_queue(), ^{ @autoreleasepool { operation(); } }); }
}

static NSImage *CVTrayImage(const void *imageBytes, size_t imageLength) {
    if (!imageBytes || imageLength == 0) return nil;
    // Copy while the Go call is active; AppKit must never retain a Go pointer.
    NSData *data = [NSData dataWithBytes:imageBytes length:imageLength];
    NSImage *image = [[[NSImage alloc] initWithData:data] autorelease];
    [image setSize:NSMakeSize(18, 18)];
    [image setTemplate:YES];
    return image;
}

int cv_tray_install(const void *imageBytes, size_t imageLength) {
    __block int result = 1;
    CVTrayMain(^{
        if (trayStatusItem || !NSApp) return;
        NSStatusItem *statusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength] retain];
        NSStatusBarButton *button = [statusItem button];
        NSImage *image = CVTrayImage(imageBytes, imageLength);
        if (!button || !image) {
            if (statusItem) [[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
            [statusItem release];
            return;
        }
        CVTrayTarget *target = [[CVTrayTarget alloc] init];
        NSMenu *menu = [[NSMenu alloc] initWithTitle:@"CVTranslate"];
        NSMenuItem *openItem = [[NSMenuItem alloc] initWithTitle:@"\u6253\u5f00\u7ba1\u7406\u754c\u9762"
            action:@selector(openManagement:) keyEquivalent:@""];
        NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"\u9000\u51fa"
            action:@selector(quitApplication:) keyEquivalent:@""];
        if (!target || !menu || !openItem || !quitItem) {
            [openItem release];
            [quitItem release];
            [menu release];
            [target release];
            [[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
            [statusItem release];
            return;
        }
        [menu setAutoenablesItems:NO];
        [openItem setTarget:target];
        [quitItem setTarget:target];
        [menu addItem:openItem];
        [menu addItem:[NSMenuItem separatorItem]];
        [menu addItem:quitItem];
        [openItem release];
        [quitItem release];
        [button setImage:image];
        [button setToolTip:@"CVTranslate"];
        [button setAccessibilityLabel:@"CVTranslate"];
        [statusItem setMenu:menu];
        [menu release];
        trayStatusItem = statusItem;
        trayTarget = target;
        atomic_store(&trayPending, CVTrayIdle);
        trayAccepting = YES;
        result = 0;
    });
    return result;
}

// Called by the Go worker without dispatching to Cocoa or holding Go pointers.
int cv_tray_consume(void) {
    return atomic_exchange(&trayPending, CVTrayIdle);
}

void cv_tray_remove(void) {
    CVTrayMain(^{
        trayAccepting = NO;
        atomic_store(&trayPending, CVTrayIdle);
        if (trayStatusItem) {
            [[trayStatusItem menu] cancelTrackingWithoutAnimation];
            [trayStatusItem setMenu:nil];
            [[NSStatusBar systemStatusBar] removeStatusItem:trayStatusItem];
            [trayStatusItem release];
            trayStatusItem = nil;
        }
        [trayTarget release];
        trayTarget = nil;
    });
}
