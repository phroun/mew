/* mewdemo.c - the smallest KittyTK app that hosts a mew editor.
 *
 * A single main window whose only child is an "editor" control, which
 * fills the window. The editor is the mew text editor running inside a
 * PurfecTerm display surface (server-side); typing into it just works
 * through the normal focus/paint flow.
 *
 * The "editor" trinket type only exists when the desktop host is built
 * with the `mew` build tag, so run a host that has it:
 *
 *   # terminal 1: a mew-capable desktop host
 *   go run -tags mew ./examples/kittytk-tui      (or -tags 'sdl mew' ./examples/kittytk-sdl)
 *   # terminal 2:
 *   make mewdemo && ./mewdemo
 *
 * A single child in a window is sized to fill it, so no explicit sizing
 * is needed - the editor matches the window (compare ptydriver_smoke.c's
 * `children={ term=new terminal }`).
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <stdio.h>
#include <stdlib.h>
#include <time.h>

typedef struct {
    volatile int quit;
} App;

/* Window closed: end the app. */
static void on_closed(const kt_event *ev, void *ud) { (void)ev; ((App *)ud)->quit = 1; }

int main(void) {
    char *path = kt_default_socket_path();

    App a = {0};
    kt_conn *conn = kt_dial(path, "Mew Editor");
    if (!conn) {
        fprintf(stderr, "cannot reach display service at %s\n", path);
        fprintf(stderr, "start a mew-capable desktop first:\n");
        fprintf(stderr, "  go run -tags mew ./examples/kittytk-tui\n");
        free(path);
        return 1;
    }

    /* One build: a main window whose sole child is an editor control.
     * The single child fills the window, so it is sized to match it. */
    kt_ui *ui = kt_build(conn,
        "w=new window title=\"Mew Editor (C)\" width=640 height=400 main children={\n"
        "  ed=new editor\n"
        "}\n");
    if (!ui) {
        fprintf(stderr, "build failed - is the host built with -tags mew?\n");
        kt_close(conn);
        free(path);
        return 1;
    }

    kt_on(conn, kt_ui_id(ui, "w"), "window_closed", on_closed, &a);
    kt_ui_free(ui);

    while (!a.quit && !kt_is_closed(conn)) {
        struct timespec ts = {0, 100 * 1000 * 1000};  /* 100ms */
        nanosleep(&ts, NULL);
    }
    kt_close(conn);
    free(path);
    return 0;
}
