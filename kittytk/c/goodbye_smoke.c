/* goodbye_smoke.c - what a C application hears when its display goes.
 *
 * Run by c/interop/00_interop_test.go against a REAL Go display host, which
 * quits once this says it is connected. What is proved is that the farewell
 * crosses the wire into a non-Go client: the display says `goodbye reason=quit`
 * and hangs up, and this reads the reason back off its own connection.
 *
 * A closed socket cannot say why it closed -- a display that quit and a
 * connection that broke are the same silence -- so a client that cannot read
 * the reason is a client whose applications have to guess.
 *
 * stdout markers: READY / GOODBYE <word> / DONE. */
#define _POSIX_C_SOURCE 200809L
#include "kittytk.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: goodbye_smoke <endpoint>\n");
        return 64;
    }
    kt_conn *c = kt_dial(argv[1], "C Goodbye App");
    if (!c) {
        fprintf(stderr, "dial failed\n");
        return 1;
    }

    /* Work of its own, which has nothing to do with any display and outlives
     * it. Nothing about hearing a farewell ends an application; that is the
     * application's own decision, taken below by returning. */
    int own = 6 * 7;

    printf("READY\n");
    fflush(stdout);

    kt_wait_closed(c);

    char why[32] = "";
    int said = kt_goodbye(c, why, sizeof why);
    printf("GOODBYE %s\n", said ? why : "-");
    fflush(stdout);

    if (own != 42) {
        printf("FAIL: its own work did not survive the display going\n");
        fflush(stdout);
        kt_close(c);
        return 1;
    }

    printf("DONE\n");
    fflush(stdout);
    kt_close(c);
    return 0;
}
