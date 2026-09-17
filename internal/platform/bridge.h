// Declarations for the Objective-C shim. Kept free of definitions so the Go
// file that includes it may also export Go callbacks (cgo forbids having both
// C definitions and //export in one file).
#ifndef DSS_BRIDGE_H
#define DSS_BRIDGE_H

#include <stdint.h>
#include <stdbool.h>

// Caller owns every returned char* and must free() it.

// JSON array of volume descriptors.
char *dss_list_volumes(bool with_icons);

// Begin/stop delivering mount and unmount notifications to goVolumesChanged.
void dss_watch_volumes(void);
void dss_unwatch_volumes(void);

// Begin/stop taking the fourth and fifth mouse buttons out of the event
// stream and reporting them to goMouseButton.
void dss_watch_mouse_buttons(void);
void dss_unwatch_mouse_buttons(void);

// Finder integration. Each returns NULL on success or an error string.
char *dss_trash(const char *path, char **out_new_path);
char *dss_reveal(const char *const *paths, int n);
char *dss_get_info(const char *path);
char *dss_open(const char *path);
char *dss_open_url(const char *url);

// Quick Look, driven by a caller-supplied list of paths.
void dss_quicklook(const char *const *paths, int n, int index);
void dss_quicklook_close(void);
bool dss_quicklook_visible(void);

// Dock tile badge. Pass NULL or "" to clear.
void dss_set_dock_badge(const char *text);
void dss_set_dock_progress(double fraction); // <0 clears the progress bar

// The bundle identifier and Team ID of the running process, for the helper's
// designated requirement. NULL when unsigned or not bundled.
char *dss_bundle_id(void);
char *dss_team_id(void);
bool dss_is_bundled(void);

// Renders a preview thumbnail for a file as a PNG data URI, or NULL when the
// system cannot represent it. Blocks until the thumbnail is ready, so call it
// off the main thread.
char *dss_thumbnail(const char *path, int max_points, double scale);

// SMAppService, for the privileged helper. plist_name is the LaunchDaemons
// plist filename inside the bundle, e.g. "com.sr3d.diskspacesaver.helper.plist".
// Each returns NULL on success or an error string the caller must free.
char *dss_helper_register(const char *plist_name);
char *dss_helper_unregister(const char *plist_name);
// One of: "not-registered", "enabled", "requires-approval", "not-found",
// "unknown", or "unsupported" on macOS older than 13.
char *dss_helper_status(const char *plist_name);
void dss_open_login_items_settings(void);

#endif
