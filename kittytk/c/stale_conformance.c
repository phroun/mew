/* stale_conformance.c - the C client answering the shared stale corpus.
 *
 * ../testdata/stale.wire holds what a source says has stopped being true and
 * the structure it has to come out as, two lines per case. Every client library
 * takes that text apart, and they have to arrive at the same structure or the
 * same notice means three things.
 *
 * stdout: one line per failure, then `cases N` and `DONE`.
 *
 * It includes the client rather than linking it, so it reaches the same parser
 * and the same encoder the client's own writing path uses. The taking-apart
 * lives here rather than in the library because nothing in a C application
 * READS a notice -- an application writes them, and the display is the end that
 * reads -- so a parser in the library would be dead code.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

static int failures = 0;
static int cases = 0;

/* A notice taken apart, for reading one back.
 *
 * `id` is borrowed from the statement and is valid while it is. `fields` points
 * at the statement's own block, so nothing here copies and nothing frees. */
typedef struct {
    const char *source;
    const kt_arg *id;      /* NULL for a notice about every record */
    kt_change    how;
    const kt_arg *fields;  /* NULL where it named none */
} kt_stale;

/* stale_of reads a notice from a statement. Returns 0 for a statement that is
 * not one -- an argument it does not know, a reason that is not one of the
 * four, or a `fields=` contradicting the reason beside it.
 *
 * Refused rather than half-read: quietly dropping one half of a contradiction
 * is how a source comes to believe it said something it did not. */
static int stale_of(kt_stmt *st, kt_stale *out) {
    memset(out, 0, sizeof *out);
    out->how = KT_CHANGE_REPLACED; /* the widest claim about one named record */
    int said = 0;

    for (int i = 0; i < st->n; i++) {
        kt_arg *a = &st->args[i];
        if (!a->name) return 0;
        if (strcmp(a->name, "source") == 0) {
            if (!a->has_value || a->kind != 2) return 0;
            out->source = a->sval;
        } else if (strcmp(a->name, "id") == 0) {
            if (!a->has_value) return 0;
            out->id = a;
        } else if (strcmp(a->name, "how") == 0) {
            if (!a->has_value || a->kind != 3) return 0;
            if (!change_named(a->sval, a->slen, &out->how)) return 0;
            said = 1;
        } else if (strcmp(a->name, "fields") == 0) {
            out->fields = a;
        } else {
            return 0;
        }
    }
    if (!out->source || !*out->source) return 0;
    if (out->fields) {
        if (!said || out->how != KT_CHANGE_ALTERED) return 0;
        /* It names FIELDS, not their values: a value here would read as an
         * assertion about what the record now holds, which a notice never
         * makes. The short string spelling is a list of names by construction. */
        if (out->fields->has_value && out->fields->kind == 4) {
            kt_script *blk = out->fields->block;
            for (int i = 0; blk && i < blk->n; i++) {
                kt_stmt *f = &blk->stmts[i];
                if (!f->verb || !*f->verb || f->n != 0) return 0;
            }
        } else if (!out->fields->has_value || out->fields->kind != 2) {
            return 0;
        }
    }
    return 1;
}

/* rendered writes the structure back out, in the canonical order and spelling:
 * source, the record where it names one, the reason always, and what an
 * alteration touched. The reason is written even where the statement left it
 * out, which is what makes the corpus show what silence meant. */
static void rendered(kt_buf *b, const kt_stale *n, kt_stmt *st) {
    buf_puts(b, "source=");
    char *q = kt_quote(n->source);
    buf_puts(b, q);
    free(q);
    if (n->id) {
        buf_puts(b, " id=");
        kt_value v;
        memset(&v, 0, sizeof v);
        switch (n->id->kind) {
        case 0: v.kind = KT_V_INT;    v.ival = n->id->ival; break;
        case 1: v.kind = KT_V_FLOAT;  v.fval = n->id->fval; break;
        case 2: v.kind = KT_V_STRING; v.sval = n->id->sval; v.slen = n->id->slen; break;
        default: v.kind = KT_V_WORD;  v.sval = n->id->sval; v.slen = n->id->slen; break;
        }
        enc_value(b, &v);
    }
    buf_puts(b, " how=");
    buf_puts(b, kt_change_word(n->how));
    if (!n->fields) return;

    /* The names, from either spelling: a block of bare names, or the short
     * comma-separated string a query's own fields= also takes. */
    buf_puts(b, " fields=");
    if (n->fields->kind == 4) {
        kt_script *blk = n->fields->block;
        int count = blk ? blk->n : 0;
        if (count == 0) { buf_puts(b, "{}"); return; }
        kt_value *named = calloc((size_t)count, sizeof *named);
        for (int i = 0; i < count; i++) {
            named[i].name = blk->stmts[i].verb;
            named[i].kind = KT_V_NONE;
        }
        kt_bag bag = { named, count };
        enc_bag(b, &bag);
        free(named);
        return;
    }
    /* The string form. Split on commas, trimming, the way the other two do.  */
    kt_value named[64];
    char *copy = strdup(n->fields->sval);
    int count = 0;
    char *p = copy;
    while (*p && count < 64) {
        while (*p == ' ' || *p == '\t') p++;
        char *start = p;
        while (*p && *p != ',') p++;
        char *stop = p;
        if (*p == ',') p++;
        while (stop > start && (stop[-1] == ' ' || stop[-1] == '\t')) stop--;
        if (stop == start) continue;
        *stop = '\0';
        memset(&named[count], 0, sizeof named[count]);
        named[count].name = start;
        named[count].kind = KT_V_NONE;
        count++;
    }
    kt_bag bag = { named, count };
    enc_bag(b, &bag);
    free(copy);
    (void)st;
}

/* notice parses one case and writes the structure back out. Returns 0 when the
 * text was refused, which the corpus may be asking for. */
static int notice(const char *text, char *out, size_t cap) {
    char *src = strdup(text);
    kt_stmt *st = parse_statement(src);
    free(src);
    if (!st) return 0;
    if (!st->verb || strcmp(st->verb, "stale") != 0) { stmt_free(st); return 0; }

    kt_stale n;
    if (!stale_of(st, &n)) { stmt_free(st); return 0; }

    kt_buf b;
    memset(&b, 0, sizeof b);
    rendered(&b, &n, st);
    char *s = buf_dup(&b);
    snprintf(out, cap, "%s", s);
    free(s);
    free(b.p);
    stmt_free(st);
    return 1;
}

static void check(int line, const char *text, const char *want, int bad) {
    char got[4096];
    got[0] = '\0';
    int ok = notice(text, got, sizeof got);
    cases++;
    if (bad) {
        if (ok) {
            printf("line %d: %s was taken as %s, and should have been refused\n",
                   line, text, got);
            failures++;
        }
        return;
    }
    if (!ok) {
        printf("line %d: %s was refused\n", line, text);
        failures++;
        return;
    }
    if (strcmp(got, want) != 0) {
        printf("line %d: %s\n  came out as %s\n  want        %s\n", line, text, got, want);
        failures++;
        return;
    }
    /* What comes out goes back in unchanged. */
    char again_in[4096], again[4096];
    snprintf(again_in, sizeof again_in, "stale %s", want);
    again[0] = '\0';
    if (!notice(again_in, again, sizeof again) || strcmp(again, want) != 0) {
        printf("line %d: the canonical form %s renders as %s the second time\n",
               line, want, again);
        failures++;
    }
}

static char *trim(char *s) {
    while (*s == ' ' || *s == '\t') s++;
    size_t n = strlen(s);
    while (n && (s[n - 1] == '\n' || s[n - 1] == '\r' || s[n - 1] == ' '
                 || s[n - 1] == '\t')) s[--n] = '\0';
    return s;
}

int main(int argc, char **argv) {
    if (argc < 2) { fprintf(stderr, "usage: %s <stale.wire>\n", argv[0]); return 2; }
    FILE *f = fopen(argv[1], "r");
    if (!f) { perror(argv[1]); return 2; }

    char raw[8192];
    char pending[8192];
    int pending_line = 0;
    int have = 0;
    int lineno = 0;
    while (fgets(raw, sizeof raw, f)) {
        lineno++;
        char *text = trim(raw);
        if (!*text || *text == '#') continue;
        if (strncmp(text, "stale ", 6) == 0 || strcmp(text, "stale") == 0) {
            if (have) {
                printf("line %d: a case with no answer\n", pending_line);
                failures++;
            }
            snprintf(pending, sizeof pending, "%s", text);
            pending_line = lineno;
            have = 1;
            continue;
        }
        if (strncmp(text, "want ", 5) == 0) {
            if (!have) { printf("line %d: a notice with no case\n", lineno); failures++; continue; }
            check(pending_line, pending, text + 5, 0);
            have = 0;
            continue;
        }
        if (strcmp(text, "bad") == 0) {
            if (!have) { printf("line %d: a notice with no case\n", lineno); failures++; continue; }
            check(pending_line, pending, NULL, 1);
            have = 0;
            continue;
        }
        printf("line %d: %s is not a corpus line\n", lineno, text);
        failures++;
    }
    if (have) { printf("line %d: a case with no answer\n", pending_line); failures++; }
    fclose(f);

    printf("cases %d\n", cases);
    printf("DONE\n");
    return failures ? 1 : 0;
}
