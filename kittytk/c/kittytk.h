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

/* Render arbitrary bytes as a protocol string, escaping every one that is not
   printable ASCII. kt_quote takes a C string and lets high bytes through,
   which is right for text and wrong for a blob: a blob may hold NUL, and bytes
   that are not valid UTF-8 do not survive being read back as text. Caller
   frees. */
char *kt_quote_blob(const void *data, size_t n);

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

/* Tell an object to do something: kt_do(c, id, "tile") sends `do <id> tile`.
   Nothing comes back from it -- that is what separates an action from a
   question -- though what it changes may raise the object's events. */
int kt_do(kt_conn *c, uint64_t id, const char *action);

/* Put a question to an object: kt_ask(c, id, "bytes offset=2048") sends
   `ask <id> bytes offset=2048`. The answer arrives as the events the question
   declares it answers with, so register for those (kt_on) before asking. */
int kt_ask(kt_conn *c, uint64_t id, const char *question);

/* --- the objects the connection is handed ------------------------------ */

/* The display sends an `init` statement after the welcome, one field per
   object it has handed this connection: its application, its store, its handle
   on the display, and whatever else that display offers. Every field of it is
   an object, so a client reads them all without being taught the names.

   kt_init is the ObjectID under one of those names, 0 for a name the display
   has not handed over. The name is also a session key the display bound, so
   `set <name> ...` says the same thing as the id does; the id is what reaches
   the object when a client has taken that name for something of its own.

   init is not only a handshake step: the display says it again whenever it has
   something new to give, or a new object to put under a name already in hand,
   and this answers with what that name means now. */
uint64_t kt_init(kt_conn *c, const char *name);

/* The three this header wraps, by name. */
uint64_t kt_store_id(kt_conn *c);
uint64_t kt_host_id(kt_conn *c);

/* --- the store --------------------------------------------------------- */

/* The events the store answers with. All of them name the store as their
   source, so one kt_on(c, kt_store_id(c), ...) per type hears everything. */
#define KT_STORE_BLOB  "store_blob"  /* one blob: what it is and how big */
#define KT_STORE_DONE  "store_done"  /* the end of an inventory */
#define KT_STORE_DATA  "store_data"  /* one chunk of a blob being read back */
#define KT_STORE_GONE  "store_gone"  /* a blob is no longer there */
#define KT_STORE_ERROR "store_error" /* what went wrong, and with which key */

/* A key beginning with this names something the desktop may throw away at any
   moment, the way `#` names a temporary table in SQL. */
#define KT_CACHE_MARK "#"

/* Put a blob in the store under key, replacing whatever it held. type is one
   of txt, psl, bin, ini or conf. The answer is a KT_STORE_BLOB naming the id
   the blob can be addressed by, which is how something larger than one
   statement is continued -- see kt_blob_append. */
int kt_store_write(kt_conn *c, const char *key, const char *type,
                   const void *data, size_t n);

/* Ask what the store holds: a KT_STORE_BLOB per blob, then a KT_STORE_DONE
   saying how many there were. */
int kt_store_list(kt_conn *c);

/* Add to the end of a blob, replace it whole, ask for the chunk starting at
   offset, or take it out of the store. A blob id is learned from an answer;
   an app never invents one. */
int kt_blob_append(kt_conn *c, uint64_t blob, const void *data, size_t n);
int kt_blob_replace(kt_conn *c, uint64_t blob, const void *data, size_t n);
int kt_blob_read(kt_conn *c, uint64_t blob, long long offset);
int kt_blob_drop(kt_conn *c, uint64_t blob);

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

/* One named argument of a question or an action, or one field of an event. */
typedef struct {
    char *name;
    char *kind;
    char *doc;
} kt_field;

/* One question a type answers or one action it performs. They are the same
 * shape because they are the same declaration with a different verb in front
 * of it -- except that an action answers nothing, so `answers` is "". */
typedef struct {
    char   *name;
    char   *doc;
    char   *answers;  /* comma-separated event names; "" for an action */
    kt_field *args;
    int       nargs;
} kt_call;

/* One event a type raises, and what it carries. */
typedef struct {
    char   *name;
    char   *doc;
    kt_field *fields;
    int       nfields;
} kt_event_info;

typedef struct {
    char    *name;
    int      is_virtual;
    /* is_hosted marks a type the wire cannot construct: the host registers an
     * instance and hands over its ID, and `new <name>` is refused. */
    int      is_hosted;
    kt_prop *props;
    int      nprops;
    kt_call *asks;    /* the questions it answers (ask) */
    int      nasks;
    kt_call *does;    /* the actions it performs (do) */
    int      ndoes;
    kt_event_info *events;
    int      nevents;
} kt_type;

typedef struct {
    kt_prop *common;   /* properties every non-virtual type accepts */
    int      ncommon;
    kt_type *types;
    int      ntypes;
} kt_vocab;

/* Query the host's wire vocabulary: the supported trinket types and, for each,
 * the properties it accepts with each property's kind, default and a brief
 * description, the questions it answers, the actions it performs, and the
 * events it raises. NULL on error. Free with kt_vocab_free. */
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
