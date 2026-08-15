/* scripts.h - the demo's display-protocol build scripts (C).
 * Each returns a malloc'd string the caller frees. The protocol text is
 * identical to the Go and Python demos; only the string-building differs. */
#ifndef KITTYTK_SCRIPTS_H
#define KITTYTK_SCRIPTS_H

char *main_build_script(void);
char *main_menu_script(void);
char *main_status_script(void);
char *protocol_window_script(void);
char *demo_terminal_script(int n);
char *about_dialog_script(void);
char *secondary_build_script(int n);
char *mdi_child_script(int n);

#endif
