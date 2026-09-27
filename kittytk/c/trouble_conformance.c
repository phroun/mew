/* trouble_conformance.c - the C client answering the shared trouble corpus.
 *
 * ../testdata/trouble.wire holds what the display says a complaint with and the
 * structure it has to come out as, two lines per case. Every client library
 * takes that text apart, and they have to arrive at the same structure or the
 * same complaint means three things.
 *
 * A refusal is what a batch is answered WITH, in place of the reply that would
 * have named what it made. This is not that: the statements ran and something on
 * the way is worth the author knowing. What the corpus agrees is what it says --
 * the reason, and what it was about -- and that an argument this version does
 * not know is a later one saying more rather than a statement to refuse.
 *
 * stdout: one line per failure, then `cases N` and `DONE`.
 *
 * It includes the client rather than linking it, so it reaches the same parser
 * the client's own inbound path uses.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

static int failures = 0;
static int cases = 0;

/* trouble parses one case and writes the structure back out, in the canonical
 * order: `about` where there is one, then `text`. Returns 0 when the text was
 * refused, which the corpus may be asking for. */
static int trouble(const char *text, char *out, size_t cap) {
    char *src = strdup(text);
    kt_stmt *st = parse_statement(src);
    free(src);
    if (!st) return 0;
    if (!st->verb || strcmp(st->verb, "trouble") != 0) { stmt_free(st); return 0; }

    const char *about = NULL, *reason = NULL;
    for (int i = 0; i < st->n; i++) {
        const char *nm = st->args[i].name ? st->args[i].name : "";
        /* Only the two it reads are checked: an argument this version does not
         * know is a later version saying more. */
        if (strcmp(nm, "about") != 0 && strcmp(nm, "text") != 0) continue;
        if (!st->args[i].has_value || st->args[i].kind != 2) { stmt_free(st); return 0; }
        if (strcmp(nm, "about") == 0) about = st->args[i].sval;
        else reason = st->args[i].sval;
    }
    /* No text= is a complaint with nothing in it, which says less than silence. */
    if (!reason) { stmt_free(st); return 0; }

    kt_buf b;
    memset(&b, 0, sizeof b);
    if (about && *about) {
        char *q = kt_quote(about);
        buf_puts(&b, "about=");
        buf_puts(&b, q);
        buf_put(&b, ' ');
        free(q);
    }
    char *q = kt_quote(reason);
    buf_puts(&b, "text=");
    buf_puts(&b, q);
    free(q);
    buf_put(&b, '\0');
    snprintf(out, cap, "%s", b.p ? b.p : "");
    free(b.p);
    stmt_free(st);
    return 1;
}

static void check(int line, const char *text, const char *want, int bad) {
    char got[4096];
    got[0] = '\0';
    int ok = trouble(text, got, sizeof got);
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
    snprintf(line2, sizeof line2, "trouble %s", want);
    again[0] = '\0';
    if (!trouble(line2, again, sizeof again) || strcmp(again, want) != 0) {
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
    if (argc < 2) { fprintf(stderr, "usage: %s <trouble.wire>\n", argv[0]); return 2; }
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
        if (strncmp(text, "trouble ", 8) == 0 || strcmp(text, "trouble") == 0) {
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
