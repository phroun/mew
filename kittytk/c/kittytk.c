/* kittytk.c - implementation of the KittyTK display-protocol client in C.
 * A faithful port of the wire format and the client's read/event loops,
 * with a thin platform shim (POSIX sockets+pthreads / Windows Winsock+
 * Win32 threads) and optional TLS (compile with -DKT_TLS, needs OpenSSL). */
#define _GNU_SOURCE
#include "kittytk.h"

#include <ctype.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/* --- platform shim: sockets & threads -------------------------------- */

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#include <afunix.h> /* AF_UNIX on Windows 10+ */
#include <process.h>
#include <direct.h>
typedef SOCKET kt_socket;
#define KT_BAD_SOCKET INVALID_SOCKET
#define kt_closesocket closesocket
typedef CRITICAL_SECTION kt_mutex;
typedef CONDITION_VARIABLE kt_cond;
typedef HANDLE kt_thread;
static void kt_mutex_init(kt_mutex *m) { InitializeCriticalSection(m); }
static void kt_mutex_lock(kt_mutex *m) { EnterCriticalSection(m); }
static void kt_mutex_unlock(kt_mutex *m) { LeaveCriticalSection(m); }
static void kt_cond_init(kt_cond *c) { InitializeConditionVariable(c); }
static void kt_cond_wait(kt_cond *c, kt_mutex *m) { SleepConditionVariableCS(c, m, INFINITE); }
static void kt_cond_signal(kt_cond *c) { WakeConditionVariable(c); }
static void kt_cond_broadcast(kt_cond *c) { WakeAllConditionVariable(c); }
static int kt_cond_timedwait(kt_cond *c, kt_mutex *m, int ms) {
    return SleepConditionVariableCS(c, m, (DWORD)ms) ? 0 : 1; /* 0 ok, nonzero timeout */
}
typedef struct { void *(*fn)(void *); void *arg; } kt_thunk;
static unsigned __stdcall kt_trampoline(void *p) {
    kt_thunk t = *(kt_thunk *)p;
    free(p);
    t.fn(t.arg);
    return 0;
}
static int kt_thread_create(kt_thread *th, void *(*fn)(void *), void *arg) {
    kt_thunk *t = malloc(sizeof *t);
    t->fn = fn; t->arg = arg;
    *th = (HANDLE)_beginthreadex(NULL, 0, kt_trampoline, t, 0, NULL);
    return *th ? 0 : -1;
}
static void kt_thread_join(kt_thread th) { WaitForSingleObject(th, INFINITE); CloseHandle(th); }
static void kt_platform_init(void) {
    static int done = 0;
    if (!done) { WSADATA w; WSAStartup(MAKEWORD(2, 2), &w); done = 1; }
}
#ifdef KT_TLS
static int kt_mkdir(const char *p) { return _mkdir(p); }
#endif
#else
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <pthread.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>
typedef int kt_socket;
#define KT_BAD_SOCKET (-1)
#define kt_closesocket close
typedef pthread_mutex_t kt_mutex;
typedef pthread_cond_t kt_cond;
typedef pthread_t kt_thread;
static void kt_mutex_init(kt_mutex *m) { pthread_mutex_init(m, NULL); }
static void kt_mutex_lock(kt_mutex *m) { pthread_mutex_lock(m); }
static void kt_mutex_unlock(kt_mutex *m) { pthread_mutex_unlock(m); }
static void kt_cond_init(kt_cond *c) { pthread_cond_init(c, NULL); }
static void kt_cond_wait(kt_cond *c, kt_mutex *m) { pthread_cond_wait(c, m); }
static void kt_cond_signal(kt_cond *c) { pthread_cond_signal(c); }
static void kt_cond_broadcast(kt_cond *c) { pthread_cond_broadcast(c); }
static int kt_cond_timedwait(kt_cond *c, kt_mutex *m, int ms) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += ms / 1000;
    ts.tv_nsec += (long)(ms % 1000) * 1000000L;
    if (ts.tv_nsec >= 1000000000L) { ts.tv_sec++; ts.tv_nsec -= 1000000000L; }
    return pthread_cond_timedwait(c, m, &ts); /* 0 or ETIMEDOUT */
}
static int kt_thread_create(kt_thread *th, void *(*fn)(void *), void *arg) { return pthread_create(th, NULL, fn, arg); }
static void kt_thread_join(kt_thread th) { pthread_join(th, NULL); }
static void kt_platform_init(void) {}
#ifdef KT_TLS
static int kt_mkdir(const char *p) { return mkdir(p, 0700); }
#endif
#endif

#ifdef KT_TLS
#include <openssl/err.h>
#include <openssl/pem.h>
#include <openssl/sha.h>
#include <openssl/ssl.h>
#include <openssl/x509.h>
#endif

/* --- optional tracing (KITTYTK_DEBUG=1) ----------------------------- */

static int kt_debug(void) {
    static int v = -1;
    if (v < 0) {
        const char *e = getenv("KITTYTK_DEBUG");
        v = (e && (!strcmp(e, "1") || !strcmp(e, "true") ||
                   !strcmp(e, "yes") || !strcmp(e, "on"))) ? 1 : 0;
    }
    return v;
}
#define KTDBG(...)                                        \
    do {                                                  \
        if (kt_debug()) {                                 \
            fprintf(stderr, "kittytk/client: " __VA_ARGS__); \
            fprintf(stderr, "\n");                        \
        }                                                 \
    } while (0)

/* --- growable byte buffer ------------------------------------------- */

typedef struct { char *p; size_t len, cap; } kt_buf;
static void buf_put(kt_buf *b, char c) {
    if (b->len + 1 >= b->cap) {
        b->cap = b->cap ? b->cap * 2 : 64;
        b->p = realloc(b->p, b->cap);
    }
    b->p[b->len++] = c;
}
static void buf_puts(kt_buf *b, const char *s) { for (; *s; s++) buf_put(b, *s); }
static char *buf_dup(kt_buf *b) {
    char *s = malloc(b->len + 1);
    memcpy(s, b->p, b->len);
    s[b->len] = '\0';
    return s;
}

/* --- string quoting -------------------------------------------------- */

char *kt_quote(const char *s) {
    kt_buf b = {0};
    buf_put(&b, '"');
    for (; *s; s++) {
        unsigned char c = (unsigned char)*s;
        if (c == '"') { buf_put(&b, '\\'); buf_put(&b, '"'); }
        else if (c == '\\') { buf_put(&b, '\\'); buf_put(&b, '\\'); }
        else if (c == '\n') { buf_put(&b, '\\'); buf_put(&b, 'n'); }
        else if (c == '\t') { buf_put(&b, '\\'); buf_put(&b, 't'); }
        else if (c == '\r') { buf_put(&b, '\\'); buf_put(&b, 'r'); }
        else if (c == 0x1b) { buf_put(&b, '\\'); buf_put(&b, 'e'); }
        else if (c < 0x20 || c == 0x7f) {
            char tmp[5];
            snprintf(tmp, sizeof tmp, "\\x%02x", c);
            for (char *t = tmp; *t; t++) buf_put(&b, *t);
        } else buf_put(&b, (char)c);
    }
    buf_put(&b, '"');
    return buf_dup(&b);
}

/* --- config paths (mirror the Go & Python clients) ------------------- */
/* Only the TLS build (identity + known_hosts) needs these. */
#ifdef KT_TLS

/* config_dir: $XDG_CONFIG_HOME, else %APPDATA% on Windows, else
 * ~/.config; with "/kittytk" appended. malloc'd. */
static char *config_dir(void) {
    const char *base = getenv("XDG_CONFIG_HOME");
    char *home_cfg = NULL;
    if (!base || !*base) {
#ifdef _WIN32
        base = getenv("APPDATA");
#endif
    }
    if (!base || !*base) {
        const char *home = getenv("HOME");
#ifdef _WIN32
        if (!home || !*home) home = getenv("USERPROFILE");
#endif
        if (!home || !*home) home = ".";
        size_t n = strlen(home) + strlen("/.config") + 1;
        home_cfg = malloc(n);
        snprintf(home_cfg, n, "%s/.config", home);
        base = home_cfg;
    }
    size_t n = strlen(base) + strlen("/kittytk") + 1;
    char *out = malloc(n);
    snprintf(out, n, "%s/kittytk", base);
    free(home_cfg);
    return out;
}

static char *path_in_config(const char *env, const char *leaf) {
    const char *e = getenv(env);
    if (e && *e) return strdup(e);
    char *dir = config_dir();
    size_t n = strlen(dir) + 1 + strlen(leaf) + 1;
    char *out = malloc(n);
    snprintf(out, n, "%s/%s", dir, leaf);
    free(dir);
    return out;
}

/* make_parent_dirs: mkdir -p on the directory holding path. */
static void make_parent_dirs(const char *path) {
    char *tmp = strdup(path);
    for (char *p = tmp + 1; *p; p++) {
        if (*p == '/') {
            *p = '\0';
            kt_mkdir(tmp);
            *p = '/';
        }
    }
    free(tmp);
}

#endif /* KT_TLS (config paths) */

char *kt_default_endpoint(void) {
    const char *env = getenv("KITTYTK_DISPLAY");
    if (env && *env) return strdup(env);
#ifdef _WIN32
    /* AF_UNIX is unsupported under Wine and unreliable on older Windows, so
     * default to loopback TCP - matching the Go host's Windows default. */
    return strdup("tcp://127.0.0.1:9797");
#else
    /* Match the Go host's default: XDG_RUNTIME_DIR, else TMPDIR, else
     * /tmp (macOS TMPDIR is /var/folders/.../T, NOT /tmp). */
    const char *rt = getenv("XDG_RUNTIME_DIR");
    if (!rt || !*rt) rt = getenv("TMPDIR");
    if (!rt || !*rt) rt = "/tmp";
    size_t rtlen = strlen(rt);
    while (rtlen > 1 && rt[rtlen - 1] == '/') rtlen--;
    size_t n = rtlen + strlen("/kittytk/display-0.sock") + 1;
    char *out = malloc(n);
    snprintf(out, n, "%.*s/kittytk/display-0.sock", (int)rtlen, rt);
    return out;
#endif
}
char *kt_default_socket_path(void) { return kt_default_endpoint(); }

/* --- parsed statement ------------------------------------------------ */

typedef struct {
    char *name;
    kt_flag flag;    /* KT_FLAG_NONE when it has a value */
    int has_value;
    int kind;        /* 0=int 1=float 2=string 3=word */
    long long ival;
    double fval;
    char *sval;      /* string (unescaped) or word text; NUL-terminated */
    size_t slen;     /* byte length of sval, so interior NUL (\x00) survives */
} kt_arg;

typedef struct { char *verb; kt_arg *args; int n; } kt_stmt;

struct kt_event { const char *type; const kt_arg *fields; int n; };

static void stmt_free(kt_stmt *s) {
    if (!s) return;
    free(s->verb);
    for (int i = 0; i < s->n; i++) { free(s->args[i].name); free(s->args[i].sval); }
    free(s->args);
    free(s);
}

/* Flat parser: inbound statements (welcome/reply/error/event) are always
 * `verb arg...` with no nested blocks, so this covers the whole inbound
 * grammar. */
typedef struct { const char *s; size_t pos, len; } kt_p;

static int p_eof(kt_p *p) { return p->pos >= p->len; }
static char p_peek(kt_p *p) { return p_eof(p) ? '\0' : p->s[p->pos]; }
static void p_skip_inline(kt_p *p) {
    while (!p_eof(p)) {
        char c = p_peek(p);
        if (c == ' ' || c == '\t' || c == '\r' || c == '\n') p->pos++;
        else break;
    }
}
static int is_word_start(char c) { return c == '_' || isalpha((unsigned char)c); }
static int is_word_rune(char c) { return is_word_start(c) || c == '.' || isdigit((unsigned char)c); }

static char *p_word(kt_p *p) {
    kt_buf b = {0};
    while (!p_eof(p) && is_word_rune(p_peek(p))) buf_put(&b, p->s[p->pos++]);
    char *w = buf_dup(&b);
    free(b.p);
    return w;
}

static int hexv(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static char *p_string(kt_p *p, size_t *outlen) {  /* assumes current char is '"' */
    kt_buf b = {0};
    p->pos++;  /* opening quote */
    while (!p_eof(p)) {
        char c = p->s[p->pos++];
        if (c == '"') break;
        if (c == '\\' && !p_eof(p)) {
            char e = p->s[p->pos++];
            switch (e) {
            case '\\': buf_put(&b, '\\'); break;
            case '"': buf_put(&b, '"'); break;
            case 'n': buf_put(&b, '\n'); break;
            case 't': buf_put(&b, '\t'); break;
            case 'r': buf_put(&b, '\r'); break;
            case 'e': buf_put(&b, 0x1b); break;
            case 'x': {
                int hi = p_eof(p) ? -1 : hexv(p->s[p->pos++]);
                int lo = p_eof(p) ? -1 : hexv(p->s[p->pos++]);
                if (hi >= 0 && lo >= 0) buf_put(&b, (char)(hi << 4 | lo));
                break;
            }
            default: buf_put(&b, e); break;
            }
        } else buf_put(&b, c);
    }
    if (outlen) *outlen = b.len;
    char *s = buf_dup(&b);
    free(b.p);
    return s;
}

static void p_value(kt_p *p, kt_arg *a) {
    char c = p_peek(p);
    if (c == '"') {
        a->kind = 2; a->has_value = 1; a->sval = p_string(p, &a->slen);
    } else if (c == '-' || isdigit((unsigned char)c)) {
        kt_buf b = {0};
        int dot = 0;
        if (c == '-') buf_put(&b, p->s[p->pos++]);
        while (!p_eof(p)) {
            char d = p_peek(p);
            if (isdigit((unsigned char)d)) buf_put(&b, p->s[p->pos++]);
            else if (d == '.' && !dot) { dot = 1; buf_put(&b, p->s[p->pos++]); }
            else break;
        }
        char *num = buf_dup(&b); free(b.p);
        a->has_value = 1;
        if (dot) { a->kind = 1; a->fval = strtod(num, NULL); }
        else { a->kind = 0; a->ival = strtoll(num, NULL, 10); }
        free(num);
    } else {
        a->kind = 3; a->has_value = 1; a->sval = p_word(p); a->slen = strlen(a->sval);
    }
}

static kt_stmt *parse_statement(const char *text) {
    kt_p p = {text, 0, strlen(text)};
    p_skip_inline(&p);
    if (p_eof(&p) || !is_word_start(p_peek(&p))) return NULL;
    kt_stmt *st = calloc(1, sizeof *st);
    st->verb = p_word(&p);
    int cap = 0;
    for (;;) {
        p_skip_inline(&p);
        if (p_eof(&p) || p_peek(&p) == ';') break;
        char c = p_peek(&p);
        kt_arg a;
        memset(&a, 0, sizeof a);
        if (c == '!' || c == '?') {
            p.pos++;
            a.name = p_word(&p);
            a.flag = (c == '?') ? KT_FLAG_INDET : KT_FLAG_FALSE;
        } else if (is_word_start(c)) {
            a.name = p_word(&p);
            p_skip_inline(&p);
            if (!p_eof(&p) && p_peek(&p) == '=') { p.pos++; p_value(&p, &a); a.flag = KT_FLAG_NONE; }
            else a.flag = KT_FLAG_TRUE;
        } else if (c == '-' || isdigit((unsigned char)c)) {
            p_value(&p, &a);  /* bare number (target ref) */
        } else break;
        if (st->n + 1 > cap) { cap = cap ? cap * 2 : 8; st->args = realloc(st->args, cap * sizeof(kt_arg)); }
        st->args[st->n++] = a;
    }
    return st;
}

/* --- event field readers -------------------------------------------- */

static const kt_arg *ev_field(const kt_event *ev, const char *name) {
    for (int i = 0; i < ev->n; i++)
        if (ev->fields[i].name && strcmp(ev->fields[i].name, name) == 0) return &ev->fields[i];
    return NULL;
}
const char *kt_event_type(const kt_event *ev) { return ev->type; }
int kt_event_uint(const kt_event *ev, const char *name, uint64_t *out) {
    const kt_arg *a = ev_field(ev, name);
    if (!a || !a->has_value || a->kind != 0 || a->ival < 0) return 0;
    *out = (uint64_t)a->ival; return 1;
}
int kt_event_int(const kt_event *ev, const char *name, long long *out) {
    const kt_arg *a = ev_field(ev, name);
    if (!a || !a->has_value || a->kind != 0) return 0;
    *out = a->ival; return 1;
}
const char *kt_event_text(const kt_event *ev, const char *name) {
    const kt_arg *a = ev_field(ev, name);
    return (a && a->has_value && a->kind == 2) ? a->sval : NULL;
}
const char *kt_event_text_n(const kt_event *ev, const char *name, size_t *len) {
    const kt_arg *a = ev_field(ev, name);
    if (!a || !a->has_value || a->kind != 2) return NULL;
    if (len) *len = a->slen;
    return a->sval;
}
const char *kt_event_word(const kt_event *ev, const char *name) {
    const kt_arg *a = ev_field(ev, name);
    return (a && a->has_value && a->kind == 3) ? a->sval : NULL;
}
kt_flag kt_event_flag(const kt_event *ev, const char *name) {
    const kt_arg *a = ev_field(ev, name);
    if (!a || a->has_value) return KT_FLAG_NONE;
    return a->flag;
}
uint64_t kt_event_trinket(const kt_event *ev, int *ok) {
    uint64_t v;
    if (kt_event_uint(ev, "trinket", &v)) { if (ok) *ok = 1; return v; }
    if (kt_event_uint(ev, "window", &v)) { if (ok) *ok = 1; return v; }
    if (ok) *ok = 0;
    return 0;
}

/* --- connection ------------------------------------------------------ */

typedef struct { char *name; uint64_t id; } kt_pair;
struct kt_ui { kt_pair *pairs; int n; };

typedef struct evnode { char *text; struct evnode *next; } evnode;

typedef struct {
    uint64_t id;         /* 0 for command-only handlers */
    char *event_type;    /* NULL for command handlers */
    char *action;        /* non-NULL for command handlers */
    kt_event_cb cb;
    kt_command_cb ccb;
    void *ud;
} kt_handler;

struct kt_conn {
    kt_socket fd;
#ifdef KT_TLS
    SSL *ssl;
    SSL_CTX *ssl_ctx;
    kt_mutex ssl_mu; /* serializes SSL_read/SSL_write (one SSL, two threads) */
#endif
    unsigned char rbuf[4096];
    size_t rpos, rlen;
    int reof;

    uint64_t app_id; /* Application ObjectID from the handshake (0 until set) */

    kt_mutex write_mu;

    kt_mutex rmu; kt_cond rcv;
    int reply_ready, reply_err;
    kt_ui reply_ids;
    char reply_errmsg[256];
    /* describe (D24): flat vocabulary statements buffered (under rmu)
     * until the reply that terminates the batch. */
    char **desc; int desc_n;

    kt_mutex emu; kt_cond ecv;
    evnode *ehead, *etail;
    int estop;

    kt_mutex hmu;
    kt_handler *handlers; int nh, caph;
    kt_pair *subs; int nsubs, capsubs;
    char **subtypes;

    int closed;
    kt_thread rthread, ethread;
};

/* transport read/write: TLS when negotiated, else the raw socket.
 *
 * A single SSL object is shared by the reader thread (SSL_read) and any
 * do_exec caller (SSL_write); OpenSSL forbids concurrent use, so both go
 * through ssl_mu. The socket is non-blocking under TLS so a blocked read
 * never holds ssl_mu (which would deadlock the writer): on WANT_READ/
 * WANT_WRITE we drop the lock and poll. Plaintext read/write need no
 * lock - the kernel serializes concurrent recv/send on one fd. */

#ifdef KT_TLS
static void tls_wait(kt_socket fd, int for_write, int timeout_ms) {
#ifdef _WIN32
    WSAPOLLFD pfd;
    pfd.fd = fd;
    pfd.events = for_write ? POLLWRNORM : POLLRDNORM;
    pfd.revents = 0;
    WSAPoll(&pfd, 1, timeout_ms);
#else
    struct pollfd pfd;
    pfd.fd = fd;
    pfd.events = for_write ? POLLOUT : POLLIN;
    pfd.revents = 0;
    poll(&pfd, 1, timeout_ms);
#endif
}

static void set_nonblocking(kt_socket fd) {
#ifdef _WIN32
    u_long nb = 1;
    ioctlsocket(fd, FIONBIO, &nb);
#else
    int fl = fcntl(fd, F_GETFL, 0);
    if (fl >= 0) fcntl(fd, F_SETFL, fl | O_NONBLOCK);
#endif
}
#endif /* KT_TLS */

static long conn_read(kt_conn *c, void *buf, size_t n) {
#ifdef KT_TLS
    if (c->ssl) {
        for (;;) {
            kt_mutex_lock(&c->ssl_mu);
            int r = SSL_read(c->ssl, buf, (int)n);
            int err = r > 0 ? 0 : SSL_get_error(c->ssl, r);
            kt_mutex_unlock(&c->ssl_mu);
            if (r > 0) return r;
            if (err == SSL_ERROR_WANT_READ) { tls_wait(c->fd, 0, 200); continue; }
            if (err == SSL_ERROR_WANT_WRITE) { tls_wait(c->fd, 1, 200); continue; }
            KTDBG("conn_read: SSL_read err=%d (errno=%d)", err, errno);
            return -1; /* clean close or fatal error */
        }
    }
#endif
    return recv(c->fd, buf, (int)n, 0);
}

static int conn_write_all(kt_conn *c, const void *buf, size_t n) {
    const char *p = buf;
#ifdef KT_TLS
    if (c->ssl) {
        while (n > 0) {
            kt_mutex_lock(&c->ssl_mu);
            int w = SSL_write(c->ssl, p, (int)n);
            int err = w > 0 ? 0 : SSL_get_error(c->ssl, w);
            kt_mutex_unlock(&c->ssl_mu);
            if (w > 0) { p += w; n -= (size_t)w; continue; }
            if (err == SSL_ERROR_WANT_WRITE) { tls_wait(c->fd, 1, 200); continue; }
            if (err == SSL_ERROR_WANT_READ) { tls_wait(c->fd, 0, 200); continue; }
            KTDBG("conn_write: SSL_write err=%d (errno=%d)", err, errno);
            return -1;
        }
        return 0;
    }
#endif
    while (n > 0) {
        long w = send(c->fd, p, (int)n, 0);
        if (w <= 0) return -1;
        p += w; n -= (size_t)w;
    }
    return 0;
}

/* scanner: read one byte from the buffered transport, -1 at EOF/error. */
static int read_byte(kt_conn *c) {
    if (c->rpos >= c->rlen) {
        if (c->reof) return -1;
        long n = conn_read(c, c->rbuf, sizeof c->rbuf);
        if (n <= 0) { c->reof = 1; return -1; }
        c->rpos = 0; c->rlen = (size_t)n;
    }
    return c->rbuf[c->rpos++];
}

/* Frame the next statement (mirror of the Go Scanner). */
static char *scan_next(kt_conn *c) {
    kt_buf b = {0};
    int depth = 0, in_string = 0, escaped = 0, saw = 0;
    for (;;) {
        int ch = read_byte(c);
        if (ch < 0) {
            char *r = (saw && depth == 0 && !in_string) ? buf_dup(&b) : NULL;
            free(b.p);
            return r;
        }
        if (escaped) escaped = 0;
        else if (in_string) {
            if (ch == '\\') escaped = 1;
            else if (ch == '"') in_string = 0;
        } else if (ch == '"') { in_string = 1; saw = 1; }
        else if (ch == '{') { depth++; saw = 1; }
        else if (ch == '}') { depth--; saw = 1; }
        else if (ch == '#') {
            for (;;) { int x = read_byte(c); if (x < 0 || x == '\n') break; }
            if (saw && depth == 0) { buf_put(&b, '\n'); char *r = buf_dup(&b); free(b.p); return r; }
            continue;
        } else if (ch == '\n') {
            if (depth == 0) {
                if (saw) { buf_put(&b, '\n'); char *r = buf_dup(&b); free(b.p); return r; }
                continue;
            }
        } else if (ch != ' ' && ch != '\t' && ch != '\r' && ch != ';') saw = 1;
        buf_put(&b, (char)ch);
    }
}

static void enqueue_event(kt_conn *c, const char *text) {
    evnode *n = malloc(sizeof *n);
    n->text = strdup(text);
    n->next = NULL;
    kt_mutex_lock(&c->emu);
    if (c->etail) c->etail->next = n; else c->ehead = n;
    c->etail = n;
    kt_cond_signal(&c->ecv);
    kt_mutex_unlock(&c->emu);
}

static void dispatch_event(kt_conn *c, kt_stmt *st) {
    if (st->n < 1) return;
    kt_event ev = { st->args[0].name, st->n > 1 ? &st->args[1] : NULL, st->n - 1 };
    int ok = 0;
    uint64_t tid = kt_event_trinket(&ev, &ok);
    const char *action = (strcmp(ev.type, "command") == 0) ? kt_event_word(&ev, "action") : NULL;

    kt_mutex_lock(&c->hmu);
    kt_handler *snap = malloc(sizeof(kt_handler) * (c->nh ? c->nh : 1));
    int m = 0;
    for (int i = 0; i < c->nh; i++) {
        kt_handler *h = &c->handlers[i];
        if (h->action) {
            if (action && strcmp(h->action, action) == 0) snap[m++] = *h;
        } else if (ok && h->id == tid && h->event_type && strcmp(h->event_type, ev.type) == 0) {
            snap[m++] = *h;
        }
    }
    kt_mutex_unlock(&c->hmu);

    for (int i = 0; i < m; i++) {
        if (snap[i].action) snap[i].ccb(snap[i].ud);
        else snap[i].cb(&ev, snap[i].ud);
    }
    free(snap);
}

static void *event_loop(void *arg) {
    kt_conn *c = arg;
    for (;;) {
        kt_mutex_lock(&c->emu);
        while (!c->ehead && !c->estop) kt_cond_wait(&c->ecv, &c->emu);
        if (!c->ehead && c->estop) { kt_mutex_unlock(&c->emu); return NULL; }
        evnode *n = c->ehead;
        c->ehead = n->next;
        if (!c->ehead) c->etail = NULL;
        kt_mutex_unlock(&c->emu);

        kt_stmt *st = parse_statement(n->text);
        if (st) { dispatch_event(c, st); stmt_free(st); }
        free(n->text);
        free(n);
    }
}

static void mark_closed(kt_conn *c) {
    kt_mutex_lock(&c->rmu);
    c->closed = 1;
    c->reply_ready = 1;
    c->reply_err = 1;
    snprintf(c->reply_errmsg, sizeof c->reply_errmsg, "connection closed");
    kt_cond_broadcast(&c->rcv);
    kt_mutex_unlock(&c->rmu);

    kt_mutex_lock(&c->emu);
    c->estop = 1;
    kt_cond_signal(&c->ecv);
    kt_mutex_unlock(&c->emu);
}

static void *read_loop(void *arg) {
    kt_conn *c = arg;
    for (;;) {
        char *text = scan_next(c);
        if (!text) break;
        kt_stmt *st = parse_statement(text);
        if (!st) { free(text); continue; }
        if (strcmp(st->verb, "reply") == 0) {
            kt_mutex_lock(&c->rmu);
            free(c->reply_ids.pairs); c->reply_ids.pairs = NULL; c->reply_ids.n = 0;
            for (int i = 0; i < st->n; i++) {
                if (st->args[i].has_value && st->args[i].kind == 0) {
                    c->reply_ids.pairs = realloc(c->reply_ids.pairs, (c->reply_ids.n + 1) * sizeof(kt_pair));
                    c->reply_ids.pairs[c->reply_ids.n].name = strdup(st->args[i].name);
                    c->reply_ids.pairs[c->reply_ids.n].id = (uint64_t)st->args[i].ival;
                    c->reply_ids.n++;
                }
            }
            c->reply_err = 0;
            c->reply_ready = 1;
            kt_cond_signal(&c->rcv);
            kt_mutex_unlock(&c->rmu);
        } else if (strcmp(st->verb, "error") == 0) {
            const char *msg = "display error";
            for (int i = 0; i < st->n; i++)
                if (strcmp(st->args[i].name ? st->args[i].name : "", "text") == 0
                    && st->args[i].has_value && st->args[i].kind == 2)
                    msg = st->args[i].sval;
            kt_mutex_lock(&c->rmu);
            c->reply_err = 1;
            snprintf(c->reply_errmsg, sizeof c->reply_errmsg, "%s", msg);
            c->reply_ready = 1;
            kt_cond_signal(&c->rcv);
            kt_mutex_unlock(&c->rmu);
        } else if (strcmp(st->verb, "event") == 0) {
            enqueue_event(c, text);
        } else if (strcmp(st->verb, "proptype") == 0 ||
                   strcmp(st->verb, "prop") == 0 ||
                   strcmp(st->verb, "propcommon") == 0) {
            /* describe verb output: buffer until the reply. */
            kt_mutex_lock(&c->rmu);
            c->desc = realloc(c->desc, (c->desc_n + 1) * sizeof(char *));
            c->desc[c->desc_n++] = strdup(text);
            kt_mutex_unlock(&c->rmu);
        }
        stmt_free(st);
        free(text);
    }
    mark_closed(c);
    return NULL;
}

/* Write src + "\nend\n"; wait for the reply. out_ids may be NULL. When
 * out_desc is non-NULL it receives ownership of the batch's buffered
 * describe statements (out_ndesc their count). */
static int do_exec(kt_conn *c, const char *src, kt_ui *out_ids,
                   char ***out_desc, int *out_ndesc) {
    kt_mutex_lock(&c->write_mu);
    kt_mutex_lock(&c->rmu);
    if (c->closed) { kt_mutex_unlock(&c->rmu); kt_mutex_unlock(&c->write_mu); return -1; }
    c->reply_ready = 0;
    for (int i = 0; i < c->desc_n; i++) free(c->desc[i]);
    free(c->desc); c->desc = NULL; c->desc_n = 0;
    kt_mutex_unlock(&c->rmu);

    /* One write: src + the D22 terminator as a single buffer, so the
     * batch and its `end` can never be split across records (a lost or
     * reordered terminator would leave the host's readBatch waiting
     * forever). */
    size_t srclen = strlen(src);
    kt_buf wb = {0};
    for (size_t i = 0; i < srclen; i++) buf_put(&wb, src[i]);
    buf_puts(&wb, "\nend\n");
    int werr = conn_write_all(c, wb.p, wb.len) < 0;
    free(wb.p);
    if (werr) {
        KTDBG("exec: write failed");
        kt_mutex_unlock(&c->write_mu);
        return -1;
    }
    KTDBG("exec: batch sent (%zu bytes), awaiting reply", srclen + 5);

    kt_mutex_lock(&c->rmu);
    while (!c->reply_ready) {
        /* Bounded wait so a lost reply can't wedge the caller (and, in
         * demoapp, the event thread) forever. */
        if (kt_cond_timedwait(&c->rcv, &c->rmu, 30000) != 0 && !c->reply_ready) {
            kt_mutex_unlock(&c->rmu);
            kt_mutex_unlock(&c->write_mu);
            KTDBG("exec: TIMED OUT after 30s waiting for reply");
            return -1;
        }
    }
    int err = c->reply_err;
    KTDBG("exec: reply received (err=%d)", err);
    if (!err && out_ids) {
        out_ids->n = c->reply_ids.n;
        out_ids->pairs = malloc(sizeof(kt_pair) * (c->reply_ids.n ? c->reply_ids.n : 1));
        for (int i = 0; i < c->reply_ids.n; i++) {
            out_ids->pairs[i].name = strdup(c->reply_ids.pairs[i].name);
            out_ids->pairs[i].id = c->reply_ids.pairs[i].id;
        }
    }
    if (out_desc) {
        /* Transfer ownership of the buffered describe lines to the caller. */
        *out_desc = err ? NULL : c->desc;
        *out_ndesc = err ? 0 : c->desc_n;
        if (!err) { c->desc = NULL; c->desc_n = 0; }
    }
    kt_mutex_unlock(&c->rmu);
    kt_mutex_unlock(&c->write_mu);
    return err ? -1 : 0;
}

int kt_exec(kt_conn *c, const char *src) { return do_exec(c, src, NULL, NULL, NULL); }

kt_ui *kt_build(kt_conn *c, const char *src) {
    kt_ui *ui = calloc(1, sizeof *ui);
    if (do_exec(c, src, ui, NULL, NULL) != 0) { free(ui->pairs); free(ui); return NULL; }
    return ui;
}
uint64_t kt_ui_id(const kt_ui *ui, const char *name) {
    if (!ui) return 0;
    for (int i = 0; i < ui->n; i++)
        if (strcmp(ui->pairs[i].name, name) == 0) return ui->pairs[i].id;
    return 0;
}

uint64_t kt_app_id(kt_conn *c) { return c ? c->app_id : 0; }

int kt_set_app(kt_conn *c, const char *props) {
    if (!c || !c->app_id) return -1;
    char buf[512];
    snprintf(buf, sizeof(buf), "set %llu %s",
             (unsigned long long)c->app_id, props ? props : "");
    return kt_exec(c, buf);
}
void kt_ui_free(kt_ui *ui) {
    if (!ui) return;
    for (int i = 0; i < ui->n; i++) free(ui->pairs[i].name);
    free(ui->pairs);
    free(ui);
}

/* --- introspection (describe, D24) ----------------------------------- */

static const char *stmt_str(const kt_stmt *st, const char *name) {
    for (int i = 0; i < st->n; i++)
        if (st->args[i].name && strcmp(st->args[i].name, name) == 0
            && st->args[i].has_value && st->args[i].kind == 2)
            return st->args[i].sval;
    return "";
}
static int stmt_flag_true(const kt_stmt *st, const char *name) {
    for (int i = 0; i < st->n; i++)
        if (st->args[i].name && strcmp(st->args[i].name, name) == 0 && !st->args[i].has_value)
            return st->args[i].flag == KT_FLAG_TRUE;
    return 0;
}
static void fill_prop(kt_prop *p, const kt_stmt *st) {
    p->name  = strdup(stmt_str(st, "name"));
    p->kind  = strdup(stmt_str(st, "kind"));
    p->deflt = strdup(stmt_str(st, "default"));
    p->doc   = strdup(stmt_str(st, "doc"));
    p->enums = strdup(stmt_str(st, "enum"));
}

kt_vocab *kt_describe(kt_conn *c) {
    char **lines = NULL; int nlines = 0;
    if (do_exec(c, "describe", NULL, &lines, &nlines) != 0) return NULL;
    kt_vocab *v = calloc(1, sizeof *v);
    for (int i = 0; i < nlines; i++) {
        kt_stmt *st = parse_statement(lines[i]);
        if (!st) continue;
        if (strcmp(st->verb, "propcommon") == 0) {
            v->common = realloc(v->common, (v->ncommon + 1) * sizeof(kt_prop));
            fill_prop(&v->common[v->ncommon++], st);
        } else if (strcmp(st->verb, "proptype") == 0) {
            v->types = realloc(v->types, (v->ntypes + 1) * sizeof(kt_type));
            kt_type *t = &v->types[v->ntypes++];
            t->name = strdup(stmt_str(st, "name"));
            t->is_virtual = stmt_flag_true(st, "virtual");
            t->props = NULL; t->nprops = 0;
        } else if (strcmp(st->verb, "prop") == 0) {
            const char *of = stmt_str(st, "of");
            for (int k = 0; k < v->ntypes; k++)
                if (strcmp(v->types[k].name, of) == 0) {
                    kt_type *t = &v->types[k];
                    t->props = realloc(t->props, (t->nprops + 1) * sizeof(kt_prop));
                    fill_prop(&t->props[t->nprops++], st);
                    break;
                }
        }
        stmt_free(st);
    }
    for (int i = 0; i < nlines; i++) free(lines[i]);
    free(lines);
    return v;
}

static void prop_free(kt_prop *p) {
    free(p->name); free(p->kind); free(p->deflt); free(p->doc); free(p->enums);
}
void kt_vocab_free(kt_vocab *v) {
    if (!v) return;
    for (int i = 0; i < v->ncommon; i++) prop_free(&v->common[i]);
    free(v->common);
    for (int i = 0; i < v->ntypes; i++) {
        for (int j = 0; j < v->types[i].nprops; j++) prop_free(&v->types[i].props[j]);
        free(v->types[i].props);
        free(v->types[i].name);
    }
    free(v->types);
    free(v);
}

int kt_set(kt_conn *c, uint64_t id, const char *args) {
    char *src = malloc(strlen(args) + 32);
    sprintf(src, "set %llu %s", (unsigned long long)id, args);
    int r = kt_exec(c, src);
    free(src);
    return r;
}
int kt_destroy(kt_conn *c, uint64_t id) {
    char src[32];
    snprintf(src, sizeof src, "destroy %llu", (unsigned long long)id);
    return kt_exec(c, src);
}

/* --- subscriptions & handlers --------------------------------------- */

static void ensure_sub(kt_conn *c, uint64_t id, const char *event) {
    kt_mutex_lock(&c->hmu);
    for (int i = 0; i < c->nsubs; i++)
        if (c->subs[i].id == id && strcmp(c->subtypes[i], event) == 0) {
            kt_mutex_unlock(&c->hmu);
            return;
        }
    if (c->nsubs + 1 > c->capsubs) {
        c->capsubs = c->capsubs ? c->capsubs * 2 : 8;
        c->subs = realloc(c->subs, c->capsubs * sizeof(kt_pair));
        c->subtypes = realloc(c->subtypes, c->capsubs * sizeof(char *));
    }
    c->subs[c->nsubs].id = id;
    c->subtypes[c->nsubs] = strdup(event);
    c->nsubs++;
    kt_mutex_unlock(&c->hmu);

    char src[64];
    snprintf(src, sizeof src, "sub %llu %s", (unsigned long long)id, event);
    kt_exec(c, src);
}

static void add_handler(kt_conn *c, kt_handler h) {
    kt_mutex_lock(&c->hmu);
    if (c->nh + 1 > c->caph) {
        c->caph = c->caph ? c->caph * 2 : 8;
        c->handlers = realloc(c->handlers, c->caph * sizeof(kt_handler));
    }
    c->handlers[c->nh++] = h;
    kt_mutex_unlock(&c->hmu);
}

void kt_on(kt_conn *c, uint64_t id, const char *event_type, kt_event_cb cb, void *ud) {
    ensure_sub(c, id, event_type);
    kt_handler h = {0};
    h.id = id; h.event_type = strdup(event_type); h.cb = cb; h.ud = ud;
    add_handler(c, h);
}
void kt_on_command(kt_conn *c, const char *action, kt_command_cb cb, void *ud) {
    kt_handler h = {0};
    h.action = strdup(action); h.ccb = cb; h.ud = ud;
    add_handler(c, h);
}

/* --- endpoint parsing ------------------------------------------------ */

typedef struct { int is_unix; int use_tls; char *address; char *host; } kt_endpoint;

/* address gets a default :9797 when a tcp/tls endpoint omits the port. */
static char *with_port(const char *addr) {
    const char *colon = strrchr(addr, ':');
    int has_port = 0;
    if (colon && colon[1]) {
        has_port = 1;
        for (const char *d = colon + 1; *d; d++) if (!isdigit((unsigned char)*d)) { has_port = 0; break; }
    }
    if (has_port) return strdup(addr);
    size_t n = strlen(addr) + strlen(":9797") + 1;
    char *out = malloc(n);
    snprintf(out, n, "%s:9797", addr);
    return out;
}

static kt_endpoint parse_endpoint(const char *s) {
    kt_endpoint e = {1, 0, NULL, NULL};
    if (strncmp(s, "unix:", 5) == 0) {
        e.is_unix = 1; e.address = strdup(s + 5);
    } else if (strncmp(s, "tcp://", 6) == 0) {
        e.is_unix = 0; e.address = with_port(s + 6);
    } else if (strncmp(s, "tls://", 6) == 0) {
        e.is_unix = 0; e.use_tls = 1; e.address = with_port(s + 6);
        const char *colon = strrchr(e.address, ':');
        e.host = colon ? strndup(e.address, (size_t)(colon - e.address)) : strdup(e.address);
    } else {
        e.is_unix = 1; e.address = strdup(s);
    }
    return e;
}
static void endpoint_free(kt_endpoint *e) { free(e->address); free(e->host); }

/* --- TCP / unix connect ---------------------------------------------- */

static kt_socket connect_unix(const char *path) {
    kt_socket fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd == KT_BAD_SOCKET) return KT_BAD_SOCKET;
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof addr);
    addr.sun_family = AF_UNIX;
    strncpy(addr.sun_path, path, sizeof addr.sun_path - 1);
    if (connect(fd, (struct sockaddr *)&addr, sizeof addr) != 0) { kt_closesocket(fd); return KT_BAD_SOCKET; }
    return fd;
}

static kt_socket connect_tcp(const char *hostport) {
    const char *colon = strrchr(hostport, ':');
    if (!colon) return KT_BAD_SOCKET;
    char *host = strndup(hostport, (size_t)(colon - hostport));
    const char *port = colon + 1;

    struct addrinfo hints, *res = NULL, *ai;
    memset(&hints, 0, sizeof hints);
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    kt_socket fd = KT_BAD_SOCKET;
    if (getaddrinfo(host, port, &hints, &res) == 0) {
        for (ai = res; ai; ai = ai->ai_next) {
            fd = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
            if (fd == KT_BAD_SOCKET) continue;
            if (connect(fd, ai->ai_addr, (int)ai->ai_addrlen) == 0) break;
            kt_closesocket(fd);
            fd = KT_BAD_SOCKET;
        }
        freeaddrinfo(res);
    }
    free(host);
    return fd;
}

/* --- TLS (optional) -------------------------------------------------- */

#ifdef KT_TLS

/* fingerprint_hex: "sha256:" + lowercase hex of the DER SHA-256. */
static char *fingerprint_of(X509 *cert) {
    unsigned char *der = NULL;
    int len = i2d_X509(cert, &der);
    if (len <= 0) return NULL;
    unsigned char md[SHA256_DIGEST_LENGTH];
    SHA256(der, (size_t)len, md);
    OPENSSL_free(der);
    char *out = malloc(7 + 2 * SHA256_DIGEST_LENGTH + 1);
    strcpy(out, "sha256:");
    for (int i = 0; i < SHA256_DIGEST_LENGTH; i++)
        sprintf(out + 7 + 2 * i, "%02x", md[i]);
    return out;
}

/* ensure_identity: load or create the persistent client identity PEM. */
static char *identity_path(void) { return path_in_config("KITTYTK_IDENTITY", "identity.pem"); }

static int create_identity(const char *path) {
    EVP_PKEY *pkey = EVP_EC_gen("P-256");
    if (!pkey) return -1;
    X509 *x = X509_new();
    ASN1_INTEGER_set(X509_get_serialNumber(x), 1);
    X509_gmtime_adj(X509_getm_notBefore(x), -3600);
    X509_gmtime_adj(X509_getm_notAfter(x), (long)60 * 60 * 24 * 7300);
    X509_set_pubkey(x, pkey);
    X509_NAME *nm = X509_get_subject_name(x);
    X509_NAME_add_entry_by_txt(nm, "CN", MBSTRING_ASC, (const unsigned char *)"kittytk-client", -1, -1, 0);
    X509_set_issuer_name(x, nm);
    X509_sign(x, pkey, EVP_sha256());

    make_parent_dirs(path);
    FILE *f = fopen(path, "wb");
    int ok = 0;
    if (f) {
        ok = PEM_write_PrivateKey(f, pkey, NULL, NULL, 0, NULL, NULL) &&
             PEM_write_X509(f, x);
        fclose(f);
    }
    X509_free(x);
    EVP_PKEY_free(pkey);
    return ok ? 0 : -1;
}

static char *ensure_identity(void) {
    char *path = identity_path();
    FILE *f = fopen(path, "rb");
    if (f) { fclose(f); return path; }
    if (create_identity(path) != 0) { free(path); return NULL; }
    return path;
}

/* known_hosts pinning (mirror of the Go/Python clients). */
static char *known_hosts_path(const char *override) {
    if (override && *override) return strdup(override);
    return path_in_config("KITTYTK_KNOWN_HOSTS", "known_hosts");
}

static char *lookup_pin(const char *path, const char *hostport) {
    FILE *f = fopen(path, "r");
    if (!f) return NULL;
    char line[512];
    char *found = NULL;
    while (fgets(line, sizeof line, f)) {
        char host[256], fp[160];
        if (line[0] == '#') continue;
        if (sscanf(line, "%255s %159s", host, fp) == 2 && strcmp(host, hostport) == 0) {
            found = strdup(fp);
            break;
        }
    }
    fclose(f);
    return found;
}

static void add_pin(const char *path, const char *hostport, const char *fp) {
    make_parent_dirs(path);
    FILE *f = fopen(path, "a");
    if (!f) return;
    fprintf(f, "%s %s\n", hostport, fp);
    fclose(f);
}

/* verify_pin: 0 ok (pinned or newly recorded), -1 mismatch. */
static int verify_pin(const char *store, const char *hostport, const char *fp) {
    char *path = known_hosts_path(store);
    char *pinned = lookup_pin(path, hostport);
    int rc = 0;
    if (!pinned) {
        add_pin(path, hostport, fp);
        fprintf(stderr, "kittytk: pinned new host %s %s\n", hostport, fp);
    } else if (strcmp(pinned, fp) != 0) {
        fprintf(stderr,
                "kittytk: host identity for %s changed!\n  pinned %s\n  got    %s\n"
                "if this is expected, remove that line from %s\n",
                hostport, pinned, fp, path);
        rc = -1;
    }
    free(pinned);
    free(path);
    return rc;
}

/* tls_handshake: wrap fd in a mutual-TLS session, pin the host. Returns 0
 * and fills c->ssl/ssl_ctx on success. */
static int tls_handshake(kt_conn *c, const kt_endpoint *e, int insecure, const char *store) {
    SSL_CTX *ctx = SSL_CTX_new(TLS_client_method());
    if (!ctx) return -1;
    SSL_CTX_set_verify(ctx, SSL_VERIFY_NONE, NULL); /* pinning, not CA */
    char *ident = ensure_identity();
    if (ident) {
        SSL_CTX_use_certificate_file(ctx, ident, SSL_FILETYPE_PEM);
        SSL_CTX_use_PrivateKey_file(ctx, ident, SSL_FILETYPE_PEM);
        free(ident);
    }
    SSL *ssl = SSL_new(ctx);
    SSL_set_fd(ssl, (int)c->fd);
    if (e->host) SSL_set_tlsext_host_name(ssl, e->host);
    if (SSL_connect(ssl) != 1) { SSL_free(ssl); SSL_CTX_free(ctx); return -1; }

    if (!insecure) {
        X509 *cert = SSL_get_peer_certificate(ssl);
        if (!cert) { SSL_free(ssl); SSL_CTX_free(ctx); return -1; }
        char *fp = fingerprint_of(cert);
        X509_free(cert);
        int bad = !fp || verify_pin(store, e->address, fp) != 0;
        free(fp);
        if (bad) { SSL_shutdown(ssl); SSL_free(ssl); SSL_CTX_free(ctx); return -1; }
    }
    c->ssl = ssl;
    c->ssl_ctx = ctx;
    /* Non-blocking from here on so SSL_read never blocks while holding
     * ssl_mu; conn_read/conn_write poll on WANT_READ/WANT_WRITE. */
    set_nonblocking(c->fd);
    return 0;
}
#endif /* KT_TLS */

/* --- dial / close ---------------------------------------------------- */

static kt_conn *dial(const char *endpoint, const char *app_name, const kt_dial_opts *opts) {
    kt_dial_opts z = {0};
    if (!opts) opts = &z;
    kt_platform_init();

    kt_endpoint e = parse_endpoint(endpoint);
    KTDBG("dial app=%s unix=%d tls=%d addr=%s: connecting",
          app_name, e.is_unix, e.use_tls, e.address ? e.address : "");
    kt_socket fd;
    if (e.is_unix) {
        fd = connect_unix(e.address);
    } else {
        fd = connect_tcp(e.address);
    }
    if (fd == KT_BAD_SOCKET) { KTDBG("dial app=%s: connect failed", app_name); endpoint_free(&e); return NULL; }

    kt_conn *c = calloc(1, sizeof *c);
    c->fd = fd;

    if (e.use_tls) {
#ifdef KT_TLS
        if (tls_handshake(c, &e, opts->insecure, opts->known_hosts) != 0) {
            kt_closesocket(fd); free(c); endpoint_free(&e); return NULL;
        }
#else
        fprintf(stderr, "kittytk: tls:// endpoints need a build with -DKT_TLS (OpenSSL)\n");
        kt_closesocket(fd); free(c); endpoint_free(&e); return NULL;
#endif
    }
    endpoint_free(&e);

    kt_mutex_init(&c->write_mu);
    kt_mutex_init(&c->rmu); kt_cond_init(&c->rcv);
    kt_mutex_init(&c->emu); kt_cond_init(&c->ecv);
    kt_mutex_init(&c->hmu);
#ifdef KT_TLS
    kt_mutex_init(&c->ssl_mu);
#endif

    /* handshake: hello [solo] [token], then wait for welcome */
    const char *token = opts->token;
    if (!token) token = getenv("KITTYTK_TOKEN");

    kt_buf hb = {0};
    buf_puts(&hb, "hello version=1 app=");
    char *q = kt_quote(app_name);
    buf_puts(&hb, q); free(q);
    if (opts->solo) buf_puts(&hb, " solo");
    if (token && *token) {
        buf_puts(&hb, " token=");
        char *tq = kt_quote(token);
        buf_puts(&hb, tq); free(tq);
    }
    buf_puts(&hb, "\nend\n");
    KTDBG("dial app=%s: transport up, sending hello", app_name);
    int wok = conn_write_all(c, hb.p, hb.len) == 0;
    free(hb.p);
    if (!wok) { KTDBG("dial app=%s: hello write failed", app_name); goto fail; }

    KTDBG("dial app=%s: hello sent, awaiting welcome", app_name);
    char *welcome = scan_next(c);
    if (!welcome) { KTDBG("dial app=%s: reading welcome failed (EOF)", app_name); goto fail; }
    kt_stmt *st = parse_statement(welcome);
    int ok = st && strcmp(st->verb, "welcome") == 0;
    if (ok) {
        /* The handshake carries this connection's Application ObjectID, so the
         * app can address application-wide properties (kt_app_id/kt_set_app). */
        for (int i = 0; i < st->n; i++) {
            if (st->args[i].name && strcmp(st->args[i].name, "app") == 0
                && st->args[i].kind == 0) {
                c->app_id = (uint64_t)st->args[i].ival;
            }
        }
    }
    stmt_free(st);
    free(welcome);
    if (!ok) { KTDBG("dial app=%s: bad welcome", app_name); goto fail; }
    KTDBG("dial app=%s: welcome received (app id=%llu), connection ready",
          app_name, (unsigned long long)c->app_id);

    kt_thread_create(&c->rthread, read_loop, c);
    kt_thread_create(&c->ethread, event_loop, c);
    return c;

fail:
#ifdef KT_TLS
    if (c->ssl) { SSL_free(c->ssl); SSL_CTX_free(c->ssl_ctx); }
#endif
    kt_closesocket(fd);
    free(c);
    return NULL;
}

kt_conn *kt_dial(const char *endpoint, const char *app_name) { return dial(endpoint, app_name, NULL); }
kt_conn *kt_dial_solo(const char *endpoint, const char *app_name) {
    kt_dial_opts o = {0};
    o.solo = 1;
    return dial(endpoint, app_name, &o);
}
kt_conn *kt_dial_ex(const char *endpoint, const char *app_name, const kt_dial_opts *opts) {
    return dial(endpoint, app_name, opts);
}

int kt_is_closed(kt_conn *c) {
    kt_mutex_lock(&c->rmu);
    int r = c->closed;
    kt_mutex_unlock(&c->rmu);
    return r;
}
void kt_wait_closed(kt_conn *c) {
    kt_thread_join(c->rthread);
}
void kt_close(kt_conn *c) {
    if (!c) return;
#ifdef KT_TLS
    /* close-notify under ssl_mu so it can't race the reader's SSL_read */
    if (c->ssl) { kt_mutex_lock(&c->ssl_mu); SSL_shutdown(c->ssl); kt_mutex_unlock(&c->ssl_mu); }
#endif
#ifdef _WIN32
    shutdown(c->fd, SD_BOTH);
#else
    shutdown(c->fd, SHUT_RDWR);
#endif
    kt_closesocket(c->fd);
    kt_thread_join(c->rthread);
    kt_thread_join(c->ethread);
#ifdef KT_TLS
    if (c->ssl) { SSL_free(c->ssl); SSL_CTX_free(c->ssl_ctx); }
#endif
    /* (handler/sub tables reclaimed at process exit in demo/smoke usage.) */
    for (int i = 0; i < c->desc_n; i++) free(c->desc[i]);
    free(c->desc);
    free(c);
}
