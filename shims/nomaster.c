/* LD_PRELOAD shim: make ThroughTek/Kalay master-server DNS lookups FAIL, while
 * leaving all other resolution (and LAN broadcast, which doesn't use DNS) alone.
 *
 * Used to test whether the Owlet Cam can be reached with NO internet / no
 * Owlet/ThroughTek servers -- i.e. LAN-only. If IOTC_Connect_ByUIDEx still
 * succeeds with these blocked, Phase 1 (the cloud rendezvous) is not required
 * when the camera is already on the local network.
 *
 *   gcc -shared -fPIC -o nomaster.so nomaster.c -ldl
 *   LD_PRELOAD=./nomaster.so python3 stream.py ...
 */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <netdb.h>
#include <string.h>
#include <strings.h>
#include <stdio.h>

typedef int (*getaddrinfo_fn)(const char *, const char *,
                              const struct addrinfo *, struct addrinfo **);

static int blocked(const char *node) {
    if (!node) return 0;
    return strcasestr(node, "iotcplatform") || strcasestr(node, "master") ||
           strcasestr(node, "tutk") || strcasestr(node, "kalay") ||
           strcasestr(node, "throughtek");
}

int getaddrinfo(const char *node, const char *service,
                const struct addrinfo *hints, struct addrinfo **res) {
    static getaddrinfo_fn real = NULL;
    if (!real) real = (getaddrinfo_fn)dlsym(RTLD_NEXT, "getaddrinfo");
    if (blocked(node)) {
        fprintf(stderr, "[nomaster] BLOCKED resolve: %s\n", node);
        return EAI_NONAME;   /* pretend the server does not exist */
    }
    return real(node, service, hints, res);
}
