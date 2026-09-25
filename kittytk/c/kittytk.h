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
   `ask <id> bytes offset=2048`.

   It carries NO correlation key, so the answer carries none either and nothing
   here routes it -- which suits an asker that is not waiting, and nothing else.
   kt_ask_for is the one to use to be told. */
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

/* --- the store ---------------------------------------------------------
 *
 * Two flows, and which one a thing takes is settled by whether anybody asked.
 *
 * The inventory and a blob's bytes were ASKED FOR, so they are ANSWERED: the
 * two functions below take a kt_answer_cb, and nothing need be subscribed to
 * hear them. A change to a blob was not asked for, so the store reports it as
 * an event -- which is where the id to continue a large write with comes from. */

/* One `answer` statement, and where one is delivered. Declared here because the
   store's two questions take a callback; what an answer HOLDS is read through
   the accessors further down, which need kt_value first. */
typedef struct kt_answer kt_answer;
typedef void (*kt_answer_cb)(const kt_answer *a, void *userdata);

/* The events the store raises. All of them name the store as their source, so
   one kt_on(c, kt_store_id(c), ...) per type hears everything. None of them
   answers a question. */
#define KT_STORE_BLOB  "store_blob"  /* one blob: what it is and how big */
#define KT_STORE_GONE  "store_gone"  /* a blob is no longer there */
#define KT_STORE_ERROR "store_error" /* what went wrong, and with which key */

/* A key beginning with this names something the desktop may throw away at any
   moment, the way `#` names a temporary table in SQL. */
#define KT_CACHE_MARK "#"

/* Put a blob in the store under key, replacing whatever it held. type is one
   of txt, psl, bin, ini or conf. The store then reports a KT_STORE_BLOB naming
   the id the blob can be addressed by, which is how something larger than one
   statement is continued -- see kt_blob_append. */
int kt_store_write(kt_conn *c, const char *key, const char *type,
                   const void *data, size_t n);

/* Ask what the store holds: cb is called once per blob, carrying blob=, key=,
   type=, size= and hash=, and once more for the completion, which carries
   count=. The completion arrives even for a store holding nothing, which is an
   answer and not a refusal. */
int kt_store_list(kt_conn *c, kt_answer_cb cb, void *userdata);

/* Add to the end of a blob, replace it whole, ask for the chunk starting at
   offset, or take it out of the store. A blob id is learned from an inventory's
   answers or from a KT_STORE_BLOB; an app never invents one.

   A read is answered ONCE, by the chunk that starts at offset: it carries
   blob=, key=, type=, offset=, size=, data= and last=, and reading further on
   is a fresh kt_blob_read from where this one reached. */
int kt_blob_append(kt_conn *c, uint64_t blob, const void *data, size_t n);
int kt_blob_replace(kt_conn *c, uint64_t blob, const void *data, size_t n);
int kt_blob_read(kt_conn *c, uint64_t blob, long long offset,
                 kt_answer_cb cb, void *userdata);
int kt_blob_drop(kt_conn *c, uint64_t blob);

/* --- serving a query --------------------------------------------------
 *
 * Everything above points one way: the application says `new`, `set`, `ask`,
 * `do`, and the display raises events at it. A query points the other way.
 * Only the display knows a query is wanted and what it is -- the sort comes
 * from the column header somebody clicked, the filter from the filter box, the
 * scope from the scroll position -- so the display opens it, and the
 * application, which is the end that holds the records, serves it.
 *
 * It arrives nowhere near the event line. `query` is answered by `result`,
 * `ask` by `answer`, and `sub` -- or an object's mere existence -- by `event`;
 * nothing carries two of them, so a request for records can never be mistaken
 * for something a subscription raised.
 *
 * What an author has to write is one function: given a scope of the sequence,
 * produce the records in it. The statement is taken apart before it gets here,
 * so nothing in that function parses anything; and the answer is written into
 * a sink that goes out in batches as it fills, so a million records need not be
 * one message, or one uninterruptible piece of work.
 *
 * See docs/hosting-a-query.md.
 */

/* What kind of thing a value is. A named field with no value of its own -- the
   way a bare list of fields is written -- is KT_V_NONE. */
typedef enum {
    KT_V_NONE = 0,
    KT_V_INT,
    KT_V_FLOAT,
    KT_V_STRING,
    KT_V_WORD,
    KT_V_BLOCK
} kt_vkind;

/* One named value: a field of a record, a coordinate of a boundary, or an
   operand of a filter (whose name is ""). sval is NUL-terminated and slen is
   its byte length, so a string with an interior NUL is not cut short. */
typedef struct {
    const char *name;
    kt_vkind    kind;
    long long   ival;
    double      fval;
    const char *sval;
    size_t      slen;
} kt_value;

/* --- and being answered ------------------------------------------------
 *
 * kt_ask leaves the answer to whatever events the question declared. This is the
 * other way: the question carries a correlation key, the answers quote it back,
 * and an application with two questions outstanding can tell them apart.
 *
 *     q1=ask <tree> amendments
 *     -> answer to=q1 id=1 how=altered fields={ kind "Archive" }
 *        answer to=q1 complete count=1
 *
 * An answer is not an event: nothing has to have subscribed to receive one,
 * because asking IS the subscription.
 */

/* kt_answer is one `answer` statement, declared with the store above. Opaque,
   and read through the accessors here, the way an event is -- so what a
   statement is made of stays this library's.

   Which question this answers: the key the ask carried, or "" for an unkeyed
   one. */
const char *kt_answer_to(const kt_answer *a);

/* Whether this is the LAST answer for that question.
   It arrives whether or not anything came before it: a question that answered
   with nothing has still been answered, which is a different fact from one still
   being worked on, and the only thing that tells them apart. */
int kt_answer_complete(const kt_answer *a);

/* Why the question was refused, or NULL where it was not. A refusal is still an
   answer and it completes: the question was put, so it has one, and an asker
   must not wait forever because the answer happened to be no. */
const char *kt_answer_error(const kt_answer *a);

/* The question's own arguments, read the way an event's fields are. Each
   returns 0 / NULL / KT_FLAG_NONE for an argument the answer has not got, and
   for one that is there in another kind.

   kt_answer_text_n is the one to use for BYTES: an answer carrying a blob's
   contents carries the NULs among them, and the terminated form stops at the
   first. */
int kt_answer_uint(const kt_answer *a, const char *name, uint64_t *out);
int kt_answer_int(const kt_answer *a, const char *name, long long *out);
const char *kt_answer_text(const kt_answer *a, const char *name);
const char *kt_answer_text_n(const kt_answer *a, const char *name, size_t *len);
const char *kt_answer_word(const kt_answer *a, const char *name);
kt_flag kt_answer_flag(const kt_answer *a, const char *name);

/* The RECORD an answer carries, where it carries one: its members, and *n their
   count. NULL where the answer carries none, which is the ordinary case -- a
   question answering with something other than records.
 *
 * `whole` is set where the answer said `record=` rather than `fields=`: every
 * member the record has, against some of them. A caller writing these out has to
 * know which it is holding.
 *
 * The members are valid until the callback returns. A nested block among them is
 * not reported, a record of records being nothing this reads. */
const kt_value *kt_answer_fields(const kt_answer *a, int *n, int *whole);

/* Put a question and call cb for each piece of the answer, ending with the one
   that completes. Returns 0 on success.

   The correlation key is minted here and never written by a caller: it exists so
   two answers cannot be confused, which is a job for whoever is doing the
   confusing. */
int kt_ask_for(kt_conn *c, uint64_t id, const char *question,
               kt_answer_cb cb, void *userdata);

/* --- what a source says has stopped being true ------------------------
 *
 * The other direction from everything else an application says. A result
 * answers a question the display put; this is the application speaking first,
 * because only it knows its records changed and nothing on the other end can
 * find out. Invalidation is TOLD, never decided -- there is no poll here, no
 * expiry and no generation compared.
 *
 *     stale source="papers"                              nothing of it is trusted
 *     stale source="papers" id=42 how=removed            one record, and it has left
 *     stale source="papers" id=42 how=altered fields={ size }
 *     stale source="papers" how=altered fields={ size }  any record's size may have moved
 *
 * Widening is free and narrowing is fatal. A NULL id is every record of the
 * source, and an alteration naming no field is every field -- so a source that
 * cannot tell what moved says the wider thing and pays a refill. Saying less
 * than happened is a missed invalidation: silent, and permanent. */

/* The four things that can have happened, and what each costs. The same four an
   amendment is held under, because they are the same four facts.

   A removal is CHEAPER than a replacement and that is most of what the word is
   for: a record that has left the sequence leaves everything between a run's
   ends still there, so the completeness claim survives, while one that may have
   moved takes the run with it. */
typedef enum {
    KT_CHANGE_ADDED = 0,   /* a record appeared; nothing is known of it */
    KT_CHANGE_REMOVED,     /* it has left the sequence altogether */
    KT_CHANGE_REPLACED,    /* it is still there and everything about it may differ */
    KT_CHANGE_ALTERED      /* the named fields may have changed */
} kt_change;

/* The word for one of them, as the wire spells it. NULL for a value that is not
   one of the four. */
const char *kt_change_word(kt_change c);

/* Say something about a source's records has stopped being true.

   source is the name this application serves them under. id names one record,
   or NULL for every record of the source. fields names what an alteration
   touched, and is refused beside any other reason -- on those the values are
   gone entire, so a list of them would contradict the word beside it.

   Returns 0 on success. */
int kt_source_stale(kt_conn *c, const char *source, const kt_value *id,
                    kt_change how, const char *const *fields, int nfields);

/* Value constructors, for building an answer. They copy nothing: the strings
   must outlive the call that carries them. */
kt_value kt_vint(const char *name, long long v);
kt_value kt_vfloat(const char *name, double v);
kt_value kt_vstr(const char *name, const char *s);
kt_value kt_vstrn(const char *name, const char *s, size_t n);
kt_value kt_vword(const char *name, const char *w);

/* A bag of named values, and one shape serves three jobs: the fields a record
   carries, the position a boundary stands at, and -- with the values left out
   -- the bare list of fields a query asks for. An empty bag (n == 0) standing
   for a boundary means the start of the sequence. */
typedef struct { const kt_value *v; int n; } kt_bag;

/* The value under a name, or NULL where the bag does not name it. */
const kt_value *kt_bag_get(const kt_bag *b, const char *name);

/* KT_ID_ARG carries a record's identity, beside its fields rather than among
   them.

   An identity is not a field. A record can hold a field called `key` and that
   field is data like any other -- it sorts, it filters, it is shown in a column
   -- while what names the record travels here. */
#define KT_ID_ARG "id"

/* What a result carries, and how much of the record it is.

   `record` is every field the record has; `fields` is some of them. The
   difference is worth a word because a whole record answers any question about
   that record, and a subset answers only the one that asked for it -- which is
   what lets an answer be kept and reused rather than asked for again. */
/* OP_ID matches a record's identity against a set of them, the way `in`
   matches a field against a set of values. It names no field, because an
   identity is not one: it travels beside a record's fields rather than among
   them, and a field called `key` is a field like any other.

       filter={ id (left/1) (left/note) } */
#define KT_OP_ID "id"

#define KT_RECORD_ARG "record"
#define KT_FIELDS_ARG "fields"

/* How many members the record HAS -- by name and by position -- whether or not
   they were all sent. They ride beside `fields=` and never beside `record=`, a
   whole record being its own totals.

   `map` and `len` because those are already the two halves of a PSL node, which
   is what a record is read out of: its items and its keyed members. A count of
   nothing is not written. */
#define KT_MAP_ARG "map"
#define KT_LEN_ARG "len"

/* KT_PLACE_VERB carries a PLACE: a record's identity, in its position in the
   sequence, and whatever is known of it so far with no claim about how much.

   A verb of its own, and that is the whole mechanism. Places are ADDITIONAL --
   every record still arrives as a result, and the scope's own `complete` still
   ends the answer -- so a reader that does not know this verb skips these
   statements and is left with exactly the answer it gets otherwise. There is
   nothing to negotiate and nothing it can be misled about, because it never saw
   them. */
#define KT_PLACE_VERB "place"

/* How many records the whole SEQUENCE has, and whether that figure is the whole
   story rather than a floor.

   Weak by default and strengthened out loud, the same way round as `fields`
   against `record`: `total=20` alone says there are at LEAST twenty;
   `total=20 exact` says twenty is all there are. It rides a completion, being a
   fact about the order rather than about any record. A figure of nothing is not
   written; `total=0 exact` is a sequence counted and found empty, and is. */
#define KT_FIRST_ARG "first"
#define KT_TOTAL_ARG "total"
#define KT_EXACT_ARG "exact"

/* KT_EXTEND_ARG is the display saying it will HOLD the places it is sent, so a
   result may leave out what its place already carried.

   It rides the query because it is about how this asker reads rather than about
   which records it wants, and it is the ASKER's to say for the one reason that
   matters: a reader that dropped the places would then silently lose fields.
   Only the end doing the dropping can promise not to.

   Saying nothing is `replace`, where every result carries the lot. Nothing an
   author writes changes either way -- place what you have, send the record when
   you have it, and the leaving-out happens in the library. */
#define KT_EXTEND_ARG "extend"

/* Why a scope ended, which the asker cannot work out for itself: a scope that
   filled and one that ran out of records look identical from the far end, and
   they mean opposite things about whether there is any point asking again. */
#define KT_STOP_FILLED "filled"        /* the count was reached */
#define KT_STOP_JOINED "joined"        /* the walk reached `until` */
#define KT_STOP_EXHAUSTED "exhausted"  /* no more records, so no watermark */

/* One node of a filter tree: a predicate over one field, or an and/or/not over
   other nodes. A block is an AND, so the top of a parsed filter always is --
   one shape to walk, whether it held one predicate or twenty. */
typedef struct kt_filter kt_filter;
struct kt_filter {
    const char *op;          /* and or not eq ne lt le gt ge in contains starts ends */
    const char *field;       /* predicates: the field tested; "" for a group */
    const kt_value *values;  /* what it is tested against */
    int nvalues;             /* more than one only for `in` */
    const char *collate;     /* text predicates; "" is the default */
    const kt_filter *children;
    int nchildren;
};

/* One level of a sort: which field, which way, and -- for strings -- under
   which collation ("" is the default, exact). */
typedef struct {
    const char *field;
    int descending;
    const char *collation;
} kt_sortlevel;

/* What sequence a query names. Stated when the query is announced, and
   restated when the display changes it, which is a new generation of the same
   query rather than a new query. */
typedef struct {
    const char *source;
    kt_bag fields;           /* the fields asked for; empty means whatever a record has */
    kt_bag exclude;
    const kt_filter *filter; /* NULL where there is none */
    const kt_sortlevel *sort;
    int nsort;

} kt_descriptor;

/* The run of records a query asks for: where to start, which way to walk, how
 * many, and where the asker's own knowledge picks up again.
 *
 * It is not a filter and it names no field. The sequence is already decided by
 * the descriptor, and a scope only says which part of it to read.
 *
 * after and until are identities, not positions: an identity means something
 * only to the source that issued it. Start past after, send count records, and
 * stop early if you reach until -- a record the asker already holds, and
 * everything beyond it with it.
 *
 * reversed walks the sequence from its end rather than its beginning. Every
 * level turns over, the one the sort does not write included, which is what
 * makes it the exact mirror -- `sort={ size desc }` turns one level over and
 * leaves ties facing the way they were, a different sequence again. It names no
 * field, so it is the one way to turn over a sequence whose records are read in
 * a way that cannot name their identity at all.
 *
 * from is a position to start NEAR, for a reader that has a place in mind
 * rather than a record: a thumb dragged down a long sequence knows how far down
 * it is and knows no identity there at all. Best effort and never a promise --
 * `first` on the answer says where the scope really began, and a reader that
 * meant somewhere else asks again from what it learned. Nought is the
 * beginning, which is where a scope starts anyway, so an unset one asks for
 * nothing; after wins over it, and a scope naming both is refused.
 *
 * It is valid for the length of the callback and freed after it returns. */
typedef struct {
    const kt_value *after;   /* NULL: the first record in walk order */
    const kt_value *until;   /* NULL: walk to the count or to the end */
    int count;
    int from;                /* 0: the beginning, which asks for nothing */
    int reversed;
    const kt_descriptor *descriptor;    /* the sequence, so a handler need not have kept it */
} kt_qscope;

/* Where the answer is written. Unlike the request, it may be kept and used
   after the callback returns, which is what lets an answer be produced over as
   long as it takes.

   Exactly one of kt_fill_filled, kt_fill_joined, kt_fill_exhausted and
   kt_fill_fail ends it, and
   that call RELEASES the sink: nothing may touch it afterwards, and an answer
   that is never ended is a leak. The Go and Python clients refuse a second
   ending instead; C has nothing left to refuse with. */
typedef struct kt_fill kt_fill;

/* One whole record: its identity, and every field it has.

   Whole matters beyond this answer. A record that arrived entire answers any
   question about that record, so whoever asked can keep it and use it for the
   next query as well; part of one answers only the question that asked for it.
   So say kt_fill_record when these are all the fields there are, and
   kt_fill_subset when they are the ones somebody asked for.

   The identity names the record and travels beside the fields rather than
   among them: a record is free to carry a field called `key` of its own, and
   that field is data like any other. */
int kt_fill_record(kt_fill *f, kt_value id, const kt_value *fields, int n);

/* Some of a record: its identity, HOW MANY MEMBERS THE RECORD HAS, and the
   fields this query asked for. It crosses as `fields={ ... } map=5 len=3`.

   The totals are what keep a subset worth more than the one question it
   answered. Say a record has five named members and send two, and whoever asked
   knows three are missing; send the other three later and they know they hold
   the lot. Say a record has three members standing by POSITION and they know
   `0`, `1` and `2` are all there is, so `3` is answered without anyone being
   asked -- an ordered member being named by where it stands.

   Count members only. A name sent as undefined is a guarantee that the record
   has NOT got it, which is worth sending rather than leaving out, and it is not
   counted: counting it would say the record had a member it has not.

   Either count may be zero, and a zero is not written. */
/* Add a PLACE: a record's position in the sequence, and whatever is known of it
   so far.

       kt_fill_place(f, kt_vint("", 17), fields, 1);

   **Its fields are true and its silence is not.** What is sent can be believed;
   what is missing is not a claim that the record has not got it. Which is the
   whole difference between this and kt_fill_subset, and why a place carries no
   totals.

   Use it for a row whose position is known sooner than its contents, so that
   whoever asked can lay out its rows and stay reactive while the values arrive
   behind them. A record you can send outright is worth sending outright.

   **Places are additional, never substitutional.** Every record still arrives
   as a result before the answer ends, so a reader that does not know this verb
   skips these and is left with exactly the answer it would have got. */
int kt_fill_place(kt_fill *f, kt_value id, const kt_value *fields, int n);

/* Say the ORDER is settled: every record of this scope has now been named,
   under kt_fill_place or as a result, and no further one will turn up between
   two already sent.

       kt_fill_placed(f, KT_STOP_FILLED, kt_vint("", 42));

   A watermark of kind KT_V_NONE is no watermark, which is what a sequence
   exhausted has: there is nothing past the end to be complete up to.

   It carries the same claim the terminator will -- how the walk ended, and the
   watermark where there is one -- and is worth sending only where the order
   settles SOONER than the answer does. A reader cannot lay out a sequence, not
   even one of placeholders, until it knows it has all the rows; where the two
   moments are the same there is nothing to send, because the terminator settles
   the order too. */
int kt_fill_placed(kt_fill *f, const char *stop, kt_value watermark);

/* Say how many records the whole SEQUENCE has, which rides out on whatever ends
   this answer.

       kt_fill_total(f, 20, 1);   -- twenty, counted
       kt_fill_total(f, 20, 0);   -- twenty so far, and there may be more

   Optional, and about the sequence rather than this scope of it: how many came
   back is something whoever asked can count. Say it where you know it cheaply
   and say nothing where you do not. */
void kt_fill_total(kt_fill *f, int n, int exact);

/* Say where in the sequence this answer BEGAN: the position of its first record,
   counted in the sequence's own order however the scope walked it.

       kt_fill_first(f, 900);   -- it starts at the nine hundredth record

   **It is what answers `from`.** A scope carrying a position asks to begin NEAR
   somewhere, and a source honours that as well as it can -- which for an
   application walking its own body may be not at all, there being no index into a
   sequence the display named. Saying where the answer actually began is what turns
   "as well as it can" into something a reader can use.

   **Silence means the beginning**, for a scope that asked for a position: that is
   what a source which ignored it did, and it is the only reading that cannot
   misplace a record. So an application that HONOURS `from` is the one with
   something to say here, and a naive one has nothing to do -- answering from the
   top and sending more records than were wanted is slower and is never wrong.

   There is nothing to say for a scope that named `after` instead: the record it
   starts past is the position, said better. */
void kt_fill_first(kt_fill *f, int at);

int kt_fill_subset(kt_fill *f, kt_value id, const kt_value *fields, int n,
                   int named, int ordered);

/* Declare that the records are being sent in the query's own order.
   It is the one hint that cannot be left unsaid and assumed, because it
   changes what the display does with what arrives: ordered, it merges the
   records as they stand; unordered, it sorts them first. Saying nothing means
   unordered, which is always safe. */
void kt_fill_ordered(kt_fill *f);

/* Finish with the count reached, and a watermark: there is nothing of mine
   between where you asked from and this record that you do not now have.

   The watermark is what the next scope is asked from, so it names a record this
   source sent, or one it is otherwise prepared to place. A completeness
   guarantee
   rather than a position, and what lets the display shrink the scope, grow it
   back and scroll inside it without asking anything. */
int kt_fill_filled(kt_fill *f, kt_value watermark);

/* Finish at the record the display said it already held. What it holds on this
   side and what it holds on that are now one run. */
int kt_fill_joined(kt_fill *f, kt_value watermark);

/* Finish with everything there is: no watermark, because there is nothing past
   the end to be complete up to. What the simplest implementation says -- ignore
   every hint, send all your records, say this -- and not a toy: the display
   then holds the whole layer and asks nothing again until something
   invalidates it. */
int kt_fill_exhausted(kt_fill *f);

/* Finish with a refusal. A refusal is an answer: the display carries on with
   what it has. */
int kt_fill_fail(kt_fill *f, const char *message);

/* Send what has accumulated without finishing, and how many records have gone
   into the answer so far. */
int kt_fill_flush(kt_fill *f);
int kt_fill_sent(const kt_fill *f);

/* One sequence of a source's records that a display is reading. The display
   opens it; this application names it, because the ids in every statement that
   follows are this application's own.

   A query does not change. It is stated when it is made and it is that sequence
   until it is destroyed; a different sort or a different filter is a different
   query. So the id IS the generation, and results still in flight when the
   display changes its mind are told from the new ones by the number they are
   addressed to. */
typedef struct kt_query kt_query;

typedef void (*kt_fill_cb)(kt_query *q, const kt_qscope *req, kt_fill *sink, void *ud);
typedef void (*kt_hstmt_cb)(kt_query *q, const char *text, void *ud);
typedef void (*kt_dropped_cb)(kt_query *q, void *ud);

/* A body of records this application can serve, under the name a display asks
   for it by. It is not an object and has no id: the application says
   `data="files"` on whatever trinket is to show it, and the display opens
   queries against that name when somebody scrolls.

   Registering one says nothing on the wire. Returns NULL on failure. */
typedef struct kt_source kt_source;
kt_source *kt_provide_source(kt_conn *c, const char *name, kt_fill_cb cb, void *ud);
const char *kt_source_name(const kt_source *s);

/* A handler for the display letting a query go, which is one reader finishing.

   Records are held against the SOURCE rather than against any one query, so an
   application that materialised something may let it go when the last query
   against that source has gone -- which is why a display opens the query it is
   replacing something with before destroying the old one. */
void kt_source_on_dropped(kt_source *s, kt_dropped_cb cb, void *ud);

/* A handler for anything else the display addresses to one of this source's
   queries: a question this library does not know, an action, a property it
   does not read. The statement arrives as its text.

   This is the seam a fuller library is built on. Coverage and invalidation
   both travel this way and neither is implemented here, so a library that
   wants them adds them without this one having to grow. */
void kt_source_on_statement(kt_source *s, kt_hstmt_cb cb, void *ud);

/* What a query is: which source it reads, and the sequence as it stands. */
uint64_t kt_query_id(const kt_query *q);
const kt_source *kt_query_source(const kt_query *q);
const kt_descriptor *kt_query_spec(const kt_query *q);

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

/* One question a type answers or one action it performs. One shape, because
 * they are the same declaration with a different verb in front of it -- the
 * verb is the only difference there is. */
typedef struct {
    char   *name;
    char   *doc;
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
