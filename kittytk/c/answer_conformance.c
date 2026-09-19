/* answer_conformance.c - the C client answering the shared answer corpus.
 *
 * ../testdata/answer.wire holds the text an `ask` is answered with and the
 * structure it has to come out as, two lines per case. Every client library
 * takes that text apart, and they have to arrive at the same structure or the
 * same answer means three things.
 *
 * What the corpus agrees is the ENVELOPE -- which question is being answered,
 * whether this is the last piece, whether it is a refusal. The payload is the
 * question's own and nothing here reads it, so what is checked about it is that
 * it survives untouched and in order.
 *
 * stdout: one line per failure, then `cases N` and `DONE`.
 *
 * It includes the client rather than linking it, so it reaches the same parser
 * and the same taking-apart the client's own inbound path uses.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

static int failures = 0;
static int cases = 0;

/* enc_arg writes one argument back: its name, and its value where it has one.
 * A block is written through the record's own members, which is the only block
 * an answer carries. */
static void enc_arg(kt_buf *b, const kt_arg *a) {
    enc_field_name(b, a->name);
    if (!a->has_value) return;
    buf_put(b, '=');
    if (a->kind == 4) {
        int n = 0;
        kt_value *m = members_of(a, &n);
        kt_bag bag = { m, n };
        enc_bag(b, &bag);
        free(m);
        return;
    }
    kt_value v;
    memset(&v, 0, sizeof v);
    v.name = a->name;
    switch (a->kind) {
    case 0: v.kind = KT_V_INT;    v.ival = a->ival; break;
    case 1: v.kind = KT_V_FLOAT;  v.fval = a->fval; break;
    case 2: v.kind = KT_V_STRING; v.sval = a->sval; v.slen = a->slen; break;
    default: v.kind = KT_V_WORD;  v.sval = a->sval; v.slen = a->slen; break;
    }
    enc_value(b, &v);
}

/* answer parses one case and writes the structure back out. Returns 0 when the
 * text was refused, which the corpus may be asking for. */
static int answer(const char *text, char *out, size_t cap) {
    char *src = strdup(text);
    kt_stmt *st = parse_statement(src);
    free(src);
    if (!st) return 0;
    if (!st->verb || strcmp(st->verb, "answer") != 0) { stmt_free(st); return 0; }

    kt_answer ans;
    kt_arg *carries = malloc(sizeof(kt_arg) * (st->n ? st->n : 1));
    if (!answer_of(st, &ans, carries)) {
        free(carries);
        stmt_free(st);
        return 0;
    }

    /* The envelope first, then the payload, then the end, then why -- which is
     * the canonical order whichever order arrived. */
    kt_buf b;
    memset(&b, 0, sizeof b);
    int wrote = 0;
    if (ans.to) {
        buf_puts(&b, "to=");
        buf_puts(&b, ans.to);
        wrote = 1;
    }
    for (int i = 0; i < ans.ncarries; i++) {
        if (wrote) buf_put(&b, ' ');
        enc_arg(&b, &ans.carries[i]);
        wrote = 1;
    }
    if (ans.complete) {
        if (wrote) buf_put(&b, ' ');
        buf_puts(&b, "complete");
        wrote = 1;
    }
    if (ans.error) {
        if (wrote) buf_put(&b, ' ');
        buf_puts(&b, "error=");
        char *q = kt_quote(ans.error);
        buf_puts(&b, q);
        free(q);
    }
    char *rendered = buf_dup(&b);
    snprintf(out, cap, "%s", rendered);
    free(rendered);
    free(b.p);
    free(carries);
    stmt_free(st);
    return 1;
}

static void check(int line, const char *text, const char *want, int bad) {
    char got[4096];
    got[0] = '\0';
    int ok = answer(text, got, sizeof got);
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
    char line2[4096], again[4096];
    snprintf(line2, sizeof line2, "answer %s", want);
    again[0] = '\0';
    if (!answer(line2, again, sizeof again) || strcmp(again, want) != 0) {
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
    if (argc < 2) { fprintf(stderr, "usage: %s <answer.wire>\n", argv[0]); return 2; }
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
        if (strncmp(text, "answer ", 7) == 0 || strcmp(text, "answer") == 0) {
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
            if (!have) { printf("line %d: an answer with no case\n", lineno); failures++; continue; }
            check(pending_line, pending, text + 5, 0);
            have = 0;
            continue;
        }
        if (strcmp(text, "bad") == 0) {
            if (!have) { printf("line %d: an answer with no case\n", lineno); failures++; continue; }
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
