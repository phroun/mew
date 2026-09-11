/* compare_conformance.c - the C client answering the shared comparison corpus.
 *
 * ../testdata/compare.wire holds one case per statement: `a` and `b` are the
 * two values, `want` is -1, 0 or 1, and `collate` names the collation. Every
 * implementation of the comparison core answers the same file, so a
 * disagreement between C, Go and Python fails a build rather than showing up
 * later as a fold that came out in the wrong order.
 *
 * The corpus is read with the client's OWN parser, which is the point: the
 * values under test are the values the wire produces.
 *
 * stdout: one line per failure, then `cases N` and `DONE`.
 *
 * It includes the client rather than linking it, so it reaches the parser and
 * the comparison the way the client's own code does.
 */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.c"

static const kt_arg *find(const kt_stmt *s, const char *name) {
    for (int i = 0; i < s->n; i++)
        if (s->args[i].name && !strcmp(s->args[i].name, name)) return &s->args[i];
    return NULL;
}

/* show renders a value for a failure line. */
static void show(const kt_arg *v, char *out, size_t cap) {
    if (!v || !v->has_value) { snprintf(out, cap, "<missing>"); return; }
    switch (v->kind) {
    case 0: snprintf(out, cap, "%lld", v->ival); break;
    case 1: snprintf(out, cap, "%g", v->fval); break;
    case 2: snprintf(out, cap, "\"%s\"", v->sval); break;
    default: snprintf(out, cap, "%s", v->sval); break;
    }
}

int main(int argc, char **argv) {
    const char *path = argc > 1 ? argv[1] : "../testdata/compare.wire";
    FILE *f = fopen(path, "rb");
    if (!f) { printf("cannot read %s\n", path); return 1; }

    kt_buf text = {0};
    int ch;
    while ((ch = fgetc(f)) != EOF) buf_put(&text, (char)ch);
    fclose(f);
    char *body = buf_dup(&text);
    free(text.p);

    int cases = 0, failures = 0;
    char *line = strtok(body, "\n");
    for (; line; line = strtok(NULL, "\n")) {
        while (*line == ' ' || *line == '\t') line++;
        if (!*line || *line == '#') continue;

        kt_stmt *s = parse_statement(line);
        if (!s || !s->verb || strcmp(s->verb, "case")) {
            printf("not a case: %s\n", line);
            failures++;
            stmt_free(s);
            continue;
        }
        cases++;

        const kt_arg *a = find(s, "a"), *b = find(s, "b");
        const kt_arg *want = find(s, "want"), *collate = find(s, "collate");
        if (!want || want->kind != 0) {
            printf("case %d has no want\n", cases);
            failures++;
            stmt_free(s);
            continue;
        }
        int expect = (int)want->ival;
        const char *collation = collate ? collate->sval : KT_COLLATE_EXACT;

        char sa[256], sb[256];
        show(a, sa, sizeof sa);
        show(b, sb, sizeof sb);

        int got = kt_compare_value(a, b, collation);
        if (got != expect) {
            printf("case %d: compare(%s, %s, %s) = %d, want %d\n",
                   cases, sa, sb, collation, got, expect);
            failures++;
        }
        /* Every case is its own mirror: swapping the two swaps the answer. */
        int back = kt_compare_value(b, a, collation);
        if (back != -expect) {
            printf("case %d reversed: compare(%s, %s, %s) = %d, want %d\n",
                   cases, sb, sa, collation, back, -expect);
            failures++;
        }
        stmt_free(s);
    }
    free(body);

    printf("cases %d\n", cases);
    printf("DONE\n");
    return failures ? 1 : 0;
}
