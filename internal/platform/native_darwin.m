//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <Security/Security.h>
#import <dispatch/dispatch.h>
#include <stdlib.h>
#include <string.h>
#include "native_darwin.h"

typedef struct {
    AXUIElementRef element;
    AXUIElementRef window;
    pid_t process;
    uint64_t token;
} CVState;

static void CVMain(void (^operation)(void)) {
    if ([NSThread isMainThread]) { @autoreleasepool { operation(); } }
    else { dispatch_sync(dispatch_get_main_queue(), ^{ @autoreleasepool { operation(); } }); }
}

static void CVClear(CVState *state) {
    if (state->element) CFRelease(state->element);
    if (state->window) CFRelease(state->window);
    state->element = NULL;
    state->window = NULL;
    state->process = 0;
}

void *cv_create(void) { return calloc(1, sizeof(CVState)); }
void cv_close(void *opaque) {
    if (!opaque) return;
    CVMain(^{ CVClear((CVState *)opaque); free(opaque); });
}
int cv_trusted(void) { return AXIsProcessTrusted() ? 1 : 0; }

static CFTypeRef CVAttribute(AXUIElementRef element, CFStringRef attribute) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess) return NULL;
    return value;
}

// Returns 2 for a recognized secure field, 0 for uncertain focus.
// References are retained only when all target checks pass.
static int CVFocusedElement(AXUIElementRef *elementOut, AXUIElementRef *windowOut, pid_t *processOut) {
    if (!AXIsProcessTrusted()) return 0;
    NSRunningApplication *frontmost = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (!frontmost) return 0;
    pid_t process = [frontmost processIdentifier];
    if (process <= 0) return 0;
    AXUIElementRef application = AXUIElementCreateApplication(process);
    if (!application) return 0;
    if (AXUIElementSetMessagingTimeout(application, 0.3) != kAXErrorSuccess) { CFRelease(application); return 0; }
    CFTypeRef elementValue = CVAttribute(application, kAXFocusedUIElementAttribute);
    CFRelease(application);
    if (!elementValue || CFGetTypeID(elementValue) != AXUIElementGetTypeID()) {
        if (elementValue) CFRelease(elementValue);
        return 0;
    }
    AXUIElementRef element = (AXUIElementRef)elementValue;
    if (AXUIElementSetMessagingTimeout(element, 0.3) != kAXErrorSuccess) { CFRelease(element); return 0; }
    pid_t elementProcess = 0;
    if (AXUIElementGetPid(element, &elementProcess) != kAXErrorSuccess || elementProcess != process) { CFRelease(element); return 0; }
    CFTypeRef subrole = CVAttribute(element, kAXSubroleAttribute);
    BOOL secure = subrole && CFEqual(subrole, CFSTR("AXSecureTextField"));
    if (subrole) CFRelease(subrole);
    if (secure) { CFRelease(element); return 2; }
    CFTypeRef windowValue = CVAttribute(element, kAXWindowAttribute);
    BOOL validWindow = windowValue && CFGetTypeID(windowValue) == AXUIElementGetTypeID();
    if (!validWindow || [[[NSWorkspace sharedWorkspace] frontmostApplication] processIdentifier] != process) {
        if (windowValue) CFRelease(windowValue);
        CFRelease(element);
        return 0;
    }
    *elementOut = element;
    *windowOut = (AXUIElementRef)windowValue;
    *processOut = process;
    return 1;
}

static BOOL CVEditable(AXUIElementRef element) {
    CFTypeRef role = CVAttribute(element, kAXRoleAttribute);
    BOOL textRole = role && (CFEqual(role, kAXTextFieldRole) || CFEqual(role, kAXTextAreaRole) || CFEqual(role, kAXComboBoxRole));
    if (role) CFRelease(role);
    Boolean settable = false;
    AXError editable = AXUIElementIsAttributeSettable(element, kAXValueAttribute, &settable);
    CFTypeRef enabled = CVAttribute(element, kAXEnabledAttribute);
    BOOL isEnabled = enabled && CFEqual(enabled, kCFBooleanTrue);
    if (enabled) CFRelease(enabled);
    return textRole && editable == kAXErrorSuccess && settable && isEnabled;
}

static int CVFocus(AXUIElementRef *elementOut, AXUIElementRef *windowOut, pid_t *processOut) {
    AXUIElementRef element = NULL, window = NULL;
    pid_t process = 0;
    int focus = CVFocusedElement(&element, &window, &process);
    if (focus != 1) return focus;
    if (!CVEditable(element) || [[[NSWorkspace sharedWorkspace] frontmostApplication] processIdentifier] != process) {
        CFRelease(window);
        CFRelease(element);
        return 0;
    }
    *elementOut = element;
    *windowOut = window;
    *processOut = process;
    return 1;
}

int cv_capture(void *opaque, char **text, uint64_t *version, uint64_t *token) {
    __block int result = 0;
    CVMain(^{
        CVState *state = opaque;
        CVClear(state);
        state->token++;
        @try {
            int focus = CVFocus(&state->element, &state->window, &state->process);
            if (focus == 2) { result = 2; return; }
            NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
            NSInteger generation = [pasteboard changeCount];
            NSArray *sensitiveTypes = @[@"org.nspasteboard.ConcealedType", @"org.nspasteboard.TransientType", @"org.nspasteboard.AutoGeneratedType", @"application/x-keepassxc"];
            if ([pasteboard availableTypeFromArray:sensitiveTypes]) { result = 2; return; }
            NSString *contents = [pasteboard stringForType:NSPasteboardTypeString];
            if (!contents) { result = 3; return; }
            if ([contents lengthOfBytesUsingEncoding:NSUTF8StringEncoding] > 4 * 1024 * 1024) { result = 3; return; }
            if ([pasteboard changeCount] != generation) { result = 4; return; }
            *text = strdup([contents UTF8String]);
            if (!*text) { result = 3; return; }
            *version = (uint64_t)generation;
            *token = focus == 1 ? state->token : 0;
        } @finally {
            if (result) CVClear(state);
        }
    });
    return result;
}

static int CVValidate(CVState *state, uint64_t token, uint64_t version) {
    if (!token || token != state->token || !state->element || !state->window) return 5;
    if (!AXIsProcessTrusted()) return 1;
    if ((uint64_t)[[NSPasteboard generalPasteboard] changeCount] != version) return 4;
    AXUIElementRef element = NULL, window = NULL;
    pid_t process = 0;
    int focus = CVFocus(&element, &window, &process);
    BOOL matches = focus == 1 && process == state->process && CFEqual(element, state->element) && CFEqual(window, state->window);
    if (element) CFRelease(element);
    if (window) CFRelease(window);
    if ((uint64_t)[[NSPasteboard generalPasteboard] changeCount] != version) return 4;
    return matches ? 0 : 5;
}

int cv_validate(void *opaque, uint64_t token, uint64_t version) {
    __block int result;
    CVMain(^{ result = CVValidate(opaque, token, version); });
    return result;
}

int cv_released(void) {
    CGEventFlags flags = CGEventSourceFlagsState(kCGEventSourceStateCombinedSessionState);
    CGEventFlags modifiers = kCGEventFlagMaskShift | kCGEventFlagMaskControl | kCGEventFlagMaskAlternate | kCGEventFlagMaskCommand | kCGEventFlagMaskSecondaryFn;
    return (flags & modifiers) == 0;
}

int cv_write(uint64_t version, const char *text, uint64_t *written) {
    __block int result = 0;
    CVMain(^{
        NSString *contents = [NSString stringWithUTF8String:text];
        if (!contents) { result = 3; return; }
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        if ((uint64_t)[pasteboard changeCount] != version) { result = 4; return; }
        // NSPasteboard offers no cross-process compare-and-swap/locking API.
        NSInteger cleared = [pasteboard clearContents];
        if ([pasteboard changeCount] != cleared || ![pasteboard setString:contents forType:NSPasteboardTypeString]) { result = 4; return; }
        NSInteger published = [pasteboard changeCount];
        NSString *current = [pasteboard stringForType:NSPasteboardTypeString];
        if (![current isEqualToString:contents] || [pasteboard changeCount] != published) { result = 4; return; }
        *written = (uint64_t)published;
    });
    return result;
}

int cv_dispatch(void *opaque, uint64_t token, uint64_t version) {
    __block int result = 0;
    CVMain(^{
        CVState *state = opaque;
        result = CVValidate(state, token, version);
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
        // Target the verified process; never activate it or post Enter.
        CGEventPostToPid(state->process, keydown);
        CGEventPostToPid(state->process, keyup);
        CFRelease(keydown);
        CFRelease(keyup);
    });
    return result;
}

static NSMutableDictionary *CVQuery(const char *service, const char *account) {
    return [NSMutableDictionary dictionaryWithDictionary:@{
        (id)kSecClass: (id)kSecClassGenericPassword,
        (id)kSecAttrService: [NSString stringWithUTF8String:service],
        (id)kSecAttrAccount: [NSString stringWithUTF8String:account],
        (id)kSecAttrSynchronizable: @NO,
        (id)kSecUseAuthenticationUI: (id)kSecUseAuthenticationUIFail
    }];
}

int cv_credential_get(const char *service, const char *account, void **value, size_t *length) {
    @autoreleasepool {
        NSMutableDictionary *query = CVQuery(service, account);
        query[(id)kSecReturnData] = @YES;
        query[(id)kSecMatchLimit] = (id)kSecMatchLimitOne;
        CFTypeRef found = NULL;
        OSStatus status = SecItemCopyMatching((CFDictionaryRef)query, &found);
        if (status != errSecSuccess) return (int)status;
        if (!found || CFGetTypeID(found) != CFDataGetTypeID()) { if (found) CFRelease(found); return (int)errSecDecode; }
        *length = (size_t)CFDataGetLength((CFDataRef)found);
        *value = malloc(*length ? *length : 1);
        if (!*value) { CFRelease(found); return (int)errSecAllocate; }
        memcpy(*value, CFDataGetBytePtr((CFDataRef)found), *length);
        CFRelease(found);
        return 0;
    }
}

int cv_credential_set(const char *service, const char *account, const void *value, size_t length) {
    @autoreleasepool {
        NSMutableDictionary *query = CVQuery(service, account);
        NSData *data = [NSData dataWithBytes:value length:length];
        OSStatus status = SecItemUpdate((CFDictionaryRef)query, (CFDictionaryRef)@{(id)kSecValueData: data});
        if (status == errSecItemNotFound) {
            query[(id)kSecValueData] = data;
            status = SecItemAdd((CFDictionaryRef)query, NULL);
        }
        return (int)status;
    }
}

int cv_credential_delete(const char *service, const char *account) {
    @autoreleasepool { return (int)SecItemDelete((CFDictionaryRef)CVQuery(service, account)); }
}
