/* ptydriver - client-side PTY bridge for terminal surfaces. See ptydriver.h. */

/* Expose the POSIX/glibc surface (forkpty, setenv, kill, TIOCSWINSZ) under
 * -std=c11, which otherwise hides them behind feature-test macros. */
#define _DEFAULT_SOURCE
#ifndef _XOPEN_SOURCE
#define _XOPEN_SOURCE 700
#endif

#include "ptydriver.h"

#include <stdlib.h>
#include <string.h>

#ifdef _WIN32

/* No forkpty on Windows (ConPTY would be a separate port). */
kt_pty *kt_pty_attach(kt_conn *c, uint64_t term_id, const char *shell) {
    (void)c; (void)term_id; (void)shell;
    return NULL;
}
void kt_pty_close(kt_pty *p) { (void)p; }

#else

#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

#if defined(__linux__)
#include <pty.h> /* forkpty */
#else
#include <util.h> /* forkpty on macOS/BSD */
#endif

struct kt_pty {
    kt_conn *conn;
    uint64_t term;
    int master;
    pid_t pid;
    pthread_t reader;
    int started;
};

/* quote_n renders n raw bytes as a protocol string literal, matching
 * kt_quote but length-aware so terminal output containing NUL (or any
 * byte) round-trips. Control bytes ride the \xNN escape. */
static char *quote_n(const unsigned char *s, size_t n) {
    /* Worst case every byte becomes \xNN (4 chars) + the two quotes + NUL. */
    char *out = malloc(n * 4 + 3);
    if (!out) return NULL;
    char *p = out;
    *p++ = '"';
    for (size_t i = 0; i < n; i++) {
        unsigned char c = s[i];
        switch (c) {
        case '"':  *p++ = '\\'; *p++ = '"'; break;
        case '\\': *p++ = '\\'; *p++ = '\\'; break;
        case '\n': *p++ = '\\'; *p++ = 'n'; break;
        case '\t': *p++ = '\\'; *p++ = 't'; break;
        case '\r': *p++ = '\\'; *p++ = 'r'; break;
        case 0x1b: *p++ = '\\'; *p++ = 'e'; break;
        default:
            if (c < 0x20 || c == 0x7f) {
                p += sprintf(p, "\\x%02x", c);
            } else {
                *p++ = (char)c;
            }
        }
    }
    *p++ = '"';
    *p = '\0';
    return out;
}

/* reader_thread forwards child output to the terminal's feed= until the
 * PTY closes (child exit or kt_pty_close). */
static void *reader_thread(void *arg) {
    kt_pty *p = arg;
    unsigned char buf[4096];
    for (;;) {
        ssize_t nr = read(p->master, buf, sizeof buf);
        if (nr <= 0) {
            if (nr < 0 && errno == EINTR) continue;
            break;
        }
        char *q = quote_n(buf, (size_t)nr);
        if (!q) continue;
        char *args = malloc(strlen(q) + 8);
        if (args) {
            sprintf(args, "feed=%s", q);
            kt_set(p->conn, p->term, args);
            free(args);
        }
        free(q);
    }
    return NULL;
}

/* on_input writes the user's keystrokes / mouse reports / paste to the
 * child. The length-aware accessor preserves an interior NUL (Ctrl-@ sends
 * 0x00), which a plain C string would truncate. */
static void on_input(const kt_event *ev, void *ud) {
    kt_pty *p = ud;
    size_t len = 0;
    const char *data = kt_event_text_n(ev, "data", &len);
    if (!data) return;
    for (size_t off = 0; off < len;) {
        ssize_t w = write(p->master, data + off, len - off);
        if (w <= 0) {
            if (w < 0 && errno == EINTR) continue;
            break;
        }
        off += (size_t)w;
    }
}

/* on_resize matches the child's PTY winsize to the terminal grid. */
static void on_resize(const kt_event *ev, void *ud) {
    kt_pty *p = ud;
    long long cols = 0, rows = 0;
    if (!kt_event_int(ev, "cols", &cols) || !kt_event_int(ev, "rows", &rows))
        return;
    if (cols <= 0 || rows <= 0) return;
    struct winsize ws;
    memset(&ws, 0, sizeof ws);
    ws.ws_col = (unsigned short)cols;
    ws.ws_row = (unsigned short)rows;
    ioctl(p->master, TIOCSWINSZ, &ws);
}

kt_pty *kt_pty_attach(kt_conn *c, uint64_t term_id, const char *shell) {
    if (!c || !term_id) return NULL;
    if (!shell || !*shell) {
        shell = getenv("SHELL");
        if (!shell || !*shell) shell = "/bin/sh";
    }

    /* Start at a sane size; the terminal's first resize event corrects it. */
    struct winsize ws;
    memset(&ws, 0, sizeof ws);
    ws.ws_col = 80;
    ws.ws_row = 24;

    int master = -1;
    pid_t pid = forkpty(&master, NULL, NULL, &ws);
    if (pid < 0) return NULL;
    if (pid == 0) {
        /* Child: advertise a modern terminal and exec the shell. argv[0] is
         * the shell path (a plain interactive shell, not a login shell),
         * matching the previous server-side spawn. */
        setenv("TERM", "xterm-256color", 1);
        setenv("COLORTERM", "truecolor", 1);
        execl(shell, shell, (char *)NULL);
        _exit(127);
    }

    kt_pty *p = calloc(1, sizeof *p);
    if (!p) {
        close(master);
        kill(pid, SIGKILL);
        waitpid(pid, NULL, 0);
        return NULL;
    }
    p->conn = c;
    p->term = term_id;
    p->master = master;
    p->pid = pid;

    /* Subscribe before the child produces much: input drives it, resize
     * sizes it. */
    kt_on(c, term_id, "input", on_input, p);
    kt_on(c, term_id, "resize", on_resize, p);

    if (pthread_create(&p->reader, NULL, reader_thread, p) == 0)
        p->started = 1;
    return p;
}

void kt_pty_close(kt_pty *p) {
    if (!p) return;
    if (p->pid > 0) kill(p->pid, SIGHUP);
    if (p->master >= 0) close(p->master); /* unblocks the reader's read() */
    if (p->started) pthread_join(p->reader, NULL);
    if (p->pid > 0) waitpid(p->pid, NULL, 0);
    free(p);
}

#endif /* _WIN32 */
