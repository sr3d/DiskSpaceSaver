#ifndef DSS_HELPER_AUTH_H
#define DSS_HELPER_AUTH_H

#include <stdbool.h>
#include <stddef.h>

// Verifies that the process with the given pid satisfies the code signing
// requirement. Returns NULL when it does, otherwise a malloc'd error string.
char *dss_verify_peer(int pid, const char *requirement);

// Retrieves launchd's pre-bound sockets for the named Sockets entry. Returns
// NULL on success and fills fds/count (caller frees fds), otherwise an error.
char *dss_launchd_sockets(const char *name, int **fds, size_t *count);

#endif
