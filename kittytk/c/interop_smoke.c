/* interop_smoke.c - a C app driving a REAL Go display host, run by the Go
 * interop harness (the C mirror of interop_smoke.py). Proves, over a live
 * socket: build + subscribe (C -> host), write-through (C -> host), and
 * host -> C toggle/command events.
 *
 * stdout markers: READY / TOGGLE ok / COMMAND ok / DONE. */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t cv = PTHREAD_COND_INITIALIZER;
static int got_toggle = 0, got_command = 0;

/* What the store and the display answered. The blob is written in two pieces
 * and read back whole, so a byte that did not survive the wire shows up as a
 * mismatch rather than as nothing. */
static pthread_mutex_t smu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t scv = PTHREAD_COND_INITIALIZER;
static uint64_t blob_id = 0;
static long long blob_size = 0;
static int store_done = 0, read_done = 0, host_said = 0, host_dark = 0;
static unsigned char readback[1024];
static size_t readback_n = 0;

static void on_store_blob(const kt_event *ev, void *ud) {
    (void)ud;
    pthread_mutex_lock(&smu);
    kt_event_uint(ev, "blob", &blob_id);
    kt_event_int(ev, "size", &blob_size);
    pthread_cond_broadcast(&scv);
    pthread_mutex_unlock(&smu);
}
static void on_store_done(const kt_event *ev, void *ud) {
    (void)ev; (void)ud;
    pthread_mutex_lock(&smu);
    store_done = 1;
    pthread_cond_broadcast(&scv);
    pthread_mutex_unlock(&smu);
}
static void on_store_data(const kt_event *ev, void *ud) {
    (void)ud;
    size_t n = 0;
    const char *data = kt_event_text_n(ev, "data", &n);
    pthread_mutex_lock(&smu);
    if (data && readback_n + n <= sizeof readback) {
        memcpy(readback + readback_n, data, n);
        readback_n += n;
    }
    if (kt_event_flag(ev, "last") == KT_FLAG_TRUE) read_done = 1;
    pthread_cond_broadcast(&scv);
    pthread_mutex_unlock(&smu);
}
static void on_host_state(const kt_event *ev, void *ud) {
    (void)ud;
    pthread_mutex_lock(&smu);
    host_dark = kt_event_flag(ev, "dark") == KT_FLAG_TRUE;
    host_said = 1;
    pthread_cond_broadcast(&scv);
    pthread_mutex_unlock(&smu);
}

/* wait_for blocks until pred is true or five seconds pass. */
static int wait_for(const int *flag) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += 5;
    pthread_mutex_lock(&smu);
    while (!*flag)
        if (pthread_cond_timedwait(&scv, &smu, &ts) != 0) break;
    int ok = *flag;
    pthread_mutex_unlock(&smu);
    return ok;
}

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

    /* The other two objects the handshake handed over. */
    if (kt_store_id(c) == 0 || kt_host_id(c) == 0) {
        printf("FAIL handshake: store=%llu host=%llu\n",
               (unsigned long long)kt_store_id(c), (unsigned long long)kt_host_id(c));
        return 1;
    }

    /* The display answers what it is asked. */
    kt_on(c, kt_host_id(c), "host_state", on_host_state, NULL);
    kt_ask(c, kt_host_id(c), "dark");
    if (!wait_for(&host_said)) { printf("FAIL ask host dark: no answer\n"); return 1; }
    printf("ASK ok dark=%d\n", host_dark);

    /* The store: write a blob of every byte there is, in two pieces, and read
     * it back. Anything the wire mangled shows up as a mismatch. */
    unsigned char ramp[512];
    for (int i = 0; i < 512; i++) ramp[i] = (unsigned char)(i % 256);
    kt_on(c, kt_store_id(c), KT_STORE_BLOB, on_store_blob, NULL);
    kt_on(c, kt_store_id(c), KT_STORE_DONE, on_store_done, NULL);
    kt_on(c, kt_store_id(c), KT_STORE_DATA, on_store_data, NULL);

    kt_store_write(c, "c-interop", "bin", ramp, 256);
    pthread_mutex_lock(&smu);
    struct timespec bts;
    clock_gettime(CLOCK_REALTIME, &bts);
    bts.tv_sec += 5;
    while (blob_id == 0)
        if (pthread_cond_timedwait(&scv, &smu, &bts) != 0) break;
    uint64_t blob = blob_id;
    pthread_mutex_unlock(&smu);
    if (blob == 0) { printf("FAIL store write: no blob id\n"); return 1; }

    kt_blob_append(c, blob, ramp + 256, 256);
    kt_blob_read(c, blob, 0);
    if (!wait_for(&read_done)) { printf("FAIL store read: never finished\n"); return 1; }
    if (readback_n != 512 || memcmp(readback, ramp, 512) != 0) {
        printf("FAIL store read: %zu bytes back, not the 512 written\n", readback_n);
        return 1;
    }
    kt_store_list(c);
    if (!wait_for(&store_done)) { printf("FAIL store inventory: no answer\n"); return 1; }
    printf("STORE ok bytes=%zu\n", readback_n);

    /* The vocabulary says what a type does and answers, not just what it
     * holds. */
    kt_vocab *v2 = kt_describe(c);
    int host_hosted = 0, has_tile = 0, has_dark = 0, blob_appends = 0;
    for (int i = 0; v2 && i < v2->ntypes; i++) {
        if (strcmp(v2->types[i].name, "host") == 0) {
            host_hosted = v2->types[i].is_hosted;
            for (int j = 0; j < v2->types[i].ndoes; j++)
                if (strcmp(v2->types[i].does[j].name, "tile") == 0) has_tile = 1;
            for (int j = 0; j < v2->types[i].nasks; j++)
                if (strcmp(v2->types[i].asks[j].name, "dark") == 0) has_dark = 1;
        }
        if (strcmp(v2->types[i].name, "blob") == 0)
            for (int j = 0; j < v2->types[i].ndoes; j++)
                if (strcmp(v2->types[i].does[j].name, "append") == 0
                    && v2->types[i].does[j].nargs == 1)
                    blob_appends = 1;
    }
    kt_vocab_free(v2);
    if (!host_hosted || !has_tile || !has_dark || !blob_appends) {
        printf("FAIL describe: hosted=%d tile=%d dark=%d append=%d\n",
               host_hosted, has_tile, has_dark, blob_appends);
        return 1;
    }
    printf("VOCAB ok\n");

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
