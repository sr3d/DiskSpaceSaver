//go:build ignore
//
// Sparkle bridge.
//
// Sparkle is loaded at runtime rather than linked, so a build without
// Sparkle.framework in Contents/Frameworks still runs — it simply reports the
// updater as unavailable. That keeps `wails dev` and unsigned local builds
// working, where an updater could not function anyway.
//
// Built only under the `sparkle` build tag; see driver_sparkle.go.

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#include "sparkle_darwin.h"
#include "_cgo_export.h"

static id gController;          // SPUStandardUpdaterController
static id gDelegate;            // DSSSparkleDelegate
static NSString *gLatest;
static NSString *gNotes;
static NSString *gDownloadURL;
static BOOL gUpdateAvailable;

static char *dupstr(NSString *s) {
    if (s == nil) return NULL;
    const char *c = [s UTF8String];
    return c ? strdup(c) : NULL;
}

static Class sparkleClass(NSString *name) {
    Class c = NSClassFromString(name);
    if (c != nil) return c;
    // Load the framework from the bundle on first use.
    NSString *path = [[[NSBundle mainBundle] privateFrameworksPath]
                      stringByAppendingPathComponent:@"Sparkle.framework"];
    NSBundle *fw = [NSBundle bundleWithPath:path];
    if (fw != nil && [fw load]) return NSClassFromString(name);
    return nil;
}

bool dss_sparkle_available(void) {
    return sparkleClass(@"SPUStandardUpdaterController") != nil;
}

// DSSSparkleDelegate forwards Sparkle's lifecycle to Go, which reports it to
// the update service. The events are what make a broken release visible:
// without them a failed install is indistinguishable from nobody updating.
@interface DSSSparkleDelegate : NSObject
@end

@implementation DSSSparkleDelegate

- (void)updater:(id)updater didFindValidUpdate:(id)item {
    @try {
        gLatest = [[item valueForKey:@"displayVersionString"] copy];
        gNotes = [[item valueForKey:@"itemDescription"] copy];
        gDownloadURL = [[[item valueForKey:@"fileURL"] absoluteString] copy];
        gUpdateAvailable = YES;
    } @catch (NSException *e) { /* a future Sparkle may rename these */ }
    goSparkleEvent((char *)"didFindValidUpdate",
                   (char *)[(gLatest ?: @"") UTF8String], (char *)"");
}

- (void)updaterDidNotFindUpdate:(id)updater {
    gUpdateAvailable = NO;
    goSparkleEvent((char *)"didNotFindUpdate", (char *)"", (char *)"");
}

- (void)updater:(id)updater userDidSkipThisVersion:(id)item {
    goSparkleEvent((char *)"userDidSkip", (char *)"", (char *)"");
}

- (void)updater:(id)updater willInstallUpdate:(id)item {
    goSparkleEvent((char *)"willInstallUpdate", (char *)"", (char *)"");
}

- (void)updater:(id)updater didFinishUpdateCycleForUpdateCheck:(NSInteger)check error:(NSError *)error {
    goSparkleEvent((char *)"didFinishUpdateCycle", (char *)"",
                   (char *)[([error localizedDescription] ?: @"") UTF8String]);
}

@end

char *dss_sparkle_start(const char *feed_url) {
    @autoreleasepool {
        Class cls = sparkleClass(@"SPUStandardUpdaterController");
        if (cls == nil) return dupstr(@"Sparkle.framework is not present in this build");
        if (gController != nil) return NULL;

        gDelegate = [DSSSparkleDelegate new];
        // startingUpdater:NO so the feed can be set before the first check.
        gController = [[cls alloc] initWithStartingUpdater:NO
                                          updaterDelegate:gDelegate
                                       userDriverDelegate:nil];
        id updater = [gController valueForKey:@"updater"];
        if (feed_url != NULL && *feed_url) {
            @try {
                [updater setValue:[NSURL URLWithString:@(feed_url)] forKey:@"feedURL"];
            } @catch (NSException *e) { /* SUFeedURL from Info.plist is used instead */ }
        }
        NSError *err = nil;
        @try {
            [gController performSelector:@selector(startUpdater)];
        } @catch (NSException *e) {
            return dupstr([e reason] ?: @"Sparkle failed to start");
        }
        (void)err;
        return NULL;
    }
}

char *dss_sparkle_check(bool user_initiated) {
    @autoreleasepool {
        if (gController == nil) return dupstr(@"the updater has not started");
        dispatch_async(dispatch_get_main_queue(), ^{
            if (user_initiated) {
                [gController performSelector:@selector(checkForUpdates:) withObject:nil];
            } else {
                id updater = [gController valueForKey:@"updater"];
                [updater performSelector:@selector(checkForUpdatesInBackground)];
            }
        });
        return NULL;
    }
}

char *dss_sparkle_install(void) {
    @autoreleasepool {
        if (gController == nil) return dupstr(@"the updater has not started");
        // Sparkle drives download, signature verification, atomic replacement
        // and relaunch itself; there is nothing for us to do but ask.
        dispatch_async(dispatch_get_main_queue(), ^{
            [gController performSelector:@selector(checkForUpdates:) withObject:nil];
        });
        return NULL;
    }
}

char *dss_sparkle_state_json(void) {
    @autoreleasepool {
        NSDictionary *d = @{
            @"updateAvailable": gUpdateAvailable ? @YES : @NO,
            @"latestVersion":   gLatest ?: @"",
            @"releaseNotes":    gNotes ?: @"",
            @"downloadURL":     gDownloadURL ?: @"",
        };
        NSData *j = [NSJSONSerialization dataWithJSONObject:d options:0 error:nil];
        return dupstr([[NSString alloc] initWithData:j encoding:NSUTF8StringEncoding]);
    }
}
