/* kittytk.h - the KittyTK display-protocol client, in C.
 *
 * A pure-protocol client (no rendering): it speaks the identical wire
 * language the Go and Python clients do, so a C program drives the same
 * display host (kittytk-tui / kittytk-sdl).
 *
 * Transports: an endpoint is a bare path or unix:/path (unix socket,
 * default), tcp://host:port (plaintext), or tls://host:port (TLS). TLS
 * is compiled in only with -DKT_TLS (needs OpenSSL: -lssl -lcrypto);
 * without it, a tls:// dial fails. On POSIX link -lpthread; on Windows
 * link ws2_32.
 */
#ifndef KITTYTK_H
#define KITTYTK_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Flag state of a bare-name argument (values match the wire meaning). */
typedef enum {
    KT_FLAG_NONE = 0,   /* carries a value (name=value) */
    KT_FLAG_TRUE = 1,   /* bare name: `wrap` */
    KT_FLAG_FALSE = 2,  /* negated name: `!enabled` */
    KT_FLAG_INDET = 3   /* asserted-indeterminate: `?checked` */
} kt_flag;

typedef struct kt_conn kt_conn;
typedef struct kt_ui kt_ui;
typedef struct kt_event kt_event;

/* --- string quoting -------------------------------------------------- */

/* Quote s as a protocol string literal (quotes + escapes, control bytes
 * as \xNN). Returns a malloc'd string; caller frees. */
char *kt_quote(const char *s);

/* --- connection ------------------------------------------------------ */

/* The conventional endpoint ($KITTYTK_DISPLAY, else
 * $XDG_RUNTIME_DIR/kittytk/display-0.sock). malloc'd; caller frees. */
char *kt_default_endpoint(void);
char *kt_default_socket_path(void); /* historical alias of the above */

/* Connect to a display service at endpoint (a path or unix:/tcp://tls://
 * URL). Returns NULL on failure. dial_solo asks to be the whole display
 * (its `main` window replaces the desktop). */
kt_conn *kt_dial(const char *endpoint, const char *app_name);
kt_conn *kt_dial_solo(const char *endpoint, const char *app_name);

/* Full-control dial. opts may be NULL (same as kt_dial). */
typedef struct {
    int solo;                /* be the whole display */
    const char *token;       /* handshake token; NULL -> $KITTYTK_TOKEN */
    int insecure;            /* tls://: skip fingerprint pinning */
    const char *known_hosts; /* tls://: pin store; NULL -> default */
} kt_dial_opts;
kt_conn *kt_dial_ex(const char *endpoint, const char *app_name, const kt_dial_opts *opts);

void kt_close(kt_conn *c);
int  kt_is_closed(kt_conn *c);       /* 1 once disconnected */
void kt_wait_closed(kt_conn *c);     /* block until the connection ends */

/* This connection's Application ObjectID, from the handshake (0 if none).
 * Use it to address application-wide properties. */
uint64_t kt_app_id(kt_conn *c);
/* Apply application-wide properties with the same syntax as any object:
 * kt_set_app(c, "multiwindow contextonly") sends `set <app_id> ...`.
 * Returns 0 on success, -1 with no app id / on error. */
int kt_set_app(kt_conn *c, const char *props);

/* --- requests -------------------------------------------------------- */

/* Execute one batch of protocol text. Returns 0 on success, -1 on a
 * display error / disconnect. */
int kt_exec(kt_conn *c, const char *src);

/* Build a construction script; returns handle access to the surfaced
 * names (NULL on error). Free with kt_ui_free. */
kt_ui *kt_build(kt_conn *c, const char *src);
uint64_t kt_ui_id(const kt_ui *ui, const char *name);   /* 0 if absent */
void kt_ui_free(kt_ui *ui);

/* Property set / destroy on one object. */
int kt_set(kt_conn *c, uint64_t id, const char *args);
int kt_destroy(kt_conn *c, uint64_t id);

/* --- introspection (describe, D24) ----------------------------------- */

/* One property in a described vocabulary. All strings are owned by the
 * kt_vocab and freed by kt_vocab_free. */
typedef struct {
    char *name;
    char *kind;     /* string/int/float/flag/enum/word/color/units/stream/action */
    char *deflt;    /* default: a literal, or "inherited"/"as-noted"/"" */
    char *doc;      /* brief, tooltip-length */
    char *enums;    /* comma-separated allowed words, "" unless kind is enum */
} kt_prop;

typedef struct {
    char    *name;
    int      is_virtual;
    kt_prop *props;
    int      nprops;
} kt_type;

typedef struct {
    kt_prop *common;   /* properties every non-virtual type accepts */
    int      ncommon;
    kt_type *types;
    int      ntypes;
} kt_vocab;

/* Query the host's wire vocabulary: the supported trinket types and,
 * for each, the properties it accepts with each property's kind,
 * default, and a brief description. NULL on error. Free with
 * kt_vocab_free. */
kt_vocab *kt_describe(kt_conn *c);
void kt_vocab_free(kt_vocab *v);

/* --- events ---------------------------------------------------------- */

const char *kt_event_type(const kt_event *ev);
/* Field readers: return 1 and fill *out when present with the right type. */
int kt_event_uint(const kt_event *ev, const char *name, uint64_t *out);
int kt_event_int(const kt_event *ev, const char *name, long long *out);
const char *kt_event_text(const kt_event *ev, const char *name); /* NULL if absent */
/* Like kt_event_text but reports the byte length in *len, so a value with an
 * interior NUL (a \x00 escape on the wire) is not truncated at the NUL. */
const char *kt_event_text_n(const kt_event *ev, const char *name, size_t *len);
const char *kt_event_word(const kt_event *ev, const char *name); /* NULL if absent */
kt_flag kt_event_flag(const kt_event *ev, const char *name);
uint64_t kt_event_trinket(const kt_event *ev, int *ok);

typedef void (*kt_event_cb)(const kt_event *ev, void *userdata);
typedef void (*kt_command_cb)(void *userdata);

/* Subscribe to an event type from a specific object. */
void kt_on(kt_conn *c, uint64_t id, const char *event_type, kt_event_cb cb, void *ud);
/* Observe command events carrying the given action id. */
void kt_on_command(kt_conn *c, const char *action, kt_command_cb cb, void *ud);

#ifdef __cplusplus
}
#endif

#endif /* KITTYTK_H */
