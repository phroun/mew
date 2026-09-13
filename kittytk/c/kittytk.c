/* kittytk.c - implementation of the KittyTK display-protocol client in C.
 * A faithful port of the wire format and the client's read/event loops,
 * with a thin platform shim (POSIX sockets+pthreads / Windows Winsock+
 * Win32 threads) and optional TLS (compile with -DKT_TLS, needs OpenSSL). */
#define _GNU_SOURCE
#include "kittytk.h"

#include <ctype.h>
#include <errno.h>
#include <stdarg.h>
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

/* kt_quote_blob renders arbitrary bytes as a protocol string, escaping every
 * one of them that is not printable ASCII. kt_quote takes a C string and lets
 * high bytes through, which is right for text and wrong for a blob: a blob may
 * hold NUL, and bytes that are not valid UTF-8 do not survive being read back
 * as text. Takes a length for the same reason. */
char *kt_quote_blob(const void *data, size_t n) {
    const unsigned char *p = (const unsigned char *)data;
    kt_buf b = {0};
    buf_put(&b, '"');
    for (size_t i = 0; i < n; i++) {
        unsigned char c = p[i];
        if (c == '"') { buf_put(&b, '\\'); buf_put(&b, '"'); }
        else if (c == '\\') { buf_put(&b, '\\'); buf_put(&b, '\\'); }
        else if (c >= 0x20 && c < 0x7f) buf_put(&b, (char)c);
        else {
            char tmp[5];
            snprintf(tmp, sizeof tmp, "\\x%02x", c);
            for (char *t = tmp; *t; t++) buf_put(&b, *t);
        }
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

typedef struct kt_script kt_script;

typedef struct {
    char *name;
    kt_flag flag;    /* KT_FLAG_NONE when it has a value */
    int has_value;
    int kind;        /* 0=int 1=float 2=string 3=word 4=block */
    long long ival;
    double fval;
    char *sval;      /* string (unescaped) or word text; NUL-terminated */
    size_t slen;     /* byte length of sval, so interior NUL (\x00) survives */
    kt_script *block;/* kind 4: the statements between the braces */
} kt_arg;

/* key is the correlation name a statement was written under: `q=new query
 * ...` surfaces the id the receiver mints for it, in the reply. */
typedef struct { char *key; char *verb; kt_arg *args; int n; } kt_stmt;

/* A run of statements: a request body, or what a {} holds. Blocks are how a
 * filter, a sort and a boundary travel, so the parser here is no longer flat. */
struct kt_script { kt_stmt *stmts; int n; };

/* --- ordering two values the same way at both ends -------------------
 *
 * A view's records come from more than one place at once, and each end orders
 * what it holds before the results are folded together -- so both ends must
 * compute the SAME order from the same spec without conferring.
 *
 * docs/sort-and-filter.md is the spec, ../testdata/compare.wire the corpus
 * every implementation of it answers. wire/compare.go is the Go side and
 * python/kittytk/protocol.py the Python one; all three answer case for case.
 */

/* A value's rank, which its type decides before anything in it is read. */
enum {
    KT_RANK_UNDEFINED = 0,
    KT_RANK_NIL,
    KT_RANK_FALSE,
    KT_RANK_TRUE,
    KT_RANK_NUMBER,   /* int and float share one rank and interleave by value */
    KT_RANK_SYMBOL,
    KT_RANK_STRING,
    KT_RANK_BYTES,
    KT_RANK_UNORDERED /* no order of its own; the sort's last level settles it */
};

/* The collations a string may be compared under. A symbol and a blob take
 * none: a symbol is an identifier, and a blob is not text. */
#define KT_COLLATE_EXACT   "exact"
#define KT_COLLATE_FOLD    "fold"
#define KT_COLLATE_NATURAL "natural"

static int kt_sign(long long n) { return n < 0 ? -1 : (n > 0 ? 1 : 0); }

/* rank_of: where a value sits before its contents matter. A value that is not
 * there at all is undefined, which is the bottom of the order. */
static int rank_of(const kt_arg *v) {
    if (!v || !v->has_value) return KT_RANK_UNDEFINED;
    switch (v->kind) {
    case 0: case 1: return KT_RANK_NUMBER;
    case 2: return KT_RANK_STRING;   /* the wire cannot tell a blob from text */
    case 3:
        if (!strcmp(v->sval, "undefined")) return KT_RANK_UNDEFINED;
        if (!strcmp(v->sval, "nil")) return KT_RANK_NIL;
        if (!strcmp(v->sval, "false")) return KT_RANK_FALSE;
        if (!strcmp(v->sval, "true")) return KT_RANK_TRUE;
        return KT_RANK_SYMBOL;
    }
    return KT_RANK_UNORDERED;
}

static int fold_char(int c) { return (c >= 'A' && c <= 'Z') ? c + ('a' - 'A') : c; }
static int is_ascii_digit(int c) { return c >= '0' && c <= '9'; }

/* utf8_next reads one codepoint, so strings compare rune by rune rather than
 * byte by byte. A byte that is not a well-formed start is its own value. */
static unsigned utf8_next(const char *s, size_t len, size_t *i) {
    unsigned char c = (unsigned char)s[*i];
    unsigned cp = c;
    int extra = 0;
    if (c >= 0xF0) { cp = c & 0x07u; extra = 3; }
    else if (c >= 0xE0) { cp = c & 0x0Fu; extra = 2; }
    else if (c >= 0xC0) { cp = c & 0x1Fu; extra = 1; }
    if (*i + (size_t)extra >= len) extra = 0;
    for (int k = 0; k < extra; k++) {
        unsigned char n = (unsigned char)s[*i + 1 + (size_t)k];
        if ((n & 0xC0) != 0x80) { extra = 0; cp = c; break; }
        cp = (cp << 6) | (n & 0x3Fu);
    }
    *i += 1 + (size_t)extra;
    return cp;
}

/* compare_runes: codepoint order, one rune against one. */
static int compare_runes(const char *a, size_t alen, const char *b, size_t blen, int fold) {
    size_t i = 0, j = 0;
    while (i < alen && j < blen) {
        unsigned x = utf8_next(a, alen, &i), y = utf8_next(b, blen, &j);
        if (fold) { x = (unsigned)fold_char((int)x); y = (unsigned)fold_char((int)y); }
        if (x != y) return x < y ? -1 : 1;
    }
    return kt_sign((long long)(alen - i) - (long long)(blen - j));
}

/* compare_digit_runs: two runs of digits as numbers, however long they are --
 * with the leading zeros gone the longer run is the larger number, and equal
 * lengths compare digit by digit. Runs of the same value are then separated by
 * the zeros themselves, so 01 and 1 are not the same string. */
static int compare_digit_runs(const char *a, size_t alen, const char *b, size_t blen) {
    size_t ai = 0, bi = 0;
    while (ai + 1 < alen && a[ai] == '0') ai++;
    while (bi + 1 < blen && b[bi] == '0') bi++;
    size_t an = alen - ai, bn = blen - bi;
    if (an != bn) return an < bn ? -1 : 1;
    for (size_t k = 0; k < an; k++)
        if (a[ai + k] != b[bi + k]) return a[ai + k] < b[bi + k] ? -1 : 1;
    return kt_sign((long long)alen - (long long)blen);
}

/* compare_natural: a run of ASCII digits is a number, everything else a folded
 * rune, so file9 comes before file10. */
static int compare_natural(const char *a, size_t alen, const char *b, size_t blen) {
    size_t i = 0, j = 0;
    while (i < alen && j < blen) {
        if (is_ascii_digit((unsigned char)a[i]) && is_ascii_digit((unsigned char)b[j])) {
            size_t si = i, sj = j;
            while (i < alen && is_ascii_digit((unsigned char)a[i])) i++;
            while (j < blen && is_ascii_digit((unsigned char)b[j])) j++;
            int c = compare_digit_runs(a + si, i - si, b + sj, j - sj);
            if (c) return c;
            continue;
        }
        unsigned x = utf8_next(a, alen, &i), y = utf8_next(b, blen, &j);
        x = (unsigned)fold_char((int)x);
        y = (unsigned)fold_char((int)y);
        if (x != y) return x < y ? -1 : 1;
    }
    return kt_sign((long long)(alen - i) - (long long)(blen - j));
}

/* compare_numbers: exact for two integers however large they are, and exact
 * for an integer against a float -- neither is converted to the other, since
 * casting the integer loses digits and casting the float loses the fraction
 * that decides it. */
static int compare_float_to_int(double f, long long i) {
    if (f != f) return 0; /* NaN compares equal to everything */
    if (f >= 9223372036854775808.0) return 1;
    if (f < -9223372036854775808.0) return -1;
    long long w = (long long)f; /* a cast truncates toward zero, which is what
                                 * the whole part is; no libm needed for it */
    if (w != i) return w < i ? -1 : 1;
    double frac = f - (double)w;
    return frac > 0 ? 1 : (frac < 0 ? -1 : 0);
}

static int compare_numbers(const kt_arg *a, const kt_arg *b) {
    if (a->kind == 0 && b->kind == 0)
        return a->ival < b->ival ? -1 : (a->ival > b->ival ? 1 : 0);
    if (a->kind == 0) return -compare_float_to_int(b->fval, a->ival);
    if (b->kind == 0) return compare_float_to_int(a->fval, b->ival);
    return a->fval < b->fval ? -1 : (a->fval > b->fval ? 1 : 0);
}

/* kt_compare_value orders two values: -1, 0 or 1. The collation applies to the
 * string rank alone, and NULL means exact. */
static int kt_compare_value(const kt_arg *a, const kt_arg *b, const char *collation) {
    int ra = rank_of(a), rb = rank_of(b);
    if (ra != rb) return ra < rb ? -1 : 1;
    switch (ra) {
    case KT_RANK_NUMBER: return compare_numbers(a, b);
    case KT_RANK_SYMBOL: return compare_runes(a->sval, a->slen, b->sval, b->slen, 0);
    case KT_RANK_STRING:
        if (collation && !strcmp(collation, KT_COLLATE_FOLD))
            return compare_runes(a->sval, a->slen, b->sval, b->slen, 1);
        if (collation && !strcmp(collation, KT_COLLATE_NATURAL))
            return compare_natural(a->sval, a->slen, b->sval, b->slen);
        return compare_runes(a->sval, a->slen, b->sval, b->slen, 0);
    case KT_RANK_BYTES: {
        size_t n = a->slen < b->slen ? a->slen : b->slen;
        int c = n ? memcmp(a->sval, b->sval, n) : 0;
        if (c) return c < 0 ? -1 : 1;
        return kt_sign((long long)a->slen - (long long)b->slen);
    }
    }
    return 0;
}

/* kt_compare_levels walks the levels in order and answers on the first that
 * separates the two runs, so each level settles only what the ones above it
 * left equal. The caller appends the record's key as a final level. */
typedef struct { int descending; const char *collation; } kt_level;

static int kt_compare_levels(const kt_arg *const *a, const kt_arg *const *b,
                             const kt_level *levels, int n) {
    for (int i = 0; i < n; i++) {
        int c = kt_compare_value(a[i], b[i], levels[i].collation);
        if (!c) continue;
        return levels[i].descending ? -c : c;
    }
    return 0;
}


struct kt_event { const char *type; const kt_arg *fields; int n; };

static void script_free(kt_script *sc);

static void args_free(kt_arg *args, int n) {
    for (int i = 0; i < n; i++) {
        free(args[i].name);
        free(args[i].sval);
        script_free(args[i].block);
    }
    free(args);
}

static void stmt_parts_free(kt_stmt *s) {
    free(s->key);
    free(s->verb);
    args_free(s->args, s->n);
}

static void script_free(kt_script *sc) {
    if (!sc) return;
    for (int i = 0; i < sc->n; i++) stmt_parts_free(&sc->stmts[i]);
    free(sc->stmts);
    free(sc);
}

static void stmt_free(kt_stmt *s) {
    if (!s) return;
    stmt_parts_free(s);
    free(s);
}

/* bad marks text the grammar will not read: a statement that does not begin
 * with a word, an unterminated block, a character where an argument belongs.
 * The whole statement is then refused rather than half-read. */
typedef struct { const char *s; size_t pos, len; int bad; } kt_p;

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

static int p_script(kt_p *p, kt_script *out, int in_block);

static void p_value(kt_p *p, kt_arg *a) {
    char c = p_peek(p);
    if (c == '{') {
        p->pos++;  /* '{' */
        kt_script *sc = calloc(1, sizeof *sc);
        p_script(p, sc, 1);
        if (!p_eof(p) && p_peek(p) == '}') p->pos++;
        else p->bad = 1;
        a->kind = 4; a->has_value = 1; a->block = sc;
    } else if (c == '"') {
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

/* One statement: `verb arg...`, ending at a `;`, at the end of the text, or --
 * inside a block -- at the closing brace. */
static int p_statement(kt_p *p, kt_stmt *st, int in_block) {
    p_skip_inline(p);
    if (p_eof(p) || !is_word_start(p_peek(p))) return 0;
    memset(st, 0, sizeof *st);
    st->verb = p_word(p);
    /* `key=verb args...`: a name for whatever the statement makes, answered in
     * the reply with the id the receiving end minted for it. */
    if (!p_eof(p) && p_peek(p) == '=') {
        size_t save = p->pos;
        p->pos++;
        if (!p_eof(p) && is_word_start(p_peek(p))) {
            st->key = st->verb;
            st->verb = p_word(p);
        } else {
            p->pos = save;
        }
    }
    int cap = 0;
    for (;;) {
        p_skip_inline(p);
        if (p_eof(p) || p_peek(p) == ';') break;
        char c = p_peek(p);
        if (in_block && c == '}') break;
        kt_arg a;
        memset(&a, 0, sizeof a);
        if (c == '!' || c == '?') {
            p->pos++;
            a.name = p_word(p);
            a.flag = (c == '?') ? KT_FLAG_INDET : KT_FLAG_FALSE;
        } else if (is_word_start(c)) {
            a.name = p_word(p);
            p_skip_inline(p);
            if (!p_eof(p) && p_peek(p) == '=') { p->pos++; p_value(p, &a); a.flag = KT_FLAG_NONE; }
            else a.flag = KT_FLAG_TRUE;
        } else if (c == '-' || isdigit((unsigned char)c) || c == '"' || c == '{') {
            /* An operand: a value with no name, in the order it was written.
             * A target reference is one, and so is a filter's field and what
             * it is matched against. */
            p_value(p, &a);
        } else { p->bad = 1; break; }
        if (st->n + 1 > cap) { cap = cap ? cap * 2 : 8; st->args = realloc(st->args, cap * sizeof(kt_arg)); }
        st->args[st->n++] = a;
    }
    return 1;
}

/* A run of statements, separated by `;` and by newlines. */
static int p_script(kt_p *p, kt_script *out, int in_block) {
    int cap = 0;
    for (;;) {
        p_skip_inline(p);
        while (!p_eof(p) && p_peek(p) == ';') { p->pos++; p_skip_inline(p); }
        if (p_eof(p)) break;
        if (in_block && p_peek(p) == '}') break;
        kt_stmt st;
        if (!p_statement(p, &st, in_block)) { p->bad = 1; break; }
        if (out->n + 1 > cap) { cap = cap ? cap * 2 : 4; out->stmts = realloc(out->stmts, cap * sizeof(kt_stmt)); }
        out->stmts[out->n++] = st;
    }
    return 1;
}

static kt_stmt *parse_statement(const char *text) {
    kt_p p = {text, 0, strlen(text), 0};
    kt_stmt *st = calloc(1, sizeof *st);
    if (!p_statement(&p, st, 0) || p.bad) {
        stmt_free(st);
        return NULL;
    }
    return st;
}

/* --- a query, in structured form -------------------------------------
 *
 * An application hosts a query: one filter and one sort over its own records,
 * which a display fills windows out of as somebody scrolls. The wire carries
 * that as text, and every client library would otherwise make its author walk
 * a statement tree to find out what was being asked. So the walking happens
 * once, here, and what reaches an application is a struct.
 *
 * ../testdata/query.wire is the corpus every implementation of this answers;
 * wire/query.go is the Go side and python/kittytk/query.py the Python one.
 */

#define KT_QERR 256

static void qfail(char *err, const char *fmt, ...) {
    if (!err) return;
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(err, KT_QERR, fmt, ap);
    va_end(ap);
}

kt_value kt_vint(const char *name, long long v) {
    kt_value o; memset(&o, 0, sizeof o);
    o.name = name; o.kind = KT_V_INT; o.ival = v; o.fval = (double)v;
    return o;
}
kt_value kt_vfloat(const char *name, double v) {
    kt_value o; memset(&o, 0, sizeof o);
    o.name = name; o.kind = KT_V_FLOAT; o.fval = v;
    return o;
}
kt_value kt_vstrn(const char *name, const char *s, size_t n) {
    kt_value o; memset(&o, 0, sizeof o);
    o.name = name; o.kind = KT_V_STRING; o.sval = s; o.slen = n;
    return o;
}
kt_value kt_vstr(const char *name, const char *s) {
    return kt_vstrn(name, s, s ? strlen(s) : 0);
}
kt_value kt_vword(const char *name, const char *w) {
    kt_value o; memset(&o, 0, sizeof o);
    o.name = name; o.kind = KT_V_WORD; o.sval = w; o.slen = w ? strlen(w) : 0;
    return o;
}

const kt_value *kt_bag_get(const kt_bag *b, const char *name) {
    if (!b) return NULL;
    for (int i = 0; i < b->n; i++)
        if (b->v[i].name && strcmp(b->v[i].name, name) == 0) return &b->v[i];
    return NULL;
}

const kt_value *kt_bag_key(const kt_bag *b) { return kt_bag_get(b, KT_KEY_FIELD); }

/* --- writing a value back out --- */

/* A float in as few digits as read it back exactly, and never in a spelling
 * that would come back an integer: `3` is an int and `3.0` is a float, and a
 * boundary that changed type between the two ends would be comparing different
 * things. Go and Python write the same two rules. */
static void enc_float(kt_buf *b, double v) {
    char tmp[64];
    int prec = 17;
    for (int i = 1; i <= 17; i++) {
        snprintf(tmp, sizeof tmp, "%.*g", i, v);
        if (strtod(tmp, NULL) == v) { prec = i; break; }
    }
    snprintf(tmp, sizeof tmp, "%.*g", prec, v);
    if (!strpbrk(tmp, ".eEnN")) strncat(tmp, ".0", sizeof tmp - strlen(tmp) - 1);
    buf_puts(b, tmp);
}

static void enc_value(kt_buf *b, const kt_value *v) {
    char tmp[64];
    switch (v->kind) {
    case KT_V_INT:
        snprintf(tmp, sizeof tmp, "%lld", v->ival);
        buf_puts(b, tmp);
        break;
    case KT_V_FLOAT:
        enc_float(b, v->fval);
        break;
    case KT_V_STRING: {
        char *t = kt_quote(v->sval);
        buf_puts(b, t);
        free(t);
        break;
    }
    case KT_V_WORD:
        buf_puts(b, v->sval ? v->sval : "");
        break;
    default:
        buf_puts(b, "undefined");
        break;
    }
}

static void enc_bag(kt_buf *b, const kt_bag *bag) {
    if (!bag || bag->n == 0) { buf_puts(b, "{}"); return; }
    buf_puts(b, "{ ");
    for (int i = 0; i < bag->n; i++) {
        if (i) buf_puts(b, "; ");
        buf_puts(b, bag->v[i].name ? bag->v[i].name : "");
        if (bag->v[i].kind != KT_V_NONE) {
            buf_put(b, ' ');
            enc_value(b, &bag->v[i]);
        }
    }
    buf_puts(b, " }");
}

static int is_group(const char *op) {
    return !strcmp(op, "and") || !strcmp(op, "or") || !strcmp(op, "not");
}

static void enc_filter_stmt(kt_buf *b, const kt_filter *f);

static void enc_filter_block(kt_buf *b, const kt_filter *f) {
    if (f->nchildren == 0) { buf_puts(b, "{}"); return; }
    buf_puts(b, "{ ");
    for (int i = 0; i < f->nchildren; i++) {
        if (i) buf_puts(b, "; ");
        enc_filter_stmt(b, &f->children[i]);
    }
    buf_puts(b, " }");
}

static void enc_filter_stmt(kt_buf *b, const kt_filter *f) {
    if (is_group(f->op)) {
        buf_puts(b, f->op);
        buf_put(b, ' ');
        enc_filter_block(b, f);
        return;
    }
    buf_puts(b, f->op);
    buf_put(b, ' ');
    buf_puts(b, f->field);
    for (int i = 0; i < f->nvalues; i++) {
        buf_put(b, ' ');
        enc_value(b, &f->values[i]);
    }
    if (f->collate && *f->collate) {
        buf_puts(b, " collate=");
        buf_puts(b, f->collate);
    }
}

static void enc_filter(kt_buf *b, const kt_filter *f) {
    if (!f) { buf_puts(b, "{}"); return; }
    if (is_group(f->op)) { enc_filter_block(b, f); return; }
    buf_puts(b, "{ ");
    enc_filter_stmt(b, f);
    buf_puts(b, " }");
}

static void enc_sort(kt_buf *b, const kt_sortlevel *l, int n) {
    if (n == 0) { buf_puts(b, "{}"); return; }
    buf_puts(b, "{ ");
    for (int i = 0; i < n; i++) {
        if (i) buf_puts(b, "; ");
        buf_puts(b, l[i].field);
        if (l[i].collation && *l[i].collation) { buf_put(b, ' '); buf_puts(b, l[i].collation); }
        if (l[i].descending) buf_puts(b, " desc");
    }
    buf_puts(b, " }");
}

static void enc_qspec(kt_buf *b, const kt_qspec *s) {
    int wrote = 0;
    if (s->source && *s->source) {
        buf_puts(b, "source=");
        char *q = kt_quote(s->source);
        buf_puts(b, q);
        free(q);
        wrote = 1;
    }
    if (s->fields.n) {
        if (wrote) buf_put(b, ' ');
        buf_puts(b, "fields="); enc_bag(b, &s->fields); wrote = 1;
    }
    if (s->exclude.n) {
        if (wrote) buf_put(b, ' ');
        buf_puts(b, "exclude="); enc_bag(b, &s->exclude); wrote = 1;
    }
    if (s->filter) {
        if (wrote) buf_put(b, ' ');
        buf_puts(b, "filter="); enc_filter(b, s->filter); wrote = 1;
    }
    if (s->nsort) {
        if (wrote) buf_put(b, ' ');
        buf_puts(b, "sort="); enc_sort(b, s->sort, s->nsort);
    }
}

static void enc_qfill(kt_buf *b, const kt_qfill *f) {
    char tmp[64];
    int wrote = 0;
    if (f->from.n) { buf_puts(b, "from="); enc_bag(b, &f->from); wrote = 1; }
    if (f->to.n) {
        if (wrote) buf_put(b, ' ');
        buf_puts(b, "to="); enc_bag(b, &f->to);
        wrote = 1;
    }
    snprintf(tmp, sizeof tmp, "%shave=%d need=%d", wrote ? " " : "", f->have, f->need);
    buf_puts(b, tmp);
    if (f->fields.n) { buf_puts(b, " fields="); enc_bag(b, &f->fields); }
}

/* --- reading one apart --- */

static void value_release(kt_value *v) {
    free((void *)v->name);
    free((void *)v->sval);
    memset(v, 0, sizeof *v);
}

static void bag_release(kt_bag *b) {
    for (int i = 0; i < b->n; i++) value_release((kt_value *)&b->v[i]);
    free((void *)b->v);
    b->v = NULL; b->n = 0;
}

static void filter_release(kt_filter *f) {
    if (!f) return;
    free((void *)f->op);
    free((void *)f->field);
    free((void *)f->collate);
    for (int i = 0; i < f->nvalues; i++) value_release((kt_value *)&f->values[i]);
    free((void *)f->values);
    for (int i = 0; i < f->nchildren; i++) filter_release((kt_filter *)&f->children[i]);
    free((void *)f->children);
    memset(f, 0, sizeof *f);
}

static void qspec_release(kt_qspec *s) {
    free((void *)s->source);
    bag_release(&s->fields);
    bag_release(&s->exclude);
    if (s->filter) { filter_release((kt_filter *)s->filter); free((void *)s->filter); }
    for (int i = 0; i < s->nsort; i++) {
        free((void *)s->sort[i].field);
        free((void *)s->sort[i].collation);
    }
    free((void *)s->sort);
    memset(s, 0, sizeof *s);
}

static void qfill_release(kt_qfill *f) {
    bag_release(&f->from);
    bag_release(&f->to);
    bag_release(&f->fields);
    memset(f, 0, sizeof *f);
}

static char *dupn(const char *s, size_t n) {
    char *o = malloc(n + 1);
    if (n && s) memcpy(o, s, n);
    o[n] = '\0';
    return o;
}

/* One operand as a value.
 *
 * A bare word in argument position is a flag as far as the grammar is
 * concerned -- that is what `wrap` and `!enabled` are -- so a word written
 * where a value belongs arrives as a flag carrying its own name, and this is
 * where it becomes the word it was written as. `!` and `?` say something a
 * value cannot, so they are refused rather than quietly read as words. */
static int value_of(const char *what, const kt_arg *a, kt_value *out, char *err) {
    memset(out, 0, sizeof *out);
    if (a->has_value) {
        if (a->name && *a->name) {
            qfail(err, "%s: no argument called \"%s\"", what, a->name);
            return 0;
        }
        switch (a->kind) {
        case 0: out->kind = KT_V_INT; out->ival = a->ival; out->fval = (double)a->ival; break;
        case 1: out->kind = KT_V_FLOAT; out->fval = a->fval; break;
        case 2: out->kind = KT_V_STRING; out->sval = dupn(a->sval, a->slen); out->slen = a->slen; break;
        case 3: out->kind = KT_V_WORD; out->sval = dupn(a->sval, a->slen); out->slen = a->slen; break;
        default:
            qfail(err, "%s: a block is not a value here", what);
            return 0;
        }
        return 1;
    }
    if (a->flag != KT_FLAG_TRUE) {
        qfail(err, "%s: \"%s\" is asserted, not valued", what, a->name ? a->name : "");
        return 0;
    }
    out->kind = KT_V_WORD;
    out->sval = strdup(a->name ? a->name : "");
    out->slen = strlen(out->sval);
    return 1;
}

static void bag_push(kt_bag *b, kt_value v) {
    b->v = realloc((void *)b->v, (b->n + 1) * sizeof(kt_value));
    ((kt_value *)b->v)[b->n++] = v;
}

/* A field bag, from a block value. */
static int parse_bag(const kt_arg *a, kt_bag *out, char *err) {
    memset(out, 0, sizeof *out);
    if (!a->has_value || a->kind != 4 || !a->block) {
        qfail(err, "expected a block of fields");
        return 0;
    }
    for (int i = 0; i < a->block->n; i++) {
        kt_stmt *st = &a->block->stmts[i];
        if (!st->verb || !*st->verb) { qfail(err, "a field is a name"); goto bad; }
        kt_value v;
        memset(&v, 0, sizeof v);
        if (st->n == 0) {
            v.kind = KT_V_NONE;
        } else if (st->n == 1) {
            if (!value_of(st->verb, &st->args[0], &v, err)) goto bad;
        } else {
            qfail(err, "%s: a field carries one value, not %d", st->verb, st->n);
            goto bad;
        }
        v.name = strdup(st->verb);
        bag_push(out, v);
    }
    return 1;
bad:
    bag_release(out);
    return 0;
}

static int parse_filter_block(const kt_script *sc, const char *op, kt_filter *out, char *err);

static int parse_predicate(const kt_stmt *st, kt_filter *out, char *err) {
    static const char *preds[] = {"eq", "ne", "lt", "le", "gt", "ge", "in",
                                  "contains", "starts", "ends", NULL};
    memset(out, 0, sizeof *out);
    if (is_group(st->verb)) {
        if (st->n != 1 || !st->args[0].has_value || st->args[0].kind != 4) {
            qfail(err, "%s: takes one block", st->verb);
            return 0;
        }
        if (!parse_filter_block(st->args[0].block, st->verb, out, err)) return 0;
        if (!strcmp(st->verb, "not") && out->nchildren == 0) {
            qfail(err, "not: takes something to negate");
            filter_release(out);
            return 0;
        }
        return 1;
    }
    int known = 0;
    for (int i = 0; preds[i]; i++) if (!strcmp(st->verb, preds[i])) { known = 1; break; }
    if (!known) {
        qfail(err, "no filter operator called \"%s\"", st->verb);
        return 0;
    }
    out->op = strdup(st->verb);
    out->field = strdup("");
    out->collate = strdup("");
    for (int i = 0; i < st->n; i++) {
        const kt_arg *a = &st->args[i];
        if (a->name && strcmp(a->name, "collate") == 0 && a->has_value) {
            if (a->kind != 3) { qfail(err, "%s: collate= expects a word", st->verb); goto bad; }
            free((void *)out->collate);
            out->collate = dupn(a->sval, a->slen);
            continue;
        }
        if (i == 0) {
            /* The field, written bare, which is how a filter reads as a filter
             * rather than naming an argument for every operand. */
            if (a->has_value || a->flag != KT_FLAG_TRUE) {
                qfail(err, "%s: names no field", st->verb);
                goto bad;
            }
            free((void *)out->field);
            out->field = strdup(a->name);
            continue;
        }
        if (a->has_value && a->kind == 4 && !strcmp(out->op, "in")) {
            /* A set of words, which is what a block can hold: every statement
             * in it is one bare name. */
            for (int j = 0; j < a->block->n; j++) {
                kt_stmt *item = &a->block->stmts[j];
                if (!item->verb || !*item->verb || item->n != 0) {
                    qfail(err, "in: a set holds bare names; write other values after the field");
                    goto bad;
                }
                kt_value v;
                memset(&v, 0, sizeof v);
                v.name = strdup("");
                v.kind = KT_V_WORD;
                v.sval = strdup(item->verb);
                v.slen = strlen(v.sval);
                out->values = realloc((void *)out->values, (out->nvalues + 1) * sizeof(kt_value));
                ((kt_value *)out->values)[out->nvalues++] = v;
            }
            continue;
        }
        kt_value v;
        if (!value_of(st->verb, a, &v, err)) goto bad;
        v.name = strdup("");
        out->values = realloc((void *)out->values, (out->nvalues + 1) * sizeof(kt_value));
        ((kt_value *)out->values)[out->nvalues++] = v;
    }
    if (!*out->field) { qfail(err, "%s: names no field", st->verb); goto bad; }
    if (out->nvalues == 0) {
        qfail(err, "%s %s: nothing to compare against", st->verb, out->field);
        goto bad;
    }
    if (strcmp(out->op, "in") != 0 && out->nvalues > 1) {
        qfail(err, "%s %s: compares against one value, not %d",
              st->verb, out->field, out->nvalues);
        goto bad;
    }
    return 1;
bad:
    filter_release(out);
    return 0;
}

static int parse_filter_block(const kt_script *sc, const char *op, kt_filter *out, char *err) {
    memset(out, 0, sizeof *out);
    out->op = strdup(op);
    out->field = strdup("");
    out->collate = strdup("");
    for (int i = 0; sc && i < sc->n; i++) {
        kt_filter child;
        if (!parse_predicate(&sc->stmts[i], &child, err)) { filter_release(out); return 0; }
        out->children = realloc((void *)out->children, (out->nchildren + 1) * sizeof(kt_filter));
        ((kt_filter *)out->children)[out->nchildren++] = child;
    }
    return 1;
}

/* A filter tree, from a block value. A block is an AND. */
static int parse_filter_arg(const kt_arg *a, kt_filter **out, char *err) {
    *out = NULL;
    if (!a->has_value || a->kind != 4) { qfail(err, "expected a block"); return 0; }
    kt_filter *f = calloc(1, sizeof *f);
    if (!parse_filter_block(a->block, "and", f, err)) { free(f); return 0; }
    *out = f;
    return 1;
}

/* Sort levels, from a block value: one statement per level, naming a field and
 * saying which way and under which collation. */
static int parse_sort_arg(const kt_arg *a, kt_sortlevel **out, int *n, char *err) {
    *out = NULL; *n = 0;
    if (!a->has_value || a->kind != 4) { qfail(err, "expected a block"); return 0; }
    for (int i = 0; a->block && i < a->block->n; i++) {
        kt_stmt *st = &a->block->stmts[i];
        if (!st->verb || !*st->verb) { qfail(err, "a level names a field"); goto bad; }
        kt_sortlevel lv;
        memset(&lv, 0, sizeof lv);
        lv.field = strdup(st->verb);
        lv.collation = strdup("");
        for (int j = 0; j < st->n; j++) {
            const kt_arg *g = &st->args[j];
            const char *nm = g->name ? g->name : "";
            if (g->has_value && !strcmp(nm, "collate") && g->kind == 3) {
                free((void *)lv.collation);
                lv.collation = dupn(g->sval, g->slen);
            } else if (g->has_value) {
                qfail(err, "%s: no argument called \"%s\"", st->verb, nm);
                free((void *)lv.field); free((void *)lv.collation);
                goto bad;
            } else if (!strcmp(nm, "desc")) {
                lv.descending = g->flag == KT_FLAG_TRUE;
            } else if (!strcmp(nm, "asc")) {
                lv.descending = g->flag != KT_FLAG_TRUE;
            } else if (!strcmp(nm, "exact") || !strcmp(nm, "fold") || !strcmp(nm, "natural")) {
                free((void *)lv.collation);
                lv.collation = strdup(nm);
            } else {
                qfail(err, "%s: \"%s\" says nothing about a sort level", st->verb, nm);
                free((void *)lv.field); free((void *)lv.collation);
                goto bad;
            }
        }
        *out = realloc(*out, (*n + 1) * sizeof(kt_sortlevel));
        (*out)[(*n)++] = lv;
    }
    return 1;
bad:
    for (int i = 0; i < *n; i++) { free((void *)(*out)[i].field); free((void *)(*out)[i].collation); }
    free(*out);
    *out = NULL; *n = 0;
    return 0;
}

/* A query spec, from the arguments of the statement carrying it. */
static int parse_qspec(const kt_arg *args, int n, kt_qspec *out, char *err) {
    memset(out, 0, sizeof *out);
    out->source = strdup("");
    for (int i = 0; i < n; i++) {
        const kt_arg *a = &args[i];
        const char *nm = a->name ? a->name : "";
        if (!strcmp(nm, "source")) {
            if (!a->has_value || (a->kind != 2 && a->kind != 3)) {
                qfail(err, "source: expected a name");
                goto bad;
            }
            free((void *)out->source);
            out->source = dupn(a->sval, a->slen);
        } else if (!strcmp(nm, "fields") || !strcmp(nm, "exclude")) {
            kt_bag bag;
            if (!parse_bag(a, &bag, err)) goto bad;
            if (!strcmp(nm, "fields")) { bag_release(&out->fields); out->fields = bag; }
            else { bag_release(&out->exclude); out->exclude = bag; }
        } else if (!strcmp(nm, "filter")) {
            kt_filter *f;
            if (!parse_filter_arg(a, &f, err)) goto bad;
            if (out->filter) { filter_release((kt_filter *)out->filter); free((void *)out->filter); }
            out->filter = f;
        } else if (!strcmp(nm, "sort")) {
            kt_sortlevel *lv; int ln;
            if (!parse_sort_arg(a, &lv, &ln, err)) goto bad;
            for (int j = 0; j < out->nsort; j++) {
                free((void *)out->sort[j].field);
                free((void *)out->sort[j].collation);
            }
            free((void *)out->sort);
            out->sort = lv; out->nsort = ln;
        }
    }
    return 1;
bad:
    qspec_release(out);
    return 0;
}

/* A fill request, from the arguments after the question word. */
static int parse_qfill(const kt_arg *args, int n, kt_qfill *out, char *err) {
    memset(out, 0, sizeof *out);
    for (int i = 0; i < n; i++) {
        const kt_arg *a = &args[i];
        const char *nm = a->name ? a->name : "";
        if (!strcmp(nm, "have") || !strcmp(nm, "need")) {
            if (!a->has_value || a->kind != 0) {
                qfail(err, "%s: expected a whole number", nm);
                goto bad;
            }
            if (!strcmp(nm, "have")) out->have = (int)a->ival;
            else out->need = (int)a->ival;
        } else if (!strcmp(nm, "from") || !strcmp(nm, "to") || !strcmp(nm, "fields")) {
            kt_bag bag;
            if (!parse_bag(a, &bag, err)) goto bad;
            if (!strcmp(nm, "from")) { bag_release(&out->from); out->from = bag; }
            else if (!strcmp(nm, "to")) { bag_release(&out->to); out->to = bag; }
            else { bag_release(&out->fields); out->fields = bag; }
        }
    }
    return 1;
bad:
    qfill_release(out);
    return 0;
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
    /* Window events name their source window= rather than trinket=, a store's
     * name it store=, and the display's name it host=. All of them are
     * ObjectIDs, and subscriptions key on the source whichever word names it. */
    static const char *named[] = {"trinket", "window", "store", "host"};
    for (size_t i = 0; i < sizeof named / sizeof named[0]; i++)
        if (kt_event_uint(ev, named[i], &v)) { if (ok) *ok = 1; return v; }
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

/* A body of records this application can serve, and what answers a window of
 * it. A name, not an object: nothing about registering one crosses the wire. */
struct kt_source {
    kt_conn *c;
    char *name;
    kt_fill_cb fill;       void *fill_ud;
    kt_respec_cb respec;   void *respec_ud;
    kt_hstmt_cb other;     void *other_ud;
    kt_dropped_cb dropped; void *dropped_ud;
};

/* One sequence of a source's records that a display is reading. The display
 * opens it; this application names it. */
struct kt_query {
    uint64_t id;
    kt_source *source;
    kt_qspec spec;
};

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

    /* given: every object the display has handed this connection, by the name
     * it knows it by -- its application, its store, its handle on the display,
     * and whatever else a display offers. One record rather than a field per
     * object, so a display that hands over something new reaches a client that
     * was never taught its name. Guarded by rmu: the read loop writes it. */
    kt_pair *given;
    int      ngiven;

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

    /* Statements the display addressed to something this application hosts:
     * the other direction of the wire. They get a thread of their own rather
     * than sharing the event one, because answering a fill can take as long as
     * the records take, and a list nobody is looking at must not hold up a
     * click. */
    kt_mutex imu; kt_cond icv;
    evnode *ihead, *itail;
    int istop;

    /* What this application serves, and what the display is currently reading
     * of it. Guarded by hmu, with the handlers.
     *
     * last_hosted_id names the queries in this application's own space: each
     * end mints its own ids, and the direction a statement travelled says
     * whose space it is in, so the two never have to be told apart. */
    kt_source **sources; int nsources;
    kt_query **queries;  int nqueries;
    uint64_t last_hosted_id;

    kt_mutex hmu;
    kt_handler *handlers; int nh, caph;
    kt_pair *subs; int nsubs, capsubs;
    char **subtypes;

    int closed;
    kt_thread rthread, ethread, ithread;
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

static int conn_write_all(kt_conn *c, const void *buf, size_t n);

/* Write without waiting for anything back, which is what the reverse direction
 * needs: this is the end answering, not asking.
 *
 * What goes out this way is never a batch: a request is terminated by `end`
 * and answered, and this end is answering. */
static int conn_send(kt_conn *c, const char *src) {
    kt_buf b;
    memset(&b, 0, sizeof b);
    buf_puts(&b, src);
    buf_put(&b, '\n');
    kt_mutex_lock(&c->write_mu);
    int rc = conn_write_all(c, b.p, b.len);
    kt_mutex_unlock(&c->write_mu);
    free(b.p);
    return rc;
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
/* hand_over records an init statement: every field of it is an object the
 * display has handed this connection, under the name it knows it by. A name
 * already in hand is replaced, because the display saying it again is the
 * display saying what that name means NOW. */
static void hand_over(kt_conn *c, const kt_stmt *st) {
    kt_mutex_lock(&c->rmu);
    for (int i = 0; i < st->n; i++) {
        if (!st->args[i].name || !st->args[i].has_value || st->args[i].kind != 0)
            continue;
        uint64_t id = (uint64_t)st->args[i].ival;
        int found = 0;
        for (int j = 0; j < c->ngiven; j++)
            if (strcmp(c->given[j].name, st->args[i].name) == 0) {
                c->given[j].id = id;
                found = 1;
                break;
            }
        if (!found) {
            c->given = realloc(c->given, (c->ngiven + 1) * sizeof(kt_pair));
            c->given[c->ngiven].name = strdup(st->args[i].name);
            c->given[c->ngiven].id = id;
            c->ngiven++;
        }
    }
    kt_mutex_unlock(&c->rmu);
}

uint64_t kt_init(kt_conn *c, const char *name) {
    if (!c || !name) return 0;
    uint64_t id = 0;
    kt_mutex_lock(&c->rmu);
    for (int j = 0; j < c->ngiven; j++)
        if (strcmp(c->given[j].name, name) == 0) { id = c->given[j].id; break; }
    kt_mutex_unlock(&c->rmu);
    return id;
}

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

static int do_exec(kt_conn *c, const char *src, kt_ui *out_ids,
                   char ***out_desc, int *out_ndesc);

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

    kt_mutex_lock(&c->imu);
    c->istop = 1;
    kt_cond_signal(&c->icv);
    kt_mutex_unlock(&c->imu);
}

/* --- serving a query: the questions, and where the answer is written ---
 *
 * The statement is taken apart before the application's callback sees it, so
 * nothing in that callback parses anything. The answer goes into a sink that
 * flushes as it fills, so a window larger than one message is neither held
 * whole in memory nor one uninterruptible stretch of work.
 */

/* How much answer accumulates before it goes out on its own. It trades write
 * syscalls against how long a record waits: big enough that a window of a
 * screenful is one message, small enough that a window of a million records is
 * not held in memory. */
#define KT_FLUSH_BYTES (16 * 1024)

struct kt_fill {
    kt_conn *c;
    uint64_t query;
    kt_mutex mu;
    kt_buf buf;
    int sent, ordered;
};

static void enqueue_inbound(kt_conn *c, const char *text) {
    evnode *n = malloc(sizeof *n);
    n->text = strdup(text);
    n->next = NULL;
    kt_mutex_lock(&c->imu);
    if (c->itail) c->itail->next = n; else c->ihead = n;
    c->itail = n;
    kt_cond_signal(&c->icv);
    kt_mutex_unlock(&c->imu);
}

/* --- the two tables, both under hmu --- */

static kt_source *source_named(kt_conn *c, const char *name) {
    for (int i = 0; i < c->nsources; i++)
        if (!strcmp(c->sources[i]->name, name)) return c->sources[i];
    return NULL;
}

static kt_query *query_with(kt_conn *c, uint64_t id) {
    for (int i = 0; i < c->nqueries; i++)
        if (c->queries[i]->id == id) return c->queries[i];
    return NULL;
}

static void query_release(kt_query *q) {
    qspec_release(&q->spec);
    free(q);
}

kt_source *kt_host_source(kt_conn *c, const char *name, kt_fill_cb cb, void *ud) {
    if (!c || !name || !*name || !cb) return NULL;
    kt_source *s = calloc(1, sizeof *s);
    s->c = c;
    s->name = strdup(name);
    s->fill = cb;
    s->fill_ud = ud;
    kt_mutex_lock(&c->hmu);
    c->sources = realloc(c->sources, (c->nsources + 1) * sizeof(kt_source *));
    c->sources[c->nsources++] = s;
    kt_mutex_unlock(&c->hmu);
    return s;
}

const char *kt_source_name(const kt_source *s) { return s ? s->name : NULL; }

void kt_source_on_respec(kt_source *s, kt_respec_cb cb, void *ud) {
    if (!s) return;
    kt_mutex_lock(&s->c->hmu);
    s->respec = cb; s->respec_ud = ud;
    kt_mutex_unlock(&s->c->hmu);
}

void kt_source_on_dropped(kt_source *s, kt_dropped_cb cb, void *ud) {
    if (!s) return;
    kt_mutex_lock(&s->c->hmu);
    s->dropped = cb; s->dropped_ud = ud;
    kt_mutex_unlock(&s->c->hmu);
}

void kt_source_on_statement(kt_source *s, kt_hstmt_cb cb, void *ud) {
    if (!s) return;
    kt_mutex_lock(&s->c->hmu);
    s->other = cb; s->other_ud = ud;
    kt_mutex_unlock(&s->c->hmu);
}

uint64_t kt_query_id(const kt_query *q) { return q ? q->id : 0; }
const kt_source *kt_query_source(const kt_query *q) { return q ? q->source : NULL; }
const kt_qspec *kt_query_spec(const kt_query *q) { return q ? &q->spec : NULL; }

/* --- the sink --- */

static kt_fill *fill_new(kt_conn *c, uint64_t query) {
    kt_fill *f = calloc(1, sizeof *f);
    f->c = c;
    f->query = query;
    kt_mutex_init(&f->mu);
    return f;
}

/* Empty the buffer and hand back what was in it. Called under f->mu. */
static char *fill_take(kt_fill *f) {
    char *src = buf_dup(&f->buf);
    free(f->buf.p);
    memset(&f->buf, 0, sizeof f->buf);
    return src;
}

static int conn_send(kt_conn *c, const char *src);

static int fill_send(kt_fill *f, char *src) {
    int rc = 0;
    if (src && *src) rc = conn_send(f->c, src);
    free(src);
    return rc;
}

/* The head of a result: the query it belongs to, addressed the way every other
 * statement addresses an object. */
static void fill_head(kt_fill *f, kt_buf *b) {
    char tmp[64];
    snprintf(tmp, sizeof tmp, "result %llu", (unsigned long long)f->query);
    buf_puts(b, tmp);
}

int kt_fill_record(kt_fill *f, kt_value key, const kt_value *fields, int n) {
    kt_buf b;
    memset(&b, 0, sizeof b);
    fill_head(f, &b);
    buf_puts(&b, " fields={ ");
    buf_puts(&b, KT_KEY_FIELD);
    buf_put(&b, ' ');
    enc_value(&b, &key);
    for (int i = 0; i < n; i++) {
        buf_puts(&b, "; ");
        buf_puts(&b, fields[i].name ? fields[i].name : "");
        if (fields[i].kind != KT_V_NONE) {
            buf_put(&b, ' ');
            enc_value(&b, &fields[i]);
        }
    }
    buf_puts(&b, " }");
    char *stmt = buf_dup(&b);
    free(b.p);

    kt_mutex_lock(&f->mu);
    if (f->buf.len) buf_put(&f->buf, '\n');
    buf_puts(&f->buf, stmt);
    free(stmt);
    f->sent++;
    if (f->buf.len < KT_FLUSH_BYTES) {
        kt_mutex_unlock(&f->mu);
        return 0;
    }
    char *src = fill_take(f);
    kt_mutex_unlock(&f->mu);
    return fill_send(f, src);
}

void kt_fill_ordered(kt_fill *f) {
    kt_mutex_lock(&f->mu);
    f->ordered = 1;
    kt_mutex_unlock(&f->mu);
}

int kt_fill_sent(const kt_fill *f) { return f->sent; }

int kt_fill_flush(kt_fill *f) {
    kt_mutex_lock(&f->mu);
    char *src = fill_take(f);
    kt_mutex_unlock(&f->mu);
    return fill_send(f, src);
}

/* Append the terminator, send the rest and release the sink. tail is what
 * distinguishes the three endings; it may be NULL.
 *
 * Releasing here is why exactly one ending may be called, and why nothing may
 * touch the sink afterwards: there is no sink afterwards. */
static int fill_finish(kt_fill *f, const char *tail) {
    kt_mutex_lock(&f->mu);
    kt_buf b;
    memset(&b, 0, sizeof b);
    fill_head(f, &b);
    /* `complete` ends the window; `ordered` rides on it, so it can be decided
     * after the records have been produced rather than promised before. */
    buf_puts(&b, " complete");
    if (f->ordered) buf_puts(&b, " ordered");
    if (tail) buf_puts(&b, tail);
    if (f->buf.len) buf_put(&f->buf, '\n');
    /* buf_dup, not b.p: a kt_buf holds a length and is not NUL-terminated. */
    char *line = buf_dup(&b);
    buf_puts(&f->buf, line);
    free(line);
    free(b.p);
    char *src = fill_take(f);
    kt_mutex_unlock(&f->mu);
    int rc = fill_send(f, src);
    free(f->buf.p);
    free(f);
    return rc;
}

int kt_fill_done(kt_fill *f, const kt_value *watermark, int n) {
    if (n <= 0) return fill_finish(f, NULL);
    kt_buf b;
    memset(&b, 0, sizeof b);
    buf_puts(&b, " watermark=");
    kt_bag bag = { watermark, n };
    enc_bag(&b, &bag);
    char *tail = buf_dup(&b);
    free(b.p);
    int rc = fill_finish(f, tail);
    free(tail);
    return rc;
}

int kt_fill_exhausted(kt_fill *f) { return fill_finish(f, " exhausted"); }

int kt_fill_fail(kt_fill *f, const char *message) {
    kt_buf b;
    memset(&b, 0, sizeof b);
    buf_puts(&b, " error=");
    char *q = kt_quote(message ? message : "");
    buf_puts(&b, q);
    free(q);
    char *tail = buf_dup(&b);
    free(b.p);
    int rc = fill_finish(f, tail);
    free(tail);
    return rc;
}

/* --- running a batch the display sent --- */

/* One thing to do once the batch has been replied to. */
typedef struct {
    int kind;            /* 0=fill 1=respec 2=dropped 3=other */
    kt_query *q;
    kt_fill *sink;
    kt_qfill req;
    char *text;
} kt_deferred;

typedef struct {
    kt_pair *keys; int nkeys;     /* what this batch surfaced, before the reply */
    kt_pair *ids;  int nids;      /* what the reply carries */
    kt_deferred *todo; int ntodo;
    char err[KT_QERR];
} kt_batch;

static void batch_key(kt_batch *b, const char *name, uint64_t id) {
    b->keys = realloc(b->keys, (b->nkeys + 1) * sizeof(kt_pair));
    b->keys[b->nkeys].name = strdup(name);
    b->keys[b->nkeys].id = id;
    b->nkeys++;
    b->ids = realloc(b->ids, (b->nids + 1) * sizeof(kt_pair));
    b->ids[b->nids].name = strdup(name);
    b->ids[b->nids].id = id;
    b->nids++;
}

static void batch_defer(kt_batch *b, kt_deferred d) {
    b->todo = realloc(b->todo, (b->ntodo + 1) * sizeof(kt_deferred));
    b->todo[b->ntodo++] = d;
}

/* The object a statement is addressed to: a bare id, or a key this same batch
 * surfaced -- which is what lets a display open a query and address it again
 * without waiting for the reply. 0 when it is addressed to nothing. */
static uint64_t hosted_target(const kt_stmt *st, const kt_batch *b) {
    if (st->n < 1) return 0;
    const kt_arg *a = &st->args[0];
    if (!(a->name && *a->name) && a->has_value && a->kind == 0 && a->ival >= 0)
        return (uint64_t)a->ival;
    if (b && !a->has_value && a->flag == KT_FLAG_TRUE && a->name) {
        for (int i = 0; i < b->nkeys; i++)
            if (!strcmp(b->keys[i].name, a->name)) return b->keys[i].id;
    }
    return 0;
}

/* Take a request for one window apart and queue serving it. */
static int batch_window(kt_conn *c, kt_batch *b, kt_query *q,
                        const kt_arg *args, int n) {
    kt_deferred d;
    memset(&d, 0, sizeof d);
    d.kind = 0;
    d.q = q;
    if (!parse_qfill(args, n, &d.req, b->err)) return 0;
    d.sink = fill_new(c, q->id);
    batch_defer(b, d);
    return 1;
}

/* Make a query and ask it for its first window, which is one statement because
 * the display never wants a sequence without wanting rows of it.
 *
 * The application names it. The display has no id to offer -- ids here are the
 * application's own -- so the reply is what carries it back. */
static int batch_open(kt_conn *c, kt_batch *b, const kt_stmt *st) {
    if (st->n < 1 || st->args[0].has_value || st->args[0].flag != KT_FLAG_TRUE) {
        qfail(b->err, "new: expected a type");
        return 0;
    }
    if (strcmp(st->args[0].name, "query") != 0) {
        qfail(b->err, "new: I host nothing called \"%s\"", st->args[0].name);
        return 0;
    }
    const kt_arg *args = st->n > 1 ? &st->args[1] : NULL;
    int n = st->n - 1;

    kt_qspec spec;
    if (!parse_qspec(args, n, &spec, b->err)) return 0;

    kt_mutex_lock(&c->hmu);
    kt_source *source = source_named(c, spec.source);
    if (!source) {
        kt_mutex_unlock(&c->hmu);
        qfail(b->err, "query: I serve nothing called \"%s\"", spec.source);
        qspec_release(&spec);
        return 0;
    }
    kt_query *q = calloc(1, sizeof *q);
    q->id = ++c->last_hosted_id;
    q->source = source;
    q->spec = spec;
    c->queries = realloc(c->queries, (c->nqueries + 1) * sizeof(kt_query *));
    c->queries[c->nqueries++] = q;
    kt_mutex_unlock(&c->hmu);

    return batch_window(c, b, q, args, n);
}

/* Route one statement. Anything meant to answer with records is deferred, so
 * the batch is replied to first. */
static int batch_one(kt_conn *c, kt_batch *b, const kt_stmt *st, const char *text) {
    if (!strcmp(st->verb, "new")) return batch_open(c, b, st);

    uint64_t id = hosted_target(st, b);
    const kt_arg *rest = st->n > 1 ? &st->args[1] : NULL;
    int nrest = st->n - 1;

    if (!strcmp(st->verb, "query")) {
        if (!id) { qfail(b->err, "query: expected the query to ask"); return 0; }
        kt_mutex_lock(&c->hmu);
        kt_query *q = query_with(c, id);
        kt_mutex_unlock(&c->hmu);
        if (!q) { qfail(b->err, "query %llu: no query of mine", (unsigned long long)id); return 0; }
        return batch_window(c, b, q, rest, nrest);
    }
    if (!id) return 1;  /* not addressed to anything this application holds */

    kt_mutex_lock(&c->hmu);
    kt_query *q = query_with(c, id);
    kt_mutex_unlock(&c->hmu);
    if (!q) {
        qfail(b->err, "%s %llu: no query of mine", st->verb, (unsigned long long)id);
        return 0;
    }

    kt_deferred d;
    memset(&d, 0, sizeof d);
    d.q = q;
    if (!strcmp(st->verb, "set")) {
        kt_qspec spec;
        if (!parse_qspec(rest, nrest, &spec, b->err)) return 0;
        kt_mutex_lock(&c->hmu);
        qspec_release(&q->spec);
        q->spec = spec;
        kt_mutex_unlock(&c->hmu);
        d.kind = 1;
        batch_defer(b, d);
        return 1;
    }
    if (!strcmp(st->verb, "destroy")) {
        kt_mutex_lock(&c->hmu);
        for (int i = 0; i < c->nqueries; i++) {
            if (c->queries[i] != q) continue;
            c->queries[i] = c->queries[--c->nqueries];
            break;
        }
        kt_mutex_unlock(&c->hmu);
        d.kind = 2;
        batch_defer(b, d);
        return 1;
    }
    d.kind = 3;
    d.text = strdup(text);
    batch_defer(b, d);
    return 1;
}

/* Run one batch the display sent, answer it, and then produce whatever records
 * it asked for.
 *
 * The reply goes out before any result does, because the reply is what names a
 * query the display has not heard of yet. That ordering is not policy: the
 * application mints the id, so it writes it before anything that carries it. */
static void run_batch(kt_conn *c, kt_stmt *stmts, const char **texts, int n) {
    kt_batch b;
    memset(&b, 0, sizeof b);
    int ok = 1;

    for (int i = 0; i < n; i++) {
        kt_stmt *st = &stmts[i];
        if (ok && !batch_one(c, &b, st, texts[i])) ok = 0;
        if (ok && !strcmp(st->verb, "new") && st->key && b.ntodo > 0)
            batch_key(&b, st->key, b.todo[b.ntodo - 1].q->id);
    }

    kt_buf out;
    memset(&out, 0, sizeof out);
    if (!ok) {
        buf_puts(&out, "error text=");
        char *q = kt_quote(b.err);
        buf_puts(&out, q);
        free(q);
    } else {
        buf_puts(&out, "reply");
        for (int i = 0; i < b.nids; i++) {
            char tmp[128];
            snprintf(tmp, sizeof tmp, " %s=%llu", b.ids[i].name,
                     (unsigned long long)b.ids[i].id);
            buf_puts(&out, tmp);
        }
    }
    char *line = buf_dup(&out);
    free(out.p);
    conn_send(c, line);
    free(line);

    for (int i = 0; i < b.ntodo; i++) {
        kt_deferred *d = &b.todo[i];
        kt_source *s = d->q->source;
        kt_mutex_lock(&c->hmu);
        kt_fill_cb fill = s->fill;         void *fill_ud = s->fill_ud;
        kt_respec_cb respec = s->respec;   void *respec_ud = s->respec_ud;
        kt_dropped_cb drop = s->dropped;   void *drop_ud = s->dropped_ud;
        kt_hstmt_cb other = s->other;      void *other_ud = s->other_ud;
        kt_mutex_unlock(&c->hmu);

        switch (d->kind) {
        case 0:
            d->req.spec = &d->q->spec;
            if (ok && fill) fill(d->q, &d->req, d->sink, fill_ud);
            else kt_fill_fail(d->sink, ok ? "this source has nothing to fill it" : b.err);
            qfill_release(&d->req);
            break;
        case 1:
            if (respec) respec(d->q, &d->q->spec, respec_ud);
            break;
        case 2:
            if (drop) drop(d->q, drop_ud);
            query_release(d->q);
            break;
        case 3:
            if (other) other(d->q, d->text, other_ud);
            free(d->text);
            break;
        }
    }
    for (int i = 0; i < b.nkeys; i++) free(b.keys[i].name);
    for (int i = 0; i < b.nids; i++) free(b.ids[i].name);
    free(b.keys);
    free(b.ids);
    free(b.todo);
}

/* The inbound thread: batches the display sent, in the order they arrived. A
 * thread of its own rather than the event one, because serving a window can
 * take as long as the records take and a list nobody is looking at must not
 * hold up a click. */
static void *inbound_loop(void *arg) {
    kt_conn *c = arg;
    kt_stmt *stmts = NULL;
    char **texts = NULL;
    int n = 0, cap = 0;
    for (;;) {
        kt_mutex_lock(&c->imu);
        while (!c->ihead && !c->istop) kt_cond_wait(&c->icv, &c->imu);
        if (!c->ihead && c->istop) { kt_mutex_unlock(&c->imu); break; }
        evnode *node = c->ihead;
        c->ihead = node->next;
        if (!c->ihead) c->itail = NULL;
        kt_mutex_unlock(&c->imu);

        if (!strcmp(node->text, "end")) {
            if (n) {
                run_batch(c, stmts, (const char **)texts, n);
                for (int i = 0; i < n; i++) { stmt_parts_free(&stmts[i]); free(texts[i]); }
                n = 0;
            }
        } else {
            kt_stmt *st = parse_statement(node->text);
            if (st) {
                if (n + 1 > cap) {
                    cap = cap ? cap * 2 : 8;
                    stmts = realloc(stmts, cap * sizeof(kt_stmt));
                    texts = realloc(texts, cap * sizeof(char *));
                }
                stmts[n] = *st;
                /* The statement as it was scanned, without the newline that
                 * separated it from the next: what reaches a handler is the
                 * statement, not the ragged end of a read. */
                {
                    size_t len = strlen(node->text);
                    while (len && (node->text[len - 1] == '\n' ||
                                   node->text[len - 1] == '\r')) len--;
                    texts[n] = dupn(node->text, len);
                }
                n++;
                free(st);
            }
        }
        free(node->text);
        free(node);
    }
    for (int i = 0; i < n; i++) { stmt_parts_free(&stmts[i]); free(texts[i]); }
    free(stmts);
    free(texts);
    return NULL;
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
        } else if (strcmp(st->verb, "init") == 0) {
            /* The display handing over something: a new object, or a new
             * object under a name already in hand. Not only a handshake step
             * -- the display says it whenever it has something to give. */
            hand_over(c, st);
        } else if (strcmp(st->verb, "event") == 0 && st->n > 0
                   && !st->args[0].has_value) {
            /* Two different lines start with this word: an event the display
             * raised, whose type is the bare word after it, and a describe
             * stream's record of an event a type CAN raise, which leads with
             * of="...". The first argument tells them apart. */
            enqueue_event(c, text);
        } else if (strcmp(st->verb, "new") == 0 ||
                   strcmp(st->verb, "query") == 0 ||
                   ((strcmp(st->verb, "set") == 0 ||
                     strcmp(st->verb, "destroy") == 0 ||
                     strcmp(st->verb, "sub") == 0 ||
                     strcmp(st->verb, "unsub") == 0 ||
                     strcmp(st->verb, "ask") == 0 ||
                     strcmp(st->verb, "do") == 0) && hosted_target(st, NULL) != 0)) {
            /* The other direction: the display making, asking after, or letting
             * go of something this application holds. Two different lines start
             * with `ask` and with `do` -- a question put to an object, and the
             * describe stream's record of a question a type CAN answer -- and
             * being addressed to an object is what tells them apart.
             *
             * Gathered by the inbound thread until `end`, because a batch is
             * what gets a reply. */
            enqueue_inbound(c, text);
        } else if (strcmp(st->verb, "end") == 0) {
            enqueue_inbound(c, "end");
        } else if (strcmp(st->verb, "proptype") == 0 ||
                   strcmp(st->verb, "prop") == 0 ||
                   strcmp(st->verb, "propcommon") == 0 ||
                   strcmp(st->verb, "ask") == 0 ||
                   strcmp(st->verb, "askarg") == 0 ||
                   strcmp(st->verb, "do") == 0 ||
                   strcmp(st->verb, "doarg") == 0 ||
                   strcmp(st->verb, "event") == 0 ||
                   strcmp(st->verb, "eventfield") == 0) {
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

uint64_t kt_app_id(kt_conn *c) { return kt_init(c, "app"); }

int kt_set_app(kt_conn *c, const char *props) {
    if (!c || !kt_init(c, "app")) return -1;
    /* By the name the display knows it by, as the Go and Python clients do. */
    char buf[512];
    snprintf(buf, sizeof(buf), "set app %s", props ? props : "");
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
static void fill_field(kt_field *a, const kt_stmt *st) {
    a->name = strdup(stmt_str(st, "name"));
    a->kind = strdup(stmt_str(st, "kind"));
    a->doc  = strdup(stmt_str(st, "doc"));
}
static void field_free(kt_field *a) { free(a->name); free(a->kind); free(a->doc); }

/* type_named finds the type a describe line belongs to, or NULL. */
static kt_type *type_named(kt_vocab *v, const char *name) {
    for (int k = 0; k < v->ntypes; k++)
        if (strcmp(v->types[k].name, name) == 0) return &v->types[k];
    return NULL;
}

/* call_named finds a question or action by name within one list. */
static kt_call *call_named(kt_call *calls, int n, const char *name) {
    for (int i = 0; i < n; i++)
        if (strcmp(calls[i].name, name) == 0) return &calls[i];
    return NULL;
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
            memset(t, 0, sizeof *t);
            t->name = strdup(stmt_str(st, "name"));
            t->is_virtual = stmt_flag_true(st, "virtual");
            t->is_hosted = stmt_flag_true(st, "hosted");
        } else if (strcmp(st->verb, "prop") == 0) {
            kt_type *t = type_named(v, stmt_str(st, "of"));
            if (t) {
                t->props = realloc(t->props, (t->nprops + 1) * sizeof(kt_prop));
                fill_prop(&t->props[t->nprops++], st);
            }
        } else if (strcmp(st->verb, "ask") == 0 || strcmp(st->verb, "do") == 0) {
            kt_type *t = type_named(v, stmt_str(st, "of"));
            if (t) {
                int is_ask = strcmp(st->verb, "ask") == 0;
                kt_call **list = is_ask ? &t->asks : &t->does;
                int *n = is_ask ? &t->nasks : &t->ndoes;
                *list = realloc(*list, (*n + 1) * sizeof(kt_call));
                kt_call *call = &(*list)[(*n)++];
                memset(call, 0, sizeof *call);
                call->name = strdup(stmt_str(st, "name"));
                call->doc = strdup(stmt_str(st, "doc"));
                call->answers = strdup(is_ask ? stmt_str(st, "answers") : "");
            }
        } else if (strcmp(st->verb, "askarg") == 0 || strcmp(st->verb, "doarg") == 0) {
            kt_type *t = type_named(v, stmt_str(st, "of"));
            if (t) {
                int is_ask = strcmp(st->verb, "askarg") == 0;
                kt_call *call = is_ask
                    ? call_named(t->asks, t->nasks, stmt_str(st, "ask"))
                    : call_named(t->does, t->ndoes, stmt_str(st, "do"));
                if (call) {
                    call->args = realloc(call->args, (call->nargs + 1) * sizeof(kt_field));
                    fill_field(&call->args[call->nargs++], st);
                }
            }
        } else if (strcmp(st->verb, "event") == 0) {
            kt_type *t = type_named(v, stmt_str(st, "of"));
            if (t) {
                t->events = realloc(t->events, (t->nevents + 1) * sizeof(kt_event_info));
                kt_event_info *e = &t->events[t->nevents++];
                memset(e, 0, sizeof *e);
                e->name = strdup(stmt_str(st, "name"));
                e->doc = strdup(stmt_str(st, "doc"));
            }
        } else if (strcmp(st->verb, "eventfield") == 0) {
            kt_type *t = type_named(v, stmt_str(st, "of"));
            if (t) {
                const char *named = stmt_str(st, "event");
                for (int j = 0; j < t->nevents; j++)
                    if (strcmp(t->events[j].name, named) == 0) {
                        kt_event_info *e = &t->events[j];
                        e->fields = realloc(e->fields, (e->nfields + 1) * sizeof(kt_field));
                        fill_field(&e->fields[e->nfields++], st);
                        break;
                    }
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
        kt_type *t = &v->types[i];
        for (int j = 0; j < t->nprops; j++) prop_free(&t->props[j]);
        free(t->props);
        kt_call *lists[2] = {t->asks, t->does};
        int counts[2] = {t->nasks, t->ndoes};
        for (int l = 0; l < 2; l++)
            for (int j = 0; j < counts[l]; j++) {
                kt_call *call = &lists[l][j];
                for (int k = 0; k < call->nargs; k++) field_free(&call->args[k]);
                free(call->args);
                free(call->name); free(call->doc); free(call->answers);
            }
        free(t->asks); free(t->does);
        for (int j = 0; j < t->nevents; j++) {
            kt_event_info *e = &t->events[j];
            for (int k = 0; k < e->nfields; k++) field_free(&e->fields[k]);
            free(e->fields);
            free(e->name); free(e->doc);
        }
        free(t->events);
        free(t->name);
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
uint64_t kt_store_id(kt_conn *c) { return kt_init(c, "store"); }
uint64_t kt_host_id(kt_conn *c) { return kt_init(c, "host"); }

int kt_ask(kt_conn *c, uint64_t id, const char *question) {
    char *src = malloc(strlen(question) + 32);
    sprintf(src, "ask %llu %s", (unsigned long long)id, question);
    int r = kt_exec(c, src);
    free(src);
    return r;
}

int kt_do(kt_conn *c, uint64_t id, const char *action) {
    char *src = malloc(strlen(action) + 32);
    sprintf(src, "do %llu %s", (unsigned long long)id, action);
    int r = kt_exec(c, src);
    free(src);
    return r;
}
int kt_destroy(kt_conn *c, uint64_t id) {
    char src[32];
    snprintf(src, sizeof src, "destroy %llu", (unsigned long long)id);
    return kt_exec(c, src);
}

/* --- the store (mirrors the Go client's Store/Blob) ------------------- */

int kt_store_write(kt_conn *c, const char *key, const char *type,
                   const void *data, size_t n) {
    if (!c) return -1;
    char *qk = kt_quote(key), *qd = kt_quote_blob(data, n);
    size_t len = strlen(qk) + strlen(qd) + strlen(type) + 64;
    char *args = malloc(len);
    snprintf(args, len, "blobs={ new blob key=%s type=%s data=%s }", qk, type, qd);
    int r = kt_set(c, kt_init(c, "store"), args);
    free(args); free(qk); free(qd);
    return r;
}

int kt_store_list(kt_conn *c) {
    if (!c) return -1;
    return kt_ask(c, kt_init(c, "store"), "inventory");
}

int kt_blob_append(kt_conn *c, uint64_t blob, const void *data, size_t n) {
    char *qd = kt_quote_blob(data, n);
    size_t len = strlen(qd) + 32;
    char *action = malloc(len);
    snprintf(action, len, "append bytes=%s", qd);
    int r = kt_do(c, blob, action);
    free(action); free(qd);
    return r;
}

int kt_blob_replace(kt_conn *c, uint64_t blob, const void *data, size_t n) {
    char *qd = kt_quote_blob(data, n);
    size_t len = strlen(qd) + 32;
    char *args = malloc(len);
    snprintf(args, len, "data=%s", qd);
    int r = kt_set(c, blob, args);
    free(args); free(qd);
    return r;
}

int kt_blob_read(kt_conn *c, uint64_t blob, long long offset) {
    char q[48];
    snprintf(q, sizeof q, "bytes offset=%lld", offset);
    return kt_ask(c, blob, q);
}

int kt_blob_drop(kt_conn *c, uint64_t blob) { return kt_destroy(c, blob); }


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
    kt_mutex_init(&c->imu); kt_cond_init(&c->icv);
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
    }
    stmt_free(st);
    free(welcome);
    if (!ok) { KTDBG("dial app=%s: bad welcome", app_name); goto fail; }

    /* Then an init statement: what this connection was handed, one field per
     * object. Every field of it is an object, so a client reads them all
     * without being taught the names -- a display that hands over a fourth
     * thing is reachable with no client change (kt_init).
     *
     * This first one is read here so the ids are in hand before kt_dial
     * returns. Later ones arrive on the read loop like anything else. */
    char *init = scan_next(c);
    if (!init) { KTDBG("dial app=%s: reading init failed", app_name); goto fail; }
    kt_stmt *ist = parse_statement(init);
    int iok = ist && strcmp(ist->verb, "init") == 0;
    if (iok) hand_over(c, ist);
    stmt_free(ist);
    free(init);
    if (!iok) { KTDBG("dial app=%s: bad init", app_name); goto fail; }
    KTDBG("dial app=%s: handed %d objects, connection ready", app_name, c->ngiven);

    kt_thread_create(&c->rthread, read_loop, c);
    kt_thread_create(&c->ethread, event_loop, c);
    kt_thread_create(&c->ithread, inbound_loop, c);
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
    for (int i = 0; i < c->ngiven; i++) free(c->given[i].name);
    free(c->given);
    free(c);
}
