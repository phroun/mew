/* cmdsecond_smoke.c - faithful reproduction of demoapp "New Window".
 *
 * conn1 builds a window with a button (action=newwin) and subscribes.
 * When the host clicks that button, the command handler - running on
 * conn1's EVENT thread - dials a SECOND connection and builds on it, the
 * exact threading of openSecondary. Prints OK when the second window is
 * built. Endpoint is argv[1]. */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"
#include "scripts.h"

#include <stdio.h>
#include <stdlib.h>
#include <time.h>

static const char *g_ep;
static volatile int g_done = 0;
static volatile int g_fail = 0;

static void nap(int ms) {
    struct timespec ts = {ms / 1000, (long)(ms % 1000) * 1000000L};
    nanosleep(&ts, NULL);
}

/* Runs on conn1's event thread when the button is clicked. */
static void on_newwin(void *ud) {
    (void)ud;
    kt_conn *c2 = kt_dial(g_ep, "App 1");
    if (!c2) { g_fail = 1; g_done = 1; return; }
    char *s = secondary_build_script(1); /* the REAL secondary build */
    kt_ui *u = kt_build(c2, s);
    free(s);
    if (!u) { g_fail = 1; g_done = 1; return; }
    for (int i = 0; i < 10; i++) kt_exec(c2, "tile");
    kt_ui_free(u);
    g_done = 1;
}

int main(int argc, char **argv) {
    g_ep = argc > 1 ? argv[1] : "tls://127.0.0.1:9797";

    kt_conn *c1 = kt_dial(g_ep, "Primary");
    if (!c1) { fprintf(stderr, "dial1 failed\n"); return 1; }
    kt_ui *u1 = kt_build(c1,
        "w=new window title=\"One\" width=220 height=120 children={\n"
        "  p=new panel children={ b=new button caption=\"New\" action=newwin }\n"
        "}\n"
        "wb=w.p.b\n");
    if (!u1) { fprintf(stderr, "build1 failed\n"); return 1; }

    kt_on_command(c1, "newwin", on_newwin, NULL);

    /* Wait (up to ~15s) for the host to click the button -> command. */
    for (int i = 0; i < 300 && !g_done; i++) nap(50);
    if (!g_done) { fprintf(stderr, "command never fired\n"); return 1; }
    if (g_fail) { fprintf(stderr, "second connection failed (reproduced the stall)\n"); return 1; }

    printf("OK\n");
    fflush(stdout);
    return 0;
}
