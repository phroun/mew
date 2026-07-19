/* ptydriver_smoke - drives a real client-side PTY over the wire.
 *
 * Builds a terminal, attaches a PTY running $PTY_SMOKE_SHELL (a test script
 * that prints a marker then `exec cat` to echo input), prints READY, and
 * stays connected. The Go harness then checks the terminal's buffer for the
 * printed marker (output path) and types into it (input path). */
#include "kittytk.h"
#include "ptydriver.h"

#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv) {
    const char *ep = argc > 1 ? argv[1] : kt_default_endpoint();
    const char *shell = getenv("PTY_SMOKE_SHELL");

    kt_conn *c = kt_dial(ep, "ptysmoke");
    if (!c) { fprintf(stderr, "dial failed\n"); return 1; }

    kt_ui *ui = kt_build(c,
        "w=new window title=\"PTY\" width=480 height=320 children={ term=new terminal }\n"
        "t=w.term\n");
    if (!ui) { fprintf(stderr, "build failed\n"); return 1; }
    uint64_t t = kt_ui_id(ui, "t");
    if (!t) { fprintf(stderr, "no terminal id\n"); return 1; }

    kt_pty *p = kt_pty_attach(c, t, shell);
    if (!p) { fprintf(stderr, "pty attach failed\n"); return 1; }

    printf("READY\n");
    fflush(stdout);

    kt_wait_closed(c);
    kt_pty_close(p);
    kt_ui_free(ui);
    kt_close(c);
    return 0;
}
