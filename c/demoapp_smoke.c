/* demoapp_smoke.c - full-demo build smoke for the C client (mirror of
 * demoapp_smoke.py). Builds the whole main window + companion window +
 * dialog + MDI document over a REAL Go host and exercises the properties
 * and every desktop-action app verb. Prints OK on success. */
#include "kittytk.h"
#include "scripts.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr, "usage: demoapp_smoke <socket>\n"); return 64; }
    kt_conn *c = kt_dial(argv[1], "KittyTK Demo");
    if (!c) { fprintf(stderr, "dial failed\n"); return 1; }

    char *main_src = main_build_script();
    kt_ui *ui = kt_build(c, main_src);
    free(main_src);
    if (!ui) { fprintf(stderr, "build main failed\n"); return 1; }

    const char *keys[] = {"w", "tabs", "binput", "wfont", "dfont", "grid",
                          "bgdef", "bggreen", "bggray", "sbgdef", "sbggreen", "sbggray",
                          "mdi", "mdistatus", "mdidock", "mb", "sb"};
    for (size_t i = 0; i < sizeof keys / sizeof keys[0]; i++)
        if (kt_ui_id(ui, keys[i]) == 0) { printf("FAIL surfaced id %s missing\n", keys[i]); return 1; }

    char *pw = protocol_window_script();
    if (!kt_build(c, pw)) { printf("FAIL protocol window\n"); return 1; }
    free(pw);

    char *about = about_dialog_script();
    kt_exec(c, about);
    free(about);

    char *mc = mdi_child_script(1);
    kt_ui *child = kt_build(c, mc);
    free(mc);
    if (!child || kt_ui_id(child, "wwin") == 0) { printf("FAIL mdi child\n"); return 1; }
    char dockset[128];
    snprintf(dockset, sizeof dockset,
             "set mdidock children={e1=new dockentry caption=\"Document 1\" window=%llu}",
             (unsigned long long)kt_ui_id(child, "wwin"));
    kt_exec(c, dockset);

    uint64_t tabs = kt_ui_id(ui, "tabs");
    kt_set(c, tabs, "background=green");
    kt_set(c, tabs, "background=\"#333333\"");
    kt_set(c, tabs, "background=default");

    uint64_t win = kt_ui_id(ui, "w");
    kt_set(c, win, "font=\"tuesday12\"");
    kt_set(c, win, "denomination=32");

    const char *verbs[] = {"status text=\"hi there\"", "cut", "copy", "paste", "selectall",
                          "tile", "cascade", "theme", "desktopfont tuesday",
                          "desktopfont default", "announce_visual", "announce_speak", "rawkey"};
    for (size_t i = 0; i < sizeof verbs / sizeof verbs[0]; i++)
        if (kt_exec(c, verbs[i]) != 0) { printf("FAIL app verb %s\n", verbs[i]); return 1; }

    printf("OK\n");
    fflush(stdout);
    kt_ui_free(child);
    kt_ui_free(ui);
    kt_close(c);
    return 0;
}
