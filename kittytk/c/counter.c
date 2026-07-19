/* counter.c - the smallest useful KittyTK app in C.
 *
 * A main window with a label and a button; each click increments the
 * number shown on the label. The whole app is: dial, build, subscribe to
 * the button's click, update the label - the essential KittyTK pattern.
 *
 *   # terminal 1: a desktop host
 *   go run ./examples/kittytk-tui        (or -tags sdl ./examples/kittytk-sdl)
 *   # terminal 2:
 *   make counter && ./counter
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <stdio.h>
#include <stdlib.h>
#include <time.h>

typedef struct {
    kt_conn *conn;
    uint64_t label;   /* the label whose caption we rewrite */
    int count;
    volatile int quit;
} App;

/* Button clicked: bump the counter and rewrite the label's caption. */
static void on_click(const kt_event *ev, void *ud) {
    (void)ev;
    App *a = ud;
    a->count++;
    char args[64];
    snprintf(args, sizeof args, "caption=\"Count: %d\"", a->count);
    kt_set(a->conn, a->label, args);
}

/* Window closed: end the app. */
static void on_closed(const kt_event *ev, void *ud) { (void)ev; ((App *)ud)->quit = 1; }

int main(void) {
    char *path = kt_default_socket_path();

    App a = {0};
    a.conn = kt_dial(path, "Counter");
    if (!a.conn) {
        fprintf(stderr, "cannot reach display service at %s\n", path);
        fprintf(stderr, "start a desktop first: go run ./examples/kittytk-tui\n");
        return 1;
    }

    /* One build: a main window with a label over a button. The trailing
     * lines surface the inner names so we can address them by handle. */
    kt_ui *ui = kt_build(a.conn,
        "w=new window title=\"Counter (C)\" width=240 height=120 main children={\n"
        "  p=new panel layout=vbox spacing=8 children={\n"
        "    count=new label caption=\"Count: 0\"\n"
        "    inc=new button caption=\"Increment\"\n"
        "  }\n"
        "}\n"
        "label=w.p.count\n"
        "btn=w.p.inc\n");
    if (!ui) { fprintf(stderr, "build failed\n"); return 1; }

    a.label = kt_ui_id(ui, "label");
    kt_on(a.conn, kt_ui_id(ui, "btn"), "click", on_click, &a);
    kt_on(a.conn, kt_ui_id(ui, "w"), "window_closed", on_closed, &a);
    kt_ui_free(ui);

    while (!a.quit && !kt_is_closed(a.conn)) {
        struct timespec ts = {0, 100 * 1000 * 1000};  /* 100ms */
        nanosleep(&ts, NULL);
    }
    kt_close(a.conn);
    free(path);
    return 0;
}
