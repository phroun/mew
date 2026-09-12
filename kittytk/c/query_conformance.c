/* query_conformance.c - the C client answering the shared query corpus.
 *
 * ../testdata/query.wire holds the text a display sends and the structure it
 * has to come out as, two lines per case. Every client library takes that text
 * apart, and they have to arrive at the same structure or the same query means
 * two things. A disagreement between C, Go and Python fails a build here.
 *
 * The corpus is read with the client's OWN parser, which is the point: what is
 * under test is what the wire produces.
 *
 * stdout: one line per failure, then `cases N` and `DONE`.
 *
 * It includes the client rather than linking it, so it reaches the parser and
 * the structured reading the way the client's own inbound path does.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

static int failures = 0;
static int cases = 0;

/* answer parses one case and writes the structure back out. Returns 0 when the
 * text was refused, which the corpus may be asking for. */
static int answer(const char *kind, const char *text, char *out, size_t cap) {
    kt_buf line;
    memset(&line, 0, sizeof line);
    buf_puts(&line, kind);
    buf_put(&line, ' ');
    buf_puts(&line, text);
    char *src = buf_dup(&line);
    free(line.p);

    kt_stmt *st = parse_statement(src);
    free(src);
    if (!st) return 0;

    char err[KT_QERR];
    err[0] = '\0';
    kt_buf b;
    memset(&b, 0, sizeof b);
    int ok;
    if (!strcmp(kind, "spec")) {
        kt_qspec spec;
        ok = parse_qspec(st->args, st->n, &spec, err);
        if (ok) { enc_qspec(&b, &spec); qspec_release(&spec); }
    } else {
        kt_qfill fill;
        ok = parse_qfill(st->args, st->n, &fill, err);
        if (ok) { enc_qfill(&b, &fill); qfill_release(&fill); }
    }
    stmt_free(st);
    if (ok) {
        char *rendered = buf_dup(&b);
        snprintf(out, cap, "%s", rendered);
        free(rendered);
    }
    free(b.p);
    return ok;
}

static void check(int line, const char *kind, const char *text,
                  const char *want, int bad) {
    char got[4096];
    got[0] = '\0';
    int ok = answer(kind, text, got, sizeof got);
    cases++;
    if (bad) {
        if (ok) {
            printf("line %d: %s %s was taken as %s, and should have been refused\n",
                   line, kind, text, got);
            failures++;
        }
        return;
    }
    if (!ok) {
        printf("line %d: %s %s was refused\n", line, kind, text);
        failures++;
        return;
    }
    if (strcmp(got, want) != 0) {
        printf("line %d: %s %s\n  came out as %s\n  want        %s\n",
               line, kind, text, got, want);
        failures++;
        return;
    }
    /* What comes out goes back in unchanged: the canonical spelling is one the
     * parser reads to the same structure it was rendered from. */
    char again[4096];
    again[0] = '\0';
    if (!answer(kind, want, again, sizeof again) || strcmp(again, want) != 0) {
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
    if (argc < 2) { fprintf(stderr, "usage: %s <query.wire>\n", argv[0]); return 2; }
    FILE *f = fopen(argv[1], "r");
    if (!f) { perror(argv[1]); return 2; }

    char raw[8192];
    char kind[16] = "";
    char text[4096] = "";
    int open_line = 0;
    int lineno = 0;
    while (fgets(raw, sizeof raw, f)) {
        lineno++;
        char *line = trim(raw);
        if (!*line || *line == '#') continue;
        char *rest = strchr(line, ' ');
        if (rest) *rest++ = '\0';
        else rest = line + strlen(line);
        if (!strcmp(line, "spec") || !strcmp(line, "fill")) {
            if (open_line) { printf("line %d: a case with no answer\n", open_line); failures++; }
            snprintf(kind, sizeof kind, "%.15s", line);
            snprintf(text, sizeof text, "%s", rest);
            open_line = lineno;
        } else if (!strcmp(line, "want")) {
            if (!open_line) { printf("line %d: an answer with no case\n", lineno); failures++; continue; }
            check(open_line, kind, text, rest, 0);
            open_line = 0;
        } else if (!strcmp(line, "bad")) {
            if (!open_line) { printf("line %d: an answer with no case\n", lineno); failures++; continue; }
            check(open_line, kind, text, "", 1);
            open_line = 0;
        } else {
            printf("line %d: \"%s\" is not a corpus line\n", lineno, line);
            failures++;
        }
    }
    fclose(f);
    if (open_line) { printf("line %d: a case with no answer\n", open_line); failures++; }

    printf("cases %d\n", cases);
    printf("DONE\n");
    return failures ? 1 : 0;
}
