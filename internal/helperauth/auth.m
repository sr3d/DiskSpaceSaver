#import <Foundation/Foundation.h>
#import <Security/Security.h>
#include <launch.h>
#include <stdlib.h>
#include <string.h>
#include "auth.h"

static char *dup_err(NSString *s) {
    const char *c = [s UTF8String];
    return c ? strdup(c) : strdup("unknown error");
}

// The helper runs as root and will delete files on request, so it must satisfy
// itself about who is asking. A pid alone proves nothing — pids are reused, and
// any process can connect to the socket — so the pid is only used to obtain a
// code object, which is then checked against a designated requirement naming
// both the bundle identifier and the Team ID.
char *dss_verify_peer(int pid, const char *requirement) {
    @autoreleasepool {
        SecCodeRef guest = NULL;
        SecRequirementRef req = NULL;
        char *err = NULL;

        NSDictionary *attrs = @{ (__bridge NSString *)kSecGuestAttributePid : @(pid) };
        OSStatus st = SecCodeCopyGuestWithAttributes(
            NULL, (__bridge CFDictionaryRef)attrs, kSecCSDefaultFlags, &guest);
        if (st != errSecSuccess || guest == NULL) {
            return dup_err([NSString stringWithFormat:
                @"could not identify the connecting process (OSStatus %d)", (int)st]);
        }

        st = SecRequirementCreateWithString(
            (__bridge CFStringRef)@(requirement), kSecCSDefaultFlags, &req);
        if (st != errSecSuccess || req == NULL) {
            CFRelease(guest);
            return dup_err([NSString stringWithFormat:
                @"bad code requirement (OSStatus %d)", (int)st]);
        }

        // kSecCSCheckAllArchitectures and kSecCSStrictValidate make this a full
        // check of the on-disk signature, not just the cached one.
        CFErrorRef cfErr = NULL;
        st = SecCodeCheckValidityWithErrors(
            guest, kSecCSDefaultFlags | kSecCSCheckAllArchitectures | kSecCSStrictValidate,
            req, &cfErr);
        if (st != errSecSuccess) {
            NSString *detail = @"";
            if (cfErr != NULL) {
                detail = [NSString stringWithFormat:@": %@",
                          [(__bridge NSError *)cfErr localizedDescription]];
                CFRelease(cfErr);
            }
            err = dup_err([NSString stringWithFormat:
                @"the connecting process is not a trusted copy of DiskSpaceSaver (OSStatus %d)%@",
                (int)st, detail]);
        }

        CFRelease(req);
        CFRelease(guest);
        return err;
    }
}

char *dss_launchd_sockets(const char *name, int **fds, size_t *count) {
    int *out = NULL;
    size_t n = 0;
    int rc = launch_activate_socket(name, &out, &n);
    if (rc != 0) {
        return dup_err([NSString stringWithFormat:
            @"launch_activate_socket(%s) failed: %s", name, strerror(rc)]);
    }
    *fds = out;
    *count = n;
    return NULL;
}
