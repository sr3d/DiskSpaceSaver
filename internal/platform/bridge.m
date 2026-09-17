#import <Cocoa/Cocoa.h>
#import <Quartz/Quartz.h>
#import <Security/Security.h>
#include "bridge.h"
#include "_cgo_export.h"

static char *dupstr(NSString *s) {
    if (s == nil) return NULL;
    const char *c = [s UTF8String];
    if (c == NULL) return NULL;
    return strdup(c);
}

static char *jsonString(id obj) {
    NSError *err = nil;
    NSData *d = [NSJSONSerialization dataWithJSONObject:obj options:0 error:&err];
    if (d == nil) return dupstr(@"[]");
    return dupstr([[NSString alloc] initWithData:d encoding:NSUTF8StringEncoding]);
}

// Runs a block on the main thread and waits. Bound API calls arrive on a
// worker goroutine, never the main thread, so this cannot self-deadlock.
static void onMainSync(void (^block)(void)) {
    if ([NSThread isMainThread]) { block(); return; }
    dispatch_sync(dispatch_get_main_queue(), block);
}

#pragma mark - Volumes

static NSString *iconDataURI(NSString *path);

// AppKit image work must happen on the main thread; the rest of the volume
// enumeration is NSFileManager and is safe anywhere, so only this hops.
static NSString *iconDataURI(NSString *path) {
    if (![NSThread isMainThread]) {
        __block NSString *r = nil;
        dispatch_sync(dispatch_get_main_queue(), ^{ r = iconDataURI(path); });
        return r;
    }
    NSImage *img = [[NSWorkspace sharedWorkspace] iconForFile:path];
    if (img == nil) return nil;
    NSSize target = NSMakeSize(32, 32);
    NSImage *small = [[NSImage alloc] initWithSize:target];
    [small lockFocus];
    [img drawInRect:NSMakeRect(0, 0, target.width, target.height)
           fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1.0];
    [small unlockFocus];
    NSData *tiff = [small TIFFRepresentation];
    NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:tiff];
    NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    if (png == nil) return nil;
    return [@"data:image/png;base64," stringByAppendingString:
            [png base64EncodedStringWithOptions:0]];
}

char *dss_list_volumes(bool with_icons) {
    @autoreleasepool {
        NSArray<NSURLResourceKey> *keys = @[
            NSURLVolumeNameKey, NSURLVolumeLocalizedNameKey, NSURLVolumeUUIDStringKey,
            NSURLVolumeTotalCapacityKey,
            NSURLVolumeAvailableCapacityKey,
            NSURLVolumeAvailableCapacityForImportantUsageKey,
            NSURLVolumeIsRemovableKey, NSURLVolumeIsEjectableKey,
            NSURLVolumeIsInternalKey, NSURLVolumeIsLocalKey,
            NSURLVolumeIsBrowsableKey, NSURLVolumeIsRootFileSystemKey,
            NSURLVolumeIsReadOnlyKey, NSURLVolumeLocalizedFormatDescriptionKey,
        ];
        NSArray<NSURL *> *urls = [[NSFileManager defaultManager]
            mountedVolumeURLsIncludingResourceValuesForKeys:keys
                                                   options:NSVolumeEnumerationSkipHiddenVolumes];
        NSMutableArray *out = [NSMutableArray array];
        for (NSURL *u in urls) {
            NSDictionary *v = [u resourceValuesForKeys:keys error:nil];
            if (v == nil) continue;
            if (![v[NSURLVolumeIsBrowsableKey] boolValue]) continue;

            // Finder's "available" already counts purgeable space it is
            // willing to evict; the plain figure does not. The gap is what we
            // show as purgeable in the Hidden Space panel.
            long long total     = [v[NSURLVolumeTotalCapacityKey] longLongValue];
            long long important = [v[NSURLVolumeAvailableCapacityForImportantUsageKey] longLongValue];
            long long plain     = [v[NSURLVolumeAvailableCapacityKey] longLongValue];
            long long purgeable = important > plain ? important - plain : 0;

            NSString *name = v[NSURLVolumeLocalizedNameKey] ?: v[NSURLVolumeNameKey] ?: [u lastPathComponent];
            NSMutableDictionary *d = [@{
                @"name":      name,
                @"path":      [u path],
                @"uuid":      v[NSURLVolumeUUIDStringKey] ?: @"",
                @"total":     @(total),
                @"free":      @(important),
                @"availRaw":  @(plain),
                @"purgeable": @(purgeable),
                @"used":      @(total - important),
                // @YES/@NO rather than @(BOOL): NSJSONSerialization writes a
                // char-typed NSNumber as 0/1 and only CFBoolean as true/false.
                @"removable": ([v[NSURLVolumeIsRemovableKey] boolValue] || [v[NSURLVolumeIsEjectableKey] boolValue]) ? @YES : @NO,
                @"internal":  [v[NSURLVolumeIsInternalKey] boolValue] ? @YES : @NO,
                @"network":   [v[NSURLVolumeIsLocalKey] boolValue] ? @NO : @YES,
                @"readOnly":  [v[NSURLVolumeIsReadOnlyKey] boolValue] ? @YES : @NO,
                @"isRoot":    [v[NSURLVolumeIsRootFileSystemKey] boolValue] ? @YES : @NO,
                @"fs":        v[NSURLVolumeLocalizedFormatDescriptionKey] ?: @"",
            } mutableCopy];
            if (with_icons) {
                NSString *icon = iconDataURI([u path]);
                if (icon) d[@"icon"] = icon;
            }
            [out addObject:d];
        }
        return jsonString(out);
    }
}

@interface DSSVolumeWatcher : NSObject
@end
@implementation DSSVolumeWatcher
- (void)changed:(NSNotification *)n { goVolumesChanged(); }
@end

static DSSVolumeWatcher *gWatcher = nil;

void dss_watch_volumes(void) {
    onMainSync(^{
        if (gWatcher != nil) return;
        gWatcher = [DSSVolumeWatcher new];
        NSNotificationCenter *nc = [[NSWorkspace sharedWorkspace] notificationCenter];
        [nc addObserver:gWatcher selector:@selector(changed:)
                   name:NSWorkspaceDidMountNotification object:nil];
        [nc addObserver:gWatcher selector:@selector(changed:)
                   name:NSWorkspaceDidUnmountNotification object:nil];
        [nc addObserver:gWatcher selector:@selector(changed:)
                   name:NSWorkspaceDidRenameVolumeNotification object:nil];
    });
}

void dss_unwatch_volumes(void) {
    onMainSync(^{
        if (gWatcher == nil) return;
        [[[NSWorkspace sharedWorkspace] notificationCenter] removeObserver:gWatcher];
        gWatcher = nil;
    });
}

#pragma mark - Mouse side buttons

// The fourth and fifth mouse buttons, which every browser uses for back and
// forward.
//
// WebKit does read NSEvent.buttonNumber and does have a notion of these two
// (PlatformEventFactoryMac maps 3 and 4 to MouseButton::Back and ::Forward),
// but what the WebView goes on to do with them is its own business: it may
// walk its own history, which here would navigate out of the app, or drop
// them before the DOM ever sees them. A local monitor takes them off the
// queue first, so exactly one thing can happen — the app's own back and
// forward — whatever the WebView would have done.
static id gMouseMon = nil;

void dss_watch_mouse_buttons(void) {
    onMainSync(^{
        if (gMouseMon != nil) return;
        NSEventMask mask = NSEventMaskOtherMouseDown | NSEventMaskOtherMouseUp |
                           NSEventMaskOtherMouseDragged;
        gMouseMon = [NSEvent addLocalMonitorForEventsMatchingMask:mask
                                                          handler:^NSEvent *(NSEvent *e) {
            NSInteger b = [e buttonNumber];
            if (b != 3 && b != 4) return e;  // a real middle click is not ours
            // Only when the app's own window is the one in front. A side
            // button pressed over the folder picker, a sheet or the Quick Look
            // panel must not move the scan view behind it.
            if ([NSApp modalWindow] != nil) return e;
            if ([e window] != [NSApp mainWindow]) return e;
            if ([e type] == NSEventTypeOtherMouseDown) goMouseButton((int)b);
            // The release and any drag go too, or the view is left holding a
            // press it never saw begin.
            return nil;
        }];
    });
}

void dss_unwatch_mouse_buttons(void) {
    onMainSync(^{
        if (gMouseMon == nil) return;
        [NSEvent removeMonitor:gMouseMon];
        gMouseMon = nil;
    });
}

#pragma mark - Finder actions

char *dss_trash(const char *path, char **out_new_path) {
    @autoreleasepool {
        NSURL *u = [NSURL fileURLWithPath:@(path)];
        NSURL *dest = nil;
        NSError *err = nil;
        // trashItemAtURL: is what Finder itself uses, so the move lands in the
        // right per-volume .Trashes and Cmd-Z in Finder puts it back.
        BOOL ok = [[NSFileManager defaultManager] trashItemAtURL:u resultingItemURL:&dest error:&err];
        if (!ok) return dupstr([err localizedDescription] ?: @"could not move to Trash");
        if (out_new_path) *out_new_path = dupstr([dest path]);
        return NULL;
    }
}

char *dss_reveal(const char *const *paths, int n) {
    @autoreleasepool {
        NSMutableArray *urls = [NSMutableArray arrayWithCapacity:n];
        for (int i = 0; i < n; i++) [urls addObject:[NSURL fileURLWithPath:@(paths[i])]];
        dispatch_async(dispatch_get_main_queue(), ^{
            [[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:urls];
        });
        return NULL;
    }
}

char *dss_get_info(const char *path) {
    @autoreleasepool {
        NSString *p = @(path);
        // Finder has no public API for the Get Info window; AppleScript is the
        // supported route and is what every file manager on the platform uses.
        NSString *src = [NSString stringWithFormat:
            @"tell application \"Finder\"\n"
             "  activate\n"
             "  open information window of (POSIX file \"%@\" as alias)\n"
             "end tell",
            [p stringByReplacingOccurrencesOfString:@"\"" withString:@"\\\""]];
        __block char *err = NULL;
        onMainSync(^{
            NSDictionary *e = nil;
            NSAppleScript *s = [[NSAppleScript alloc] initWithSource:src];
            [s executeAndReturnError:&e];
            if (e != nil) err = dupstr(e[NSAppleScriptErrorMessage] ?: @"Get Info failed");
        });
        return err;
    }
}

char *dss_open(const char *path) {
    @autoreleasepool {
        NSURL *u = [NSURL fileURLWithPath:@(path)];
        dispatch_async(dispatch_get_main_queue(), ^{
            [[NSWorkspace sharedWorkspace] openURL:u];
        });
        return NULL;
    }
}

char *dss_open_url(const char *url) {
    @autoreleasepool {
        NSURL *u = [NSURL URLWithString:@(url)];
        if (u == nil) return dupstr(@"invalid URL");
        dispatch_async(dispatch_get_main_queue(), ^{
            [[NSWorkspace sharedWorkspace] openURL:u];
        });
        return NULL;
    }
}

#pragma mark - Quick Look

@interface DSSQLSource : NSObject <QLPreviewPanelDataSource, QLPreviewPanelDelegate>
@property (nonatomic, strong) NSArray<NSURL *> *urls;
@end
@implementation DSSQLSource
- (NSInteger)numberOfPreviewItemsInPreviewPanel:(QLPreviewPanel *)panel { return self.urls.count; }
- (id<QLPreviewItem>)previewPanel:(QLPreviewPanel *)panel previewItemAtIndex:(NSInteger)i {
    return self.urls[i];
}
@end

static DSSQLSource *gQL = nil;

void dss_quicklook(const char *const *paths, int n, int index) {
    @autoreleasepool {
        NSMutableArray *urls = [NSMutableArray arrayWithCapacity:n];
        for (int i = 0; i < n; i++) [urls addObject:[NSURL fileURLWithPath:@(paths[i])]];
        dispatch_async(dispatch_get_main_queue(), ^{
            if (gQL == nil) gQL = [DSSQLSource new];
            gQL.urls = urls;
            QLPreviewPanel *panel = [QLPreviewPanel sharedPreviewPanel];
            // Claim the panel directly rather than through the responder
            // chain: the WKWebView in the window does not forward
            // acceptsPreviewPanelControl:.
            panel.dataSource = gQL;
            panel.delegate = gQL;
            [panel reloadData];
            if (index >= 0 && index < (int)urls.count) panel.currentPreviewItemIndex = index;
            if ([panel isVisible]) { [panel orderOut:nil]; }
            else { [panel makeKeyAndOrderFront:nil]; }
        });
    }
}

void dss_quicklook_close(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        QLPreviewPanel *p = [QLPreviewPanel sharedPreviewPanel];
        if ([p isVisible]) [p orderOut:nil];
    });
}

bool dss_quicklook_visible(void) {
    __block bool v = false;
    onMainSync(^{ v = [[QLPreviewPanel sharedPreviewPanel] isVisible]; });
    return v;
}

#pragma mark - Dock tile

void dss_set_dock_badge(const char *text) {
    NSString *s = (text == NULL || *text == 0) ? @"" : @(text);
    dispatch_async(dispatch_get_main_queue(), ^{
        [[NSApp dockTile] setBadgeLabel:s.length ? s : nil];
    });
}

// A thin bar drawn across the bottom of the Dock icon, the way Finder shows a
// copy in progress.
@interface DSSDockProgressView : NSView
@property (nonatomic) double fraction;
@end
@implementation DSSDockProgressView
- (void)drawRect:(NSRect)r {
    [[NSApp applicationIconImage] drawInRect:[self bounds]
                                    fromRect:NSZeroRect
                                   operation:NSCompositingOperationCopy fraction:1.0];
    if (self.fraction < 0) return;
    NSRect b = [self bounds];
    NSRect track = NSMakeRect(b.size.width * 0.08, b.size.height * 0.06,
                              b.size.width * 0.84, b.size.height * 0.09);
    NSBezierPath *bg = [NSBezierPath bezierPathWithRoundedRect:track
                        xRadius:track.size.height / 2 yRadius:track.size.height / 2];
    [[NSColor colorWithWhite:0 alpha:0.55] setFill];
    [bg fill];
    NSRect fill = track;
    fill.size.width = track.size.width * MAX(0.0, MIN(1.0, self.fraction));
    if (fill.size.width > 1) {
        NSBezierPath *fp = [NSBezierPath bezierPathWithRoundedRect:fill
                            xRadius:fill.size.height / 2 yRadius:fill.size.height / 2];
        [[NSColor controlAccentColor] setFill];
        [fp fill];
    }
}
@end

static DSSDockProgressView *gDockView = nil;

void dss_set_dock_progress(double fraction) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSDockTile *tile = [NSApp dockTile];
        if (fraction < 0) {
            [tile setContentView:nil];
            gDockView = nil;
            [tile display];
            return;
        }
        if (gDockView == nil) {
            gDockView = [[DSSDockProgressView alloc] initWithFrame:NSMakeRect(0, 0, 128, 128)];
            [tile setContentView:gDockView];
        }
        gDockView.fraction = fraction;
        [tile display];
    });
}

#pragma mark - Code identity

bool dss_is_bundled(void) {
    NSBundle *b = [NSBundle mainBundle];
    return [b bundleIdentifier] != nil;
}

char *dss_bundle_id(void) {
    return dupstr([[NSBundle mainBundle] bundleIdentifier]);
}

// The Team ID of the running code, read back from its own signature. The
// helper's designated requirement is built from this, so a build signed with a
// different team cannot talk to a helper installed by another.
char *dss_team_id(void) {
    SecCodeRef self_code = NULL;
    if (SecCodeCopySelf(kSecCSDefaultFlags, &self_code) != errSecSuccess) return NULL;
    CFDictionaryRef info = NULL;
    OSStatus st = SecCodeCopySigningInformation(
        (SecStaticCodeRef)self_code, kSecCSSigningInformation, &info);
    CFRelease(self_code);
    if (st != errSecSuccess || info == NULL) return NULL;
    NSString *team = (__bridge NSString *)CFDictionaryGetValue(info, kSecCodeInfoTeamIdentifier);
    char *out = dupstr(team);
    CFRelease(info);
    return out;
}

#pragma mark - Thumbnails

#import <QuickLookThumbnailing/QuickLookThumbnailing.h>

// QuickLook is what Finder uses, so anything Finder can preview — images,
// PDFs, video, audio artwork, documents — gets a thumbnail here without this
// file knowing a single file format.
char *dss_thumbnail(const char *path, int max_points, double scale) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:@(path)];
        if (url == nil) return NULL;

        CGSize size = CGSizeMake(max_points, max_points);
        QLThumbnailGenerationRequest *req =
            [[QLThumbnailGenerationRequest alloc] initWithFileAtURL:url
                                                               size:size
                                                              scale:scale
                                                representationTypes:
                    QLThumbnailGenerationRequestRepresentationTypeThumbnail];
        // Low quality thumbnails are icons with a badge, which tell the user
        // nothing they cannot already see in the list.
        req.minimumDimension = 32;

        __block NSData *png = nil;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        [[QLThumbnailGenerator sharedGenerator]
            generateBestRepresentationForRequest:req
                               completionHandler:^(QLThumbnailRepresentation *rep, NSError *err) {
            if (rep != nil && err == nil) {
                CGImageRef cg = rep.CGImage;
                if (cg != NULL) {
                    NSBitmapImageRep *bitmap =
                        [[NSBitmapImageRep alloc] initWithCGImage:cg];
                    png = [bitmap representationUsingType:NSBitmapImageFileTypePNG
                                               properties:@{}];
                }
            }
            dispatch_semaphore_signal(done);
        }];

        // A thumbnail that has to be generated from a large file can take a
        // moment; one that never arrives must not hold the caller forever.
        if (dispatch_semaphore_wait(done,
                dispatch_time(DISPATCH_TIME_NOW, (int64_t)(8 * NSEC_PER_SEC))) != 0) {
            return NULL;
        }
        if (png == nil) return NULL;
        return dupstr([@"data:image/png;base64," stringByAppendingString:
                       [png base64EncodedStringWithOptions:0]]);
    }
}

#pragma mark - SMAppService

#import <ServiceManagement/ServiceManagement.h>

// SMAppService arrived in macOS 13. The deployment target is 13, but guard
// anyway so a stray older host degrades to a clear message instead of a crash.
static SMAppService *helperService(const char *plist_name) {
    if (@available(macOS 13.0, *)) {
        return [SMAppService daemonServiceWithPlistName:@(plist_name)];
    }
    return nil;
}

char *dss_helper_register(const char *plist_name) {
    @autoreleasepool {
        SMAppService *svc = helperService(plist_name);
        if (svc == nil) return dupstr(@"this version of macOS cannot install the helper");
        NSError *err = nil;
        if ([svc registerAndReturnError:&err]) return NULL;
        // errOSAAlreadyRegistered: already installed is success from the
        // caller's point of view.
        if (err != nil && [err code] == kSMErrorAlreadyRegistered) return NULL;
        return dupstr([err localizedDescription] ?: @"the helper could not be registered");
    }
}

char *dss_helper_unregister(const char *plist_name) {
    @autoreleasepool {
        SMAppService *svc = helperService(plist_name);
        if (svc == nil) return dupstr(@"this version of macOS cannot manage the helper");
        NSError *err = nil;
        if ([svc unregisterAndReturnError:&err]) return NULL;
        return dupstr([err localizedDescription] ?: @"the helper could not be removed");
    }
}

char *dss_helper_status(const char *plist_name) {
    @autoreleasepool {
        SMAppService *svc = helperService(plist_name);
        if (svc == nil) return dupstr(@"unsupported");
        switch ([svc status]) {
            case SMAppServiceStatusNotRegistered:    return dupstr(@"not-registered");
            case SMAppServiceStatusEnabled:          return dupstr(@"enabled");
            case SMAppServiceStatusRequiresApproval: return dupstr(@"requires-approval");
            case SMAppServiceStatusNotFound:         return dupstr(@"not-found");
            default:                                 return dupstr(@"unknown");
        }
    }
}

void dss_open_login_items_settings(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (@available(macOS 13.0, *)) {
            [SMAppService openSystemSettingsLoginItems];
        }
    });
}
