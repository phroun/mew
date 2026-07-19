/* scripts.c - demo build scripts in C.
 *
 * Translation note: protocol text like `caption="hi"` becomes C
 * `caption=\"hi\"`; a protocol-level backslash escape (`\n` inside a
 * caption, `\\_` in the tab art, `\e` in the terminal feed) doubles in C
 * (`\\n`, `\\\\_`, `\\e`); statement-separating newlines are real `\n`. */
#define _POSIX_C_SOURCE 200809L
#include "scripts.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct { char *p; size_t len, cap; } sbuf;
static void sb_add(sbuf *b, const char *s) {
    size_t n = strlen(s);
    if (b->len + n + 1 > b->cap) {
        while (b->len + n + 1 > b->cap) b->cap = b->cap ? b->cap * 2 : 4096;
        b->p = realloc(b->p, b->cap);
    }
    memcpy(b->p + b->len, s, n);
    b->len += n;
    b->p[b->len] = '\0';
}
static void sb_addf(sbuf *b, const char *fmt, ...) {
    /* Size the output exactly, then format. A fixed stack buffer would
     * silently truncate any fragment longer than it - e.g. the whole
     * secondary-window build - leaving unbalanced braces that hang the
     * host's batch scanner (it never reaches the `end` terminator). */
    va_list ap, ap2;
    va_start(ap, fmt);
    va_copy(ap2, ap);
    int n = vsnprintf(NULL, 0, fmt, ap);
    va_end(ap);
    if (n < 0) {
        va_end(ap2);
        return;
    }
    char *tmp = malloc((size_t)n + 1);
    if (tmp) {
        vsnprintf(tmp, (size_t)n + 1, fmt, ap2);
        sb_add(b, tmp);
        free(tmp);
    }
    va_end(ap2);
}

/* The demo tree, shared by the Lists and Scroll Lists tabs. */
static const char *TREE_ITEMS =
    "new item caption=\"Documents\" expanded children={\n"
    "  new item caption=\"Work\" expanded children={\n"
    "    new item caption=\"Report.txt\"\n"
    "    new item caption=\"Presentation.pptx\"\n"
    "    new item caption=\"Budget.xlsx\"\n"
    "    new item caption=\"Meeting Notes.md\"\n"
    "  }\n"
    "  new item caption=\"Personal\" children={\n"
    "    new item caption=\"Notes.txt\"\n"
    "    new item caption=\"Journal.md\"\n"
    "    new item caption=\"Ideas.txt\"\n"
    "  }\n"
    "  new item caption=\"Projects\" children={\n"
    "    new item caption=\"Alpha\"\n"
    "    new item caption=\"Beta\"\n"
    "    new item caption=\"Gamma\"\n"
    "  }\n"
    "}\n"
    "new item caption=\"Pictures\" children={\n"
    "  new item caption=\"Vacation\"\n"
    "  new item caption=\"Family\"\n"
    "  new item caption=\"Pets\"\n"
    "}\n"
    "new item caption=\"Code\" children={\n"
    "  new item caption=\"Go\" children={ new item caption=\"main.go\"; new item caption=\"utils.go\" }\n"
    "  new item caption=\"Python\" children={ new item caption=\"script.py\" }\n"
    "}\n";

char *main_build_script(void) {
    sbuf b = {0};
    sb_add(&b,
        "w=new window title=\"KittyTK Demo (C)\" width=480 height=288 tearable main children={\n"
        "t=new tabs children={\n"
        "\n"
        "b=new tab caption=\"Basic Trinkets\" children={\n"
        "  bw=new panel layout=vbox spacing=0 children={\n"
        "    new label caption=\"This is a demo of basic trinkets:\"\n"
        "    brow=new panel layout=hbox spacing=8 children={\n"
        "      input=new textinput placeholder=\"Enter text here...\" stretch=1\n"
        "      new button caption=\"Browse...\"\n"
        "    }\n"
        "    new spacer\n"
        "    new panel layout=hbox spacing=8 children={\n"
        "      new button caption=\"OK\" action=demo.basic.ok\n"
        "      new button caption=\"Cancel\" action=demo.basic.cancel\n"
        "      new button caption=\"Apply\" action=demo.basic.apply\n"
        "    }\n"
        "    new button caption=\"Disabled\" !enabled\n"
        "  }\n"
        "}\n"
        "\n"
        "s=new tab caption=\"Selection\" children={\n"
        "  o=new panel layout=vbox spacing=0 children={\n"
        "    new panel layout=hbox spacing=8 align=fill children={\n"
        "      new panel border layout=vbox fixed_width=256 children={\n"
        "        new label caption=\"The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence\" wrap align=fill\n"
        "      }\n"
        "      new panel border layout=vbox fixed_width=256 children={\n"
        "        new label caption=\"Pack my box with five dozen liquor jugs before the Tuesday checkbox below doubles every letter\" wrap align=fill\n"
        "      }\n"
        "      new panel border layout=vbox fixed_width=288 children={\n"
        "        new panel layout=vbox align=fill children={\n"
        "          new checkbox caption=\"Enable the experimental feature that reticulates splines while the moon is full\" wrap\n"
        "          new radiobutton caption=\"Prefer the long-form explanation whenever the assistant answers a question\" wrap\n"
        "        }\n"
        "      }\n"
        "    }\n"
        "    sp=new splitter orientation=vertical position=0.4 stretch=1 align=fill children={\n"
        "      c=new panel layout=vbox spacing=0 children={\n"
        "        new label caption=\"Checkboxes:\"\n"
        "        new checkbox caption=\"Enable feature A\" checked\n"
        "        new checkbox caption=\"Enable feature B\"\n"
        "        new checkbox caption=\"Tri-state checkbox\" tristate\n"
        "        new label caption=\"Font Options:\"\n"
        "        wfont=new checkbox caption=\"Window: Tuesday (double-width)\"\n"
        "        dfont=new checkbox caption=\"Desktop: Tuesday (double-width)\"\n"
        "        grid=new checkbox caption=\"Window: 32-unit rows (denomination test)\"\n"
        "      }\n"
        "      r=new panel layout=vbox spacing=0 children={\n"
        "        new label caption=\"Radio buttons:\"\n"
        "        new radiobutton caption=\"Option 1\" group=selopts\n"
        "        new radiobutton caption=\"Option 2\" group=selopts\n"
        "        new label caption=\"Tab Background Color:\"\n"
        "        bgdef=new radiobutton caption=\"Default\" group=selbg checked\n"
        "        bggreen=new radiobutton caption=\"Dark Green\" group=selbg\n"
        "        bggray=new radiobutton caption=\"TrueColor #333\" group=selbg\n"
        "        new label caption=\"Alphabet ComboBox:\"\n"
        "        new combobox children={");
    for (int i = 0; i < 26; i++)
        sb_addf(&b, "\n          new item caption=\"%c - Letter %c\"", 'A' + i, 'A' + i);
    sb_add(&b,
        "\n        }\n"
        "      }\n"
        "    }\n"
        "  }\n"
        "}\n"
        "\n"
        "new tab caption=\"Lists\" children={\n"
        "  new splitter orientation=horizontal position=0.5 children={\n"
        "    new panel layout=vbox children={\n"
        "      new label caption=\"ListView:\"\n"
        "      new listview children={");
    for (int i = 1; i <= 20; i++)
        sb_addf(&b, "\n        new item caption=\"Item %d\"", i);
    sb_add(&b,
        "\n      }\n"
        "    }\n"
        "    new panel layout=vbox children={\n"
        "      new label caption=\"TreeView:\"\n"
        "      new treeview children={\n");
    sb_add(&b, TREE_ITEMS);
    sb_add(&b,
        "}\n"
        "    }\n"
        "  }\n"
        "}\n"
        "\n"
        "ss=new tab caption=\"Scroll Selection\" children={\n"
        "  sp=new splitter orientation=vertical position=0.4 children={\n"
        "    new scrollarea children={\n"
        "      new panel layout=vbox spacing=0 children={\n"
        "        new label caption=\"Checkboxes (scrollable):\"");
    for (int i = 1; i <= 15; i++)
        sb_addf(&b, "\n        new checkbox caption=\"Feature option %d\"%s", i, (i % 3 == 0) ? " checked" : "");
    sb_add(&b,
        "\n      }\n"
        "    }\n"
        "    sa=new scrollarea children={\n"
        "      sr=new panel layout=vbox spacing=0 children={\n"
        "        new label caption=\"Radio buttons (scrollable):\"");
    for (int i = 1; i <= 10; i++)
        sb_addf(&b, "\n        new radiobutton caption=\"Radio option %d with longer text\" group=scrollopts", i);
    sb_add(&b,
        "\n        new label caption=\"Tab Background Color:\"\n"
        "        sbgdef=new radiobutton caption=\"Default\" group=scrollbg checked\n"
        "        sbggreen=new radiobutton caption=\"Dark Green\" group=scrollbg\n"
        "        sbggray=new radiobutton caption=\"TrueColor #333\" group=scrollbg\n"
        "      }\n"
        "    }\n"
        "  }\n"
        "}\n"
        "\n"
        "new tab caption=\"Progress\" children={\n"
        "  new panel layout=vbox spacing=16 children={\n"
        "    new label caption=\"Horizontal Progress Bars:\"\n"
        "    new progress value=25\n"
        "    new progress value=50\n"
        "    new progress value=75\n"
        "    new progress value=100\n"
        "    new label caption=\"Indeterminate Progress:\"\n"
        "    new progress indeterminate\n"
        "  }\n"
        "}\n"
        "\n"
        "new tab caption=\"Bottom Tabs\" children={\n"
        "  new tabs position=bottom children={\n"
        "    new tab caption=\"First\" children={\n"
        "      new panel layout=vbox children={\n"
        "        new label caption=\"This TabTrinket has tabs at the bottom.\"\n"
        "        new label caption=\"  Top tabs use: _/ and \\\\_\"\n"
        "        new label caption=\"  Bottom tabs use: \\\\_ and _/\"\n"
        "      }\n"
        "    }\n"
        "    new tab caption=\"Second\" children={\n"
        "      new panel layout=vbox children={ new label caption=\"Second tab content\"; new button caption=\"Click me\" }\n"
        "    }\n"
        "  }\n"
        "}\n"
        "\n"
        "mtab=new tab caption=\"MDI Demo\" children={\n"
        "  mdisp=new splitter orientation=vertical position=0.9 caption=\"Dock\" children={\n"
        "    mdisa=new scrollarea children={\n"
        "      mdi=new mdipane fill=\"\xe2\x96\x91\" min_width=640 min_height=400 max_width=640 max_height=400 children={\n"
        "        mdicp=new panel layout=vbox spacing=8 children={\n"
        "          new label caption=\"MDIPane Trinket Demo\"\n"
        "          new label caption=\"Click [_] to minimize windows to the dock below.\"\n"
        "          new button caption=\"Spawn Window in MDIPane\" action=demo.mdi.spawn\n"
        "          new panel layout=hbox spacing=8 children={\n"
        "            new button caption=\"Tile\" action=demo.mdi.tile\n"
        "            new button caption=\"Cascade\" action=demo.mdi.cascade\n"
        "            new button caption=\"Next\" action=demo.mdi.next\n"
        "            new button caption=\"Prev\" action=demo.mdi.prev\n"
        "          }\n"
        "          mdistatus=new label caption=\"Active: none\"\n"
        "        }\n"
        "      }\n"
        "    }\n"
        "    mdidock=new dockrow entry_width=16\n"
        "  }\n"
        "}\n"
        "\n"
        "}\n"
        "}\n"
        "\n"
        "tabs=w.t\n"
        "binput=w.t.b.bw.brow.input\n"
        "wfont=w.t.s.o.sp.c.wfont\n"
        "dfont=w.t.s.o.sp.c.dfont\n"
        "grid=w.t.s.o.sp.c.grid\n"
        "bgdef=w.t.s.o.sp.r.bgdef\n"
        "bggreen=w.t.s.o.sp.r.bggreen\n"
        "bggray=w.t.s.o.sp.r.bggray\n"
        "sbgdef=w.t.ss.sp.sa.sr.sbgdef\n"
        "sbggreen=w.t.ss.sp.sa.sr.sbggreen\n"
        "sbggray=w.t.ss.sp.sa.sr.sbggray\n"
        "mdi=w.t.mtab.mdisp.mdisa.mdi\n"
        "mdistatus=w.t.mtab.mdisp.mdisa.mdi.mdicp.mdistatus\n"
        "mdidock=w.t.mtab.mdisp.mdidock\n");

    char *menu = main_menu_script();
    char *status = main_status_script();
    sb_add(&b, menu);
    sb_add(&b, status);
    free(menu);
    free(status);
    return b.p;
}

char *main_menu_script(void) {
    sbuf b = {0};
    sb_add(&b,
        "\nmb=new menubar children={\n"
        "  new menu caption=\"&Demo\" children={\n"
        "    new menuitem caption=\"&New\" shortcut=\"^N\" action=demo.file.new\n"
        "    new menuitem caption=\"&Open...\" shortcut=\"^O\"\n"
        "    new menuitem caption=\"&Save\" shortcut=\"^S\"\n"
        "  }\n"
        "  new menu caption=\"&Edit\" children={\n"
        "    new menuitem caption=\"Cu&t\" shortcut=\"^X\" action=demo.edit.cut\n"
        "    new menuitem caption=\"&Copy\" shortcut=\"^C\" action=demo.edit.copy\n"
        "    new menuitem caption=\"&Paste\" shortcut=\"^V\" action=demo.edit.paste\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"Select &All\" action=demo.edit.selectall\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"&Raw Key Input\" shortcut=\"^\\\\\" action=demo.edit.rawkey\n"
        "  }\n"
        "  new menu caption=\"&View\" children={\n"
        "    new menuitem caption=\"&Toolbar\" checkable checked\n"
        "    new menuitem caption=\"&Light/Dark Theme\" shortcut=\"^T\" action=demo.view.theme\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"Show A&nnouncements in Status Bar\" checkable action=demo.view.announce\n"
        "    new menuitem caption=\"Speak Announcements\" checkable action=demo.view.speak\n"
        "  }\n"
        "  new menu caption=\"&Window\" children={\n"
        "    new menuitem caption=\"&New Window\" action=demo.window.new\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"&Tile\" action=demo.window.tile\n"
        "    new menuitem caption=\"&Cascade\" action=demo.window.cascade\n"
        "  }\n"
        "  new menu caption=\"&Help\" children={\n"
        "    new menuitem caption=\"&About\" action=demo.help.about\n"
        "  }\n"
        "}\n");
    return b.p;
}

char *main_status_script(void) {
    return strdup(
        "\nsb=new statusbar children={\n"
        "  new section children={\n"
        "    new span text=\"Ready - Press \"\n"
        "    new span text=\"F10\" fg=red bg=white\n"
        "    new span text=\" for menu, Tab to navigate, Ctrl+Q to quit\"\n"
        "  }\n"
        "}\n");
}

char *protocol_window_script(void) {
    return strdup(
        "alias C=\"caption\"\n"
        "pw=new window title=\"Protocol Demo\" x=64 y=64 width=448 height=256 children={\n"
        "  root=new panel layout=vbox children={\n"
        "    new label C=\"This window's content was built from protocol text.\" wrap\n"
        "    pstatus=new label C=\"Interact below; events appear here.\"\n"
        "    new separator\n"
        "    cb=new checkbox C=\"Tri-state checkbox (watch the label above)\" tristate\n"
        "    inp=new textinput placeholder=\"Type here...\"\n"
        "    combo=new combobox children={new item C=\"Alpha\"; new item C=\"Beta\"; new item C=\"Gamma\"} selected=0\n"
        "    btn=new button C=\"Dispatch demo.hello\" action=demo.hello\n"
        "  }\n"
        "}\n"
        "pstatus=pw.root.pstatus\n"
        "pcb=pw.root.cb\n"
        "pinp=pw.root.inp\n"
        "pcombo=pw.root.combo\n");
}

char *demo_terminal_script(int n) {
    sbuf b = {0};
    int off = 40 + n * 16;
    sb_addf(&b,
        "dw%d=new window title=\"Demo Window\" x=%d y=%d width=480 height=320 tearable children={\n"
        "  dsp=new splitter orientation=vertical position=0.3 caption=\"Terminal\" children={\n"
        "    dtp=new panel layout=vbox spacing=8 children={\n"
        "      new label caption=\"This is a child window.\"\n"
        "      new textinput placeholder=\"Type something...\"\n"
        "      dclose=new button caption=\"Close\"\n"
        "    }\n"
        "    dterm=new terminal\n"
        "  }\n"
        "}\n"
        "dwin=dw%d\n"
        "dcloser=dw%d.dsp.dtp.dclose\n"
        "dterm=dw%d.dsp.dterm\n"
        "set dterm feed=\"\\e[1;36mThis banner arrived as protocol text.\\e[0m\\r\\n\\r\\n\"\n",
        n, off, off, n, n, n);
    return b.p;
}

char *about_dialog_script(void) {
    return strdup(
        "dlg=new messagebox title=\"About KittyTK\" icon=information ok "
        "text=\"KittyTK Demo\\n\\nA comprehensive cross-surface UI toolkit.\\n\\nVersion 0.1.0\"\n");
}

char *secondary_build_script(int n) {
    sbuf b = {0};
    int offset = (n - 1) % 5;
    int x = (offset * 3 + 5) * 8, y = (offset * 2 + 3) * 16;
    sb_addf(&b,
        "w=new window title=\"App %d Window\" x=%d y=%d width=480 height=320 tearable main children={\n"
        "  sp=new splitter orientation=vertical position=0.3 caption=\"Terminal\" children={\n"
        "    tp=new panel layout=vbox spacing=8 children={\n"
        "      new label caption=\"This window belongs to Application #%d\"\n"
        "      new textinput placeholder=\"Enter text here...\"\n"
        "      closebtn=new button caption=\"Close Window\"\n"
        "    }\n"
        "    term=new terminal\n"
        "  }\n"
        "}\n"
        "closer=w.sp.tp.closebtn\n"
        "term=w.sp.term\n"
        "mb=new menubar children={\n"
        "  new menu caption=\"&App %d\" children={ new menuitem caption=\"&Close Window\" shortcut=\"^W\" action=demo.app.close }\n"
        "  new menu caption=\"&Edit\" children={\n"
        "    new menuitem caption=\"Cu&t\" shortcut=\"^X\" action=demo.app.cut\n"
        "    new menuitem caption=\"&Copy\" shortcut=\"^C\" action=demo.app.copy\n"
        "    new menuitem caption=\"&Paste\" shortcut=\"^V\" action=demo.app.paste\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"Select &All\" action=demo.app.selectall\n"
        "    new menuitem separator\n"
        "    new menuitem caption=\"&Raw Key Input\" shortcut=\"^\\\\\" action=demo.app.rawkey\n"
        "  }\n"
        "  new menu caption=\"&Help\" children={ new menuitem caption=\"&About\" action=demo.app.about }\n"
        "}\n"
        "sb=new statusbar children={new section children={new span text=\"Secondary Application #%d\"}}\n",
        n, x, y, n, n, n);
    return b.p;
}

char *mdi_child_script(int n) {
    sbuf b = {0};
    int offset = (n - 1) % 5;
    sb_addf(&b,
        "set mdi children={d%d=new window title=\"Document %d\" x=%d y=%d width=240 height=128 children={\n"
        "  p=new panel layout=vbox spacing=8 children={\n"
        "    new label caption=\"Document #%d\"\n"
        "    new textinput placeholder=\"Enter document content...\"\n"
        "    bp=new panel layout=hbox spacing=8 children={ nb=new button caption=\"New\"; cl=new button caption=\"Close\" }\n"
        "  }\n"
        "}}\n"
        "wwin=mdi.d%d\n"
        "wnew=mdi.d%d.p.bp.nb\n"
        "wclose=mdi.d%d.p.bp.cl\n",
        n, n, (offset * 2 + 1) * 8, (offset + 1) * 16, n, n, n, n);
    return b.p;
}
