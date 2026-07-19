/* ptydriver - run a child process on the CLIENT side and bridge it to a
 * terminal surface over the display protocol.
 *
 * Under KittyTK's network-rendering model the render server draws the
 * terminal but never spawns the child: the process belongs to the app. A
 * kt_pty owns a real PTY, spawns a shell into it, streams the child's
 * output to the terminal's feed= property, and writes the terminal's
 * input/resize events back to the PTY. It is the C twin of the Go ptydriver
 * package.
 *
 * POSIX only (forkpty). On Windows kt_pty_attach returns NULL. */
#ifndef KT_PTYDRIVER_H
#define KT_PTYDRIVER_H

#include "kittytk.h"

typedef struct kt_pty kt_pty;

/* Spawn `shell` (NULL = $SHELL, else /bin/sh) in a fresh PTY and bridge it
 * to terminal object `term_id` on connection `c`: the child's output is fed
 * in via feed=, and the terminal's `input` and `resize` events are written
 * back to the PTY. Returns NULL on failure (or on Windows). */
kt_pty *kt_pty_attach(kt_conn *c, uint64_t term_id, const char *shell);

/* Terminate the child and release the PTY. */
void kt_pty_close(kt_pty *p);

#endif /* KT_PTYDRIVER_H */
