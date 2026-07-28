/* interop_smoke.c - a C app driving a REAL Go display host, run by the Go
 * interop harness (the C mirror of interop_smoke.py). Proves, over a live
 * socket: build + subscribe (C -> host), write-through (C -> host), and
 * host -> C toggle/command events.
 *
 * stdout markers: READY / TOGGLE ok / COMMAND ok / DONE. */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t cv = PTHREAD_COND_INITIALIZER;
static int got_toggle = 0, got_command = 0;

static void on_toggle(const kt_event *ev, void *ud) {
    (void)ud;
    printf("TOGGLE ok state=%d\n", (int)kt_event_flag(ev, "checked"));
    fflush(stdout);
    pthread_mutex_lock(&mu);
    got_toggle = 1;
    pthread_cond_signal(&cv);
    pthread_mutex_unlock(&mu);
}

static void on_command(void *ud) {
    (void)ud;
    printf("COMMAND ok\n");
    fflush(stdout);
    pthread_mutex_lock(&mu);
    got_command = 1;
    pthread_cond_signal(&cv);
    pthread_mutex_unlock(&mu);
}

int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr, "usage: interop_smoke <socket>\n"); return 64; }

    kt_conn *c = kt_dial(argv[1], "C Interop App");
    if (!c) { fprintf(stderr, "dial failed\n"); return 1; }

    kt_ui *ui = kt_build(c,
        "w=new window title=\"C Interop\" width=320 height=160 children={\n"
        "  p=new panel layout=vbox children={\n"
        "    cb=new checkbox caption=\"remote checkbox\"\n"
        "    inp=new textinput\n"
        "    btn=new button caption=\"Go\" action=remote.act\n"
        "  }\n"
        "}\n"
        "wcb=w.p.cb\n"
        "winp=w.p.inp\n"
        "wbtn=w.p.btn\n");
    if (!ui) { fprintf(stderr, "build failed\n"); return 1; }

    const char *keys[] = {"w", "wcb", "winp", "wbtn"};
    for (int i = 0; i < 4; i++)
        if (kt_ui_id(ui, keys[i]) == 0) { printf("FAIL missing id %s\n", keys[i]); return 1; }

    /* app -> host write-through */
    char *q = kt_quote("over the wire");
    char args[64];
    snprintf(args, sizeof args, "text=%s", q);
    free(q);
    kt_set(c, kt_ui_id(ui, "winp"), args);

    kt_on(c, kt_ui_id(ui, "wcb"), "toggle", on_toggle, NULL);
    kt_on_command(c, "remote.act", on_command, NULL);

    /* Introspection (D24): the host describes its wire vocabulary. */
    kt_vocab *v = kt_describe(c);
    if (!v) { printf("FAIL describe: no vocabulary\n"); return 1; }
    int have_enabled = 0;
    for (int i = 0; i < v->ncommon; i++)
        if (strcmp(v->common[i].name, "enabled") == 0) have_enabled = 1;
    const kt_prop *caption = NULL;
    for (int i = 0; i < v->ntypes && !caption; i++)
        if (strcmp(v->types[i].name, "button") == 0)
            for (int j = 0; j < v->types[i].nprops; j++)
                if (strcmp(v->types[i].props[j].name, "caption") == 0)
                    caption = &v->types[i].props[j];
    if (!have_enabled) { printf("FAIL describe: no common 'enabled'\n"); kt_vocab_free(v); return 1; }
    if (!caption || strcmp(caption->kind, "string") != 0 || !caption->doc[0]) {
        printf("FAIL describe: button.caption missing/undescribed\n");
        kt_vocab_free(v); return 1;
    }
    printf("DESCRIBE ok types=%d\n", v->ntypes);
    kt_vocab_free(v);

    printf("READY\n");
    fflush(stdout);

    /* wait for both events (10s) */
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += 10;
    pthread_mutex_lock(&mu);
    while (!(got_toggle && got_command)) {
        if (pthread_cond_timedwait(&cv, &mu, &ts) != 0) break;
    }
    int done = got_toggle && got_command;
    pthread_mutex_unlock(&mu);

    if (!done) { printf("TIMEOUT\n"); return 2; }
    printf("DONE\n");
    fflush(stdout);
    kt_ui_free(ui);
    kt_close(c);
    return 0;
}
