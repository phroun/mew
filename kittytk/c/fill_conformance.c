/* fill_conformance.c - what a C application writes to serve a query.
 *
 * It never parses. The statement the display sent is taken apart before the
 * callback sees it, so the callback reads fields off a struct. And it never
 * has to hold the whole answer: records go out in batches as they accumulate.
 *
 * A socketpair stands in for the display: one end is the client's, and a
 * thread on the other end reads batches, answers each with a reply, and keeps
 * what it was sent. So this drives the real inbound thread, the real dispatch
 * and the real sink rather than a stub of any of them.
 *
 * The Go side of this is client/00_a_query_reaches_the_app_taken_apart_test.go
 * and the Python side python/tests/test_hosted_query.py; the three are meant
 * to stay recognisably the same test.
 *
 * stdout: one line per failure, then `checks N` and `DONE`.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

#include <sys/socket.h>

static int failures = 0;
static int checks = 0;

static void expect(int cond, const char *what) {
    checks++;
    if (!cond) { printf("FAIL %s\n", what); failures++; }
}

static void expect_str(const char *got, const char *want, const char *what) {
    checks++;
    if (!got || strcmp(got, want) != 0) {
        printf("FAIL %s\n  got  %s\n  want %s\n", what, got ? got : "(null)", want);
        failures++;
    }
}

/* --- the display at the other end ------------------------------------- */

static kt_mutex log_mu;
static char **sent;          /* every batch the client wrote, `end` stripped */
static int nsent;
static int display_fd;

static void log_batch(const char *text) {
    kt_mutex_lock(&log_mu);
    sent = realloc(sent, (nsent + 1) * sizeof(char *));
    sent[nsent++] = strdup(text);
    kt_mutex_unlock(&log_mu);
}

static int sent_count(void) {
    kt_mutex_lock(&log_mu);
    int n = nsent;
    kt_mutex_unlock(&log_mu);
    return n;
}

/* since joins every batch written after the nth, one statement per line. */
static char *since(int n) {
    kt_buf b;
    memset(&b, 0, sizeof b);
    kt_mutex_lock(&log_mu);
    for (int i = n; i < nsent; i++) {
        if (i > n) buf_put(&b, '\n');
        buf_puts(&b, sent[i]);
    }
    kt_mutex_unlock(&log_mu);
    char *out = buf_dup(&b);
    free(b.p);
    return out;
}

static void display_write(const char *text) {
    size_t n = strlen(text);
    while (n) {
        ssize_t w = send(display_fd, text, n, 0);
        if (w <= 0) return;
        text += w; n -= (size_t)w;
    }
}

/* The display end: every line the application wrote, kept in order. Lines
   rather than batches, because a socket has no message boundaries -- and what
   is under test is the text, which is the same either way. */
static void *display_loop(void *arg) {
    (void)arg;
    kt_buf line;
    memset(&line, 0, sizeof line);
    for (;;) {
        char ch;
        ssize_t r = recv(display_fd, &ch, 1, 0);
        if (r <= 0) break;
        if (ch != '\n') { buf_put(&line, ch); continue; }
        char *text = buf_dup(&line);
        free(line.p);
        memset(&line, 0, sizeof line);
        if (*text) log_batch(text);
        free(text);
    }
    return NULL;
}

/* --- the application -------------------------------------------------- */

static kt_conn *conn;
static kt_source *source;
static uint64_t seen_query;
static int seen_have, seen_need;
static char seen_from_name[128], seen_fields[128], seen_source[64];
static long long seen_from_key, seen_to_key;
static int seen_sort_levels;
static int ended_once;
static int served_windows;

static void copy_request(kt_query *q, const kt_qfill *req) {
    seen_query = kt_query_id(q);
    seen_have = req->have;
    seen_need = req->need;
    const kt_value *v = kt_bag_get(&req->from, "name");
    snprintf(seen_from_name, sizeof seen_from_name, "%s", v && v->sval ? v->sval : "");
    v = kt_bag_key(&req->from);
    seen_from_key = v ? v->ival : -1;
    v = kt_bag_key(&req->to);
    seen_to_key = v ? v->ival : -1;
    seen_fields[0] = '\0';
    for (int i = 0; i < req->fields.n; i++) {
        if (i) strncat(seen_fields, ",", sizeof seen_fields - strlen(seen_fields) - 1);
        strncat(seen_fields, req->fields.v[i].name,
                sizeof seen_fields - strlen(seen_fields) - 1);
    }
    snprintf(seen_source, sizeof seen_source, "%s",
             req->spec && req->spec->source ? req->spec->source : "");
    seen_sort_levels = req->spec ? req->spec->nsort : -1;
}

static void fill_two(kt_query *q, const kt_qfill *req, kt_fill *sink, void *ud) {
    (void)ud;
    copy_request(q, req);
    kt_fill_ordered(sink);
    kt_value fields[2];
    fields[0] = kt_vstr("name", "src/parser.go");
    fields[1] = kt_vint("size", 1024);
    kt_fill_record(sink, kt_vint("", 17), fields, 2);
    fields[0] = kt_vstr("name", "src/window.go");
    fields[1] = kt_vint("size", 2048);
    kt_fill_record(sink, kt_vint("", 42), fields, 2);
    kt_value mark[2] = { kt_vstr("name", "src/window.go"), kt_vint("key", 42) };
    kt_fill_done(sink, mark, 2);
}

static void fill_simplest(kt_query *q, const kt_qfill *req, kt_fill *sink, void *ud) {
    (void)q; (void)req; (void)ud;
    served_windows++;
    kt_fill_record(sink, kt_vstr("", "a"), NULL, 0);
    kt_fill_exhausted(sink);
}

static void fill_refuses(kt_query *q, const kt_qfill *req, kt_fill *sink, void *ud) {
    (void)q; (void)req; (void)ud;
    kt_fill_fail(sink, "no records past \"build.sh\"");
}

static void fill_ends_once(kt_query *q, const kt_qfill *req, kt_fill *sink, void *ud) {
    (void)q; (void)req; (void)ud;
    kt_fill_exhausted(sink);
    /* And that is the end of the sink: it was released by the ending it was
       given, so there is nothing here to answer with a second time. */
    ended_once = 1;
}

#define LONG_RECORDS 400

static void fill_long(kt_query *q, const kt_qfill *req, kt_fill *sink, void *ud) {
    (void)q; (void)req; (void)ud;
    kt_fill_ordered(sink);
    char name[128];
    for (int i = 0; i < LONG_RECORDS; i++) {
        snprintf(name, sizeof name, "file-%03d-%.80s", i,
                 "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx");
        kt_value f = kt_vstr("name", name);
        kt_fill_record(sink, kt_vint("", i), &f, 1);
    }
    kt_fill_exhausted(sink);
}

static int respec_told;
static int respec_levels;
static char respec_op[16];

static void on_respec(kt_query *q, const kt_qspec *spec, void *ud) {
    (void)q; (void)ud;
    respec_told++;
    respec_levels = spec->nsort;
    snprintf(respec_op, sizeof respec_op, "%s",
             spec->filter && spec->filter->nchildren
                 ? spec->filter->children[0].op : "");
}

static int dropped_told;

static void on_dropped(kt_query *q, void *ud) { (void)q; (void)ud; dropped_told++; }

static char other_text[256];

static void on_statement(kt_query *q, const char *text, void *ud) {
    (void)q; (void)ud;
    snprintf(other_text, sizeof other_text, "%s", text);
}

/* --- driving it ------------------------------------------------------- */

/* ask sends one statement from the display and waits for the answer to land:
   the inbound thread is a thread, so there is nothing to synchronise on but
   what comes back. */
static void say(const char *text) {
    kt_buf b;
    memset(&b, 0, sizeof b);
    buf_puts(&b, text);
    buf_puts(&b, "\n");
    char *src = buf_dup(&b);
    free(b.p);
    display_write(src);
    free(src);
}

static void settle(void) {
    struct timespec ts = {0, 50000000};
    nanosleep(&ts, NULL);
}

/* ask sends one statement from the display and waits for the whole answer to
   land: the inbound thread is a thread, so there is nothing to synchronise on
   but what comes back, and an answer is over when its terminator arrives. */
static void ask(const char *text, int n) {
    say(text);
    for (int i = 0; i < 4000; i++) {
        char *so_far = since(n);
        int done = strstr(so_far, " complete") != NULL;
        free(so_far);
        if (done) return;
        struct timespec ts = {0, 1000000};
        nanosleep(&ts, NULL);
    }
}

/* serve points the one source at a different handler and opens a query, the
   way a display would. Returns the id the application minted for it. */
static uint64_t serve(kt_fill_cb cb, const char *extra) {
    kt_mutex_lock(&conn->hmu);
    source->fill = cb;
    kt_mutex_unlock(&conn->hmu);
    int n = sent_count();
    kt_buf b;
    memset(&b, 0, sizeof b);
    buf_puts(&b, "q=new query source=\"files\" sort={ name natural } ");
    buf_puts(&b, extra);
    buf_puts(&b, "\nend");
    char *src = buf_dup(&b);
    free(b.p);
    ask(src, n);
    free(src);
    char *first = NULL;
    kt_mutex_lock(&log_mu);
    if (nsent > n) first = strdup(sent[n]);
    kt_mutex_unlock(&log_mu);
    uint64_t id = 0;
    if (first && !strncmp(first, "reply q=", 8)) id = strtoull(first + 8, NULL, 10);
    free(first);
    return id;
}

int main(void) {
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) != 0) { perror("socketpair"); return 2; }
    display_fd = sv[1];
    kt_mutex_init(&log_mu);

    conn = calloc(1, sizeof *conn);
    conn->fd = sv[0];
    kt_mutex_init(&conn->write_mu);
    kt_mutex_init(&conn->rmu); kt_cond_init(&conn->rcv);
    kt_mutex_init(&conn->emu); kt_cond_init(&conn->ecv);
    kt_mutex_init(&conn->imu); kt_cond_init(&conn->icv);
    kt_mutex_init(&conn->hmu);

    kt_thread dth, rth, eth, ith;
    kt_thread_create(&dth, display_loop, NULL);
    kt_thread_create(&rth, read_loop, conn);
    kt_thread_create(&eth, event_loop, conn);
    kt_thread_create(&ith, inbound_loop, conn);

    /* Registering a source says nothing on the wire: it is a name, not an
       object. */
    source = kt_host_source(conn, "files", fill_two, NULL);
    expect(source != NULL, "the source was registered");
    expect_str(kt_source_name(source), "files", "the source knows its name");
    settle();
    expect(sent_count() == 0, "registering a source wrote to the wire");

    /* The display opens the query and the application names it, and the reply
       goes out before any record, because the reply is what names a query the
       display has not heard of yet. */
    int n = sent_count();
    uint64_t q = serve(fill_two, "from={ name \"README.md\"; key 17 }"
                       " to={ name \"build.sh\"; key 42 } have=30 need=50"
                       " fields={ name; size }");
    expect(q == 1, "the application named the query");
    expect(seen_query == q, "the handler was given the query");
    expect(seen_have == 30 && seen_need == 50, "have and need came through");
    expect_str(seen_from_name, "README.md", "the boundary's fields came through");
    expect(seen_from_key == 17 && seen_to_key == 42, "both boundaries' keys came through");
    expect_str(seen_fields, "name,size", "this window's fields came through");
    expect_str(seen_source, "files", "the spec came with the window");
    expect(seen_sort_levels == 1, "the spec's sort came with the window");
    char *answer = since(n);
    expect_str(answer,
        "reply q=1\nend\n"
        "result 1 fields={ key 17; name \"src/parser.go\"; size 1024 }\n"
        "result 1 fields={ key 42; name \"src/window.go\"; size 2048 }\n"
        "result 1 complete ordered watermark={ name \"src/window.go\"; key 42 }",
        "the reply comes before the records");
    free(answer);

    /* The display restating the sequence is a new generation of the same
       query: the spec changes underneath, and the next window carries it. */
    kt_source_on_respec(source, on_respec, NULL);
    say("set 1 sort={ size desc; name fold } filter={ ge size 1024 }\nend");
    for (int i = 0; i < 2000 && !respec_told; i++) {
        struct timespec ts = {0, 1000000};
        nanosleep(&ts, NULL);
    }
    expect(respec_told == 1, "the application was told the sequence changed");
    expect(respec_levels == 2, "the new sort came through");
    expect_str(respec_op, "ge", "the new filter came through");
    n = sent_count();
    ask("query 1 have=0 need=2\nend", n);
    expect(seen_sort_levels == 2, "the next window carried the new spec");

    /* Anything this library does not understand reaches the application
       whole, so what it does not implement is still reachable. */
    kt_source_on_statement(source, on_statement, NULL);
    say("do 1 cover handle=3 from={ key 1 } to={ key 200 }\nend");
    for (int i = 0; i < 2000 && !*other_text; i++) {
        struct timespec ts = {0, 1000000};
        nanosleep(&ts, NULL);
    }
    expect_str(other_text, "do 1 cover handle=3 from={ key 1 } to={ key 200 }",
               "what the library does not know reaches the application");

    /* Letting the query go is how the application learns it may drop the
       records it was holding for it. */
    kt_source_on_dropped(source, on_dropped, NULL);
    say("destroy 1\nend");
    for (int i = 0; i < 2000 && !dropped_told; i++) {
        struct timespec t = {0, 1000000};
        nanosleep(&t, NULL);
    }
    expect(dropped_told == 1, "the application was told the query was let go");
    n = sent_count();
    say("query 1 have=0 need=1\nend");
    settle();
    char *after = since(n);
    expect(strstr(after, "error text=") == after && strstr(after, "no query of mine") != NULL,
           "a query that was let go was served anyway");
    free(after);

    /* A display may address a query it opened in the same batch, without
       waiting for the reply that names it. */
    n = sent_count();
    served_windows = 0;
    kt_mutex_lock(&conn->hmu);
    source->fill = fill_simplest;
    kt_mutex_unlock(&conn->hmu);
    ask("q=new query source=\"files\" have=0 need=5\nquery q have=5 need=10\nend", n);
    settle();
    expect(served_windows == 2, "a query is addressable in the batch that made it");

    /* The least an implementation can do: ignore every hint, send everything,
       say so. It says nothing about order, which leaves the display to sort. */
    n = sent_count();
    q = serve(fill_simplest, "have=0 need=10");
    answer = since(n + 2);
    char tmp[256];
    snprintf(tmp, sizeof tmp,
             "result %llu fields={ key \"a\" }\nresult %llu complete exhausted",
             (unsigned long long)q, (unsigned long long)q);
    expect_str(answer, tmp, "the simplest answer is everything and exhausted");
    free(answer);

    /* A refusal is an answer. */
    n = sent_count();
    q = serve(fill_refuses, "have=0 need=10");
    answer = since(n + 2);
    snprintf(tmp, sizeof tmp,
             "result %llu complete error=\"no records past \\\"build.sh\\\"\"",
             (unsigned long long)q);
    expect_str(answer, tmp, "a refusal is an answer");
    free(answer);

    /* An answer ends once: one ending, one message, and the sink is gone. */
    n = sent_count();
    q = serve(fill_ends_once, "have=0 need=1");
    settle();
    expect(ended_once == 1, "the handler ran");
    char *once = since(n + 2);
    snprintf(tmp, sizeof tmp, "result %llu complete exhausted", (unsigned long long)q);
    expect_str(once, tmp, "one ending is the whole answer");
    free(once);

    /* The answer goes out as it accumulates rather than all at the end, so a
       window larger than one message is neither held in memory nor one
       uninterruptible stretch of work. */
    n = sent_count();
    q = serve(fill_long, "have=0 need=400");
    int batches = sent_count() - n - 2;
    expect(batches > 1, "a long answer goes out in batches");
    answer = since(n + 2);
    int lines = *answer ? 1 : 0;
    for (char *p = answer; *p; p++) if (*p == '\n') lines++;
    expect(lines == LONG_RECORDS + 1, "every record arrives, once, with a terminator");
    expect(strstr(answer, "key 0;") != NULL && strstr(answer, "key 399;") != NULL,
           "the first and last records are both there");
    snprintf(tmp, sizeof tmp, "\nresult %llu complete ordered", (unsigned long long)q);
    expect(strstr(answer, tmp) != NULL, "the terminator is last");
    free(answer);

    /* A source this application does not serve is refused, and the refusal is
       what the batch is answered with. */
    n = sent_count();
    say("q=new query source=\"ledgers\" have=0 need=1\nend");
    settle();
    char *refusal = since(n);
    expect(strstr(refusal, "error text=") == refusal && strstr(refusal, "ledgers") != NULL,
           "an unknown source is refused");
    free(refusal);

    printf("checks %d\n", checks);
    printf("DONE\n");
    return failures ? 1 : 0;
}
