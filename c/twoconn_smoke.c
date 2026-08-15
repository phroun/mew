/* twoconn_smoke.c - reproduce the two-connection TLS stall.
 *
 * Mimics the demoapp "New Window" path: dial one connection and build,
 * then dial a SECOND connection and build on it while its reader thread
 * is already blocked in SSL_read. Before the SSL serialization fix this
 * corrupted the second connection's outbound TLS record and the build
 * batch never reached the host (build2 hangs / fails). Prints OK on
 * success. Endpoint is argv[1]. */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <stdio.h>
#include <stdlib.h>
#include <time.h>

static void nap(int ms) {
    struct timespec ts = {ms / 1000, (long)(ms % 1000) * 1000000L};
    nanosleep(&ts, NULL);
}

int main(int argc, char **argv) {
    const char *ep = argc > 1 ? argv[1] : "tls://127.0.0.1:9797";

    kt_conn *c1 = kt_dial(ep, "Primary");
    if (!c1) { fprintf(stderr, "dial1 failed\n"); return 1; }
    kt_ui *u1 = kt_build(c1,
        "w=new window title=\"One\" width=220 height=120 children={new label caption=\"hi\"}");
    if (!u1) { fprintf(stderr, "build1 failed\n"); return 1; }

    /* Second connection, as demoapp's openSecondary does. */
    kt_conn *c2 = kt_dial(ep, "App 1");
    if (!c2) { fprintf(stderr, "dial2 failed\n"); return 1; }

    /* Let conn2's reader thread settle into a blocking SSL_read, so the
     * build below writes concurrently with an in-flight read. */
    nap(300);

    kt_ui *u2 = kt_build(c2,
        "w=new window title=\"Two\" width=260 height=140 children={\n"
        "  p=new panel layout=vbox children={\n"
        "    new label caption=\"secondary application window content\"\n"
        "    new button caption=\"a button\"\n"
        "    new textinput\n"
        "  }\n"
        "}\n");
    if (!u2) { fprintf(stderr, "build2 failed (reproduced the stall)\n"); return 1; }

    /* Hammer conn2 with more request/replies to stress read+write overlap. */
    for (int i = 0; i < 20; i++) {
        if (kt_exec(c2, "tile") != 0) { fprintf(stderr, "exec2 failed at %d\n", i); return 1; }
    }

    printf("OK\n");
    fflush(stdout);
    kt_ui_free(u1);
    kt_ui_free(u2);
    return 0;
}
