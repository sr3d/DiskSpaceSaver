#ifndef DSS_SPARKLE_H
#define DSS_SPARKLE_H

#include <stdbool.h>

// Returns NULL on success or a malloc'd error string. Safe to call when
// Sparkle.framework is absent: every function reports itself unavailable
// rather than failing to link, because the framework is loaded at runtime.
char *dss_sparkle_start(const char *feed_url);
char *dss_sparkle_check(bool user_initiated);
char *dss_sparkle_install(void);
bool  dss_sparkle_available(void);
char *dss_sparkle_state_json(void);

#endif
