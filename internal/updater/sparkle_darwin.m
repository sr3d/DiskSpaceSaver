//go:build darwin && sparkle

// Sparkle bridge.
//
// The constraint above matters: Go applies build constraints to Objective-C
// files as well as Go ones, and a package with a .m file but no cgo file is
// rejected outright. Without the tag this file is excluded, the package is
// pure Go, and the updater reports itself unavailable.
//
// Sparkle is linked, not looked up at runtime, because the tag and the
// framework travel together: the Makefile only sets the tag when the framework
// is present, and embeds it in the bundle when it is.

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <Sparkle/Sparkle.h>
#include "sparkle_darwin.h"
#include "_cgo_export.h"

static SPUStandardUpdaterController *gController;
static id gDelegate;
static NSString *gFeedURL;
static NSString *gLatest;
static NSString *gNotes;
static NSString *gDownloadURL;
static BOOL gUpdateAvailable;

static char *dupstr(NSString *s) {
    if (s == nil) return NULL;
    const char *c = [s UTF8String];
    return c ? strdup(c) : NULL;
}

// Forwards Sparkle's lifecycle to Go, which reports it to the update service.
// These events are what make a broken release visible: without them a failed
// install looks exactly like nobody choosing to update.
@interface DSSSparkleDelegate : NSObject <SPUUpdaterDelegate>
@end

@implementation DSSSparkleDelegate

// Supplying the feed here rather than through the deprecated setter keeps the
// channel selectable at runtime while Info.plist still carries a default.
- (nullable NSString *)feedURLStringForUpdater:(SPUUpdater *)updater {
    return gFeedURL.length ? gFeedURL : nil;
}

- (void)updater:(SPUUpdater *)updater didFindValidUpdate:(SUAppcastItem *)item {
    gLatest = [item.displayVersionString copy];
    gNotes = [item.itemDescription copy];
    gDownloadURL = [item.fileURL.absoluteString copy];
    gUpdateAvailable = YES;
    goSparkleEvent((char *)"didFindValidUpdate",
                   (char *)[(gLatest ?: @"") UTF8String], (char *)"");
}

- (void)updaterDidNotFindUpdate:(SPUUpdater *)updater {
    gUpdateAvailable = NO;
    goSparkleEvent((char *)"didNotFindUpdate", (char *)"", (char *)"");
}

- (void)updater:(SPUUpdater *)updater willInstallUpdate:(SUAppcastItem *)item {
    goSparkleEvent((char *)"willInstallUpdate", (char *)"",
                   (char *)[(item.displayVersionString ?: @"") UTF8String]);
}

- (void)updater:(SPUUpdater *)updater
    didFinishUpdateCycleForUpdateCheck:(SPUUpdateCheck)updateCheck
                                 error:(nullable NSError *)error {
    goSparkleEvent((char *)"didFinishUpdateCycle", (char *)"",
                   (char *)[(error.localizedDescription ?: @"") UTF8String]);
}

@end

bool dss_sparkle_available(void) { return true; }

char *dss_sparkle_start(const char *feed_url) {
    @autoreleasepool {
        if (gController != nil) return NULL;
        if (feed_url != NULL && *feed_url) gFeedURL = @(feed_url);

        gDelegate = [DSSSparkleDelegate new];
        // startingUpdater:NO so the delegate is in place before the first
        // check, which is what lets the feed URL above take effect.
        gController = [[SPUStandardUpdaterController alloc]
            initWithStartingUpdater:NO
                    updaterDelegate:gDelegate
                 userDriverDelegate:nil];
        @try {
            [gController startUpdater];
        } @catch (NSException *e) {
            return dupstr([e reason] ?: @"Sparkle failed to start");
        }
        return NULL;
    }
}

char *dss_sparkle_check(bool user_initiated) {
    @autoreleasepool {
        if (gController == nil) return dupstr(@"the updater has not started");
        dispatch_async(dispatch_get_main_queue(), ^{
            // A user-initiated check shows Sparkle's own progress and result
            // windows; a background one stays silent unless there is an update.
            if (user_initiated) {
                [gController checkForUpdates:nil];
            } else {
                [gController.updater checkForUpdatesInBackground];
            }
        });
        return NULL;
    }
}

char *dss_sparkle_install(void) {
    @autoreleasepool {
        if (gController == nil) return dupstr(@"the updater has not started");
        // Sparkle owns download, signature verification, atomic replacement and
        // relaunch; there is nothing to do here but ask it to proceed.
        dispatch_async(dispatch_get_main_queue(), ^{
            [gController checkForUpdates:nil];
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
