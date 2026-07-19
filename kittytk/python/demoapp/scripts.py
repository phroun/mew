"""Display-protocol scripts that BUILD the demo's UI over the socket.

A direct port of examples/demoapp/scripts.go - and, tellingly, mostly a
copy: these are protocol text, which is language-neutral. Only the string
*building* (loops, interpolation) differs between Go and Python; the wire
output is identical.
"""

from kittytk.protocol import quote

# The demo tree, shared by the Lists and Scroll Lists tabs (nested children
# blocks ARE the tree).
TREE_ITEMS_SCRIPT = r"""
new item caption="Documents" expanded children={
    new item caption="Work" expanded children={
        new item caption="Report.txt"
        new item caption="Presentation.pptx"
        new item caption="Budget.xlsx"
        new item caption="Meeting Notes.md"
    }
    new item caption="Personal" children={
        new item caption="Notes.txt"
        new item caption="Journal.md"
        new item caption="Ideas.txt"
    }
    new item caption="Projects" children={
        new item caption="Alpha"
        new item caption="Beta"
        new item caption="Gamma"
    }
}
new item caption="Pictures" children={
    new item caption="Vacation"
    new item caption="Family"
    new item caption="Pets"
    new item caption="Events"
    new item caption="Screenshots"
}
new item caption="Downloads" children={
    new item caption="Software"
    new item caption="Documents"
    new item caption="Music"
}
new item caption="Music" children={
    new item caption="Rock"
    new item caption="Jazz"
    new item caption="Classical"
    new item caption="Electronic"
}
new item caption="Videos" children={
    new item caption="Movies"
    new item caption="TV Shows"
    new item caption="Tutorials"
}
new item caption="Code" children={
    new item caption="Go" children={
        new item caption="main.go"
        new item caption="utils.go"
    }
    new item caption="Python" children={
        new item caption="script.py"
    }
}
"""


def indent(s: str, prefix: str) -> str:
    """Re-indent a script fragment (whitespace is insignificant to the
    parser; this is only for readability of the composed script)."""
    return prefix + s.strip().replace("\n", "\n" + prefix) + "\n"


def main_build_script() -> str:
    """The whole primary application in one build: the KittyTK Demo window
    (nine tabs), its menu bar and its status bar."""
    b = []
    b.append(r'''
w=new window title="KittyTK Demo (Python)" width=480 height=288 tearable main children={
t=new tabs children={

b=new tab caption="Basic Trinkets" children={
    bw=new panel layout=vbox spacing=0 children={
        new label caption="This is a demo of basic trinkets:"
        input=new textinput placeholder="Enter text here..."
        new spacer
        new panel layout=hbox spacing=8 children={
            new button caption="OK" action=demo.basic.ok
            new button caption="Cancel" action=demo.basic.cancel
            new button caption="Apply" action=demo.basic.apply
        }
        new button caption="Disabled" !enabled
    }
}

s=new tab caption="Selection" children={
    o=new panel layout=vbox spacing=0 children={
        new panel layout=hbox spacing=8 align=fill children={
            new panel border layout=vbox fixed_width=256 children={
                new label caption="The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence" wrap align=fill
            }
            new panel border layout=vbox fixed_width=256 children={
                new label caption="Pack my box with five dozen liquor jugs before the Tuesday checkbox below doubles every letter" wrap align=fill
            }
            new panel border layout=vbox fixed_width=288 children={
                new panel layout=vbox align=fill children={
                    new checkbox caption="Enable the experimental feature that reticulates splines while the moon is full" wrap
                    new radiobutton caption="Prefer the long-form explanation whenever the assistant answers a question" wrap
                }
            }
        }
        sp=new splitter orientation=vertical position=0.4 stretch=1 align=fill children={
            c=new panel layout=vbox spacing=0 children={
                new label caption="Checkboxes:"
                new checkbox caption="Enable feature A" checked
                new checkbox caption="Enable feature B"
                new checkbox caption="Tri-state checkbox" tristate
                new label caption="Font Options:"
                wfont=new checkbox caption="Window: Tuesday (double-width)"
                dfont=new checkbox caption="Desktop: Tuesday (double-width)"
                grid=new checkbox caption="Window: 32-unit rows (denomination test)"
            }
            r=new panel layout=vbox spacing=0 children={
                new label caption="Radio buttons:"
                new radiobutton caption="Option 1" group=selopts
                new radiobutton caption="Option 2" group=selopts
                new radiobutton caption="Option 3" group=selopts
                new label caption="Tab Background Color:"
                bgdef=new radiobutton caption="Default" group=selbg checked
                bggreen=new radiobutton caption="Dark Green" group=selbg
                bggray=new radiobutton caption="TrueColor #333" group=selbg
                new label caption="ComboBox:"
                new combobox children={
                    new item caption="First item"
                    new item caption="Second item"
                    new item caption="Third item"
                    new item caption="Fourth item"
                }
                new label caption="Alphabet ComboBox:"
                new combobox children={''')
    for i in range(26):
        letter = chr(ord('A') + i)
        b.append("\n\t\t\t\t\tnew item caption=" + quote(letter + " - Letter " + letter))
    b.append(r'''
                }
            }
        }
    }
}

new tab caption="Lists" children={
    new splitter orientation=horizontal position=0.5 children={
        new panel layout=vbox children={
            new label caption="ListView:"
            new listview children={''')
    for i in range(1, 21):
        b.append('\n\t\t\t\tnew item caption="Item %d"' % i)
    b.append(r'''
            }
        }
        new panel layout=vbox children={
            new label caption="TreeView:"
            new treeview children={''' + indent(TREE_ITEMS_SCRIPT, "\t\t\t\t") + r'''}
        }
    }
}

ss=new tab caption="Scroll Selection" children={
    sp=new splitter orientation=vertical position=0.4 children={
        new scrollarea children={
            new panel layout=vbox spacing=0 children={
                new label caption="Checkboxes (scrollable):"''')
    for i in range(1, 16):
        checked = " checked" if i % 3 == 0 else ""
        b.append('\n\t\t\t\tnew checkbox caption="Feature option %d"%s' % (i, checked))
    b.append(r'''
            }
        }
        sa=new scrollarea children={
            sr=new panel layout=vbox spacing=0 children={
                new label caption="Radio buttons (scrollable):"''')
    for i in range(1, 11):
        b.append('\n\t\t\t\tnew radiobutton caption="Radio option %d with longer text" group=scrollopts' % i)
    b.append(r'''
                new label caption="Tab Background Color:"
                sbgdef=new radiobutton caption="Default" group=scrollbg checked
                sbggreen=new radiobutton caption="Dark Green" group=scrollbg
                sbggray=new radiobutton caption="TrueColor #333" group=scrollbg
                new label caption="ComboBox:"
                new combobox children={
                    new item caption="First item"
                    new item caption="Second item"
                    new item caption="Third item"
                    new item caption="Fourth item"
                }
            }
        }
    }
}

new tab caption="Scroll Lists" children={
    new splitter orientation=horizontal position=0.5 children={
        new scrollarea children={
            new panel layout=vbox children={
                new label caption="ListView (scrollable container):"
                new listview children={''')
    for i in range(1, 21):
        b.append('\n\t\t\t\t\tnew item caption="Item %d"' % i)
    b.append(r'''
                }
                new label caption="Extra content below ListView:"''')
    for i in range(1, 6):
        b.append('\n\t\t\t\tnew button caption="Button %d"' % i)
    b.append(r'''
            }
        }
        new scrollarea children={
            new panel layout=vbox children={
                new label caption="TreeView (scrollable container):"
                new treeview children={''' + indent(TREE_ITEMS_SCRIPT, "\t\t\t\t\t") + r'''}
                new label caption="Extra content below TreeView:"
                new textinput placeholder="Type something..."
            }
        }
    }
}

new tab caption="Progress" children={
    new panel layout=vbox spacing=16 children={
        new label caption="Horizontal Progress Bars:"
        new progress value=25
        new progress value=50
        new progress value=75
        new progress value=100
        new label caption="Indeterminate Progress:"
        new progress indeterminate
    }
}

new tab caption="Bottom Tabs" children={
    new tabs position=bottom children={
        new tab caption="First" children={
            new panel layout=vbox children={
                new label caption="This TabTrinket has tabs at the bottom."
                new label caption="Notice how the tab connectors are inverted:"
                new label caption="  Top tabs use: _/ and \\_"
                new label caption="  Bottom tabs use: \\_ and _/"
            }
        }
        new tab caption="Second" children={
            new panel layout=vbox children={
                new label caption="Second tab content"
                new button caption="Click me"
            }
        }
        new tab caption="Third" children={
            new panel layout=vbox children={
                new label caption="Third tab with an input field:"
                new textinput placeholder="Type here..."
            }
        }
    }
}

new tab caption="Vertical Tabs" children={
    new splitter orientation=horizontal position=0.5 children={
        new tabs position=left children={
            new tab caption="First" children={
                new panel layout=vbox children={
                    new label caption="This is the first tab in a\nTabsLeft layout."
                    new label caption="Tabs are displayed vertically\nalong the left edge."
                }
            }
            new tab caption="Second" children={
                new panel layout=vbox children={
                    new label caption="Second tab content"
                    new button caption="A Button"
                }
            }
            new tab caption="Third" children={
                new panel layout=vbox children={
                    new textinput placeholder="Type here..."
                }
            }''')
    for name in ["Fourth", "Fifth", "Sixth", "Seventh", "Eighth", "Ninth",
                 "Tenth", "Eleventh", "Twelfth", "Thirteenth"]:
        b.append('\n\t\t\t\tnew tab caption=%s children={\n'
                 '\t\t\t\t\tnew panel layout=vbox children={\n'
                 '\t\t\t\t\t\tnew label caption="%s tab content\\nin TabsLeft layout."\n'
                 '\t\t\t\t\t}\n'
                 '\t\t\t\t}' % (quote(name), name))
    b.append(r'''
        }
        new tabs position=right children={
            new tab caption="Alpha" children={
                new panel layout=vbox children={
                    new label caption="This is the first tab in a\nTabsRight layout."
                    new label caption="Tabs are displayed vertically\nalong the right edge."
                }
            }
            new tab caption="Beta" children={
                new panel layout=vbox children={
                    new label caption="Beta tab content"
                    new checkbox caption="Enable option"
                }
            }
            new tab caption="Gamma" children={
                new panel layout=vbox children={
                    new label caption="Gamma tab content"
                }
            }''')
    for name in ["Delta", "Epsilon", "Zeta", "Eta", "Theta", "Iota",
                 "Kappa", "Lambda", "Mu", "Nu"]:
        b.append('\n\t\t\t\tnew tab caption=%s children={\n'
                 '\t\t\t\t\tnew panel layout=vbox children={\n'
                 '\t\t\t\t\t\tnew label caption="%s tab content\\nin TabsRight layout."\n'
                 '\t\t\t\t\t}\n'
                 '\t\t\t\t}' % (quote(name), name))
    b.append(r'''
        }
    }
}

mtab=new tab caption="MDI Demo" children={
    mdisp=new splitter orientation=vertical position=0.9 caption="Dock" children={
        mdisa=new scrollarea children={
            mdi=new mdipane fill="░" min_width=640 min_height=400 max_width=640 max_height=400 children={
                mdicp=new panel layout=vbox spacing=8 children={
                    new label caption="MDIPane Trinket Demo"
                    new label caption="This MDIPane trinket manages floating windows.\nClick [_] to minimize windows to the dock below."
                    new button caption="Spawn Window in MDIPane" action=demo.mdi.spawn
                    new panel layout=hbox spacing=8 children={
                        new button caption="Tile" action=demo.mdi.tile
                        new button caption="Cascade" action=demo.mdi.cascade
                        new button caption="Next" action=demo.mdi.next
                        new button caption="Prev" action=demo.mdi.prev
                    }
                    mdistatus=new label caption="Active: none"
                    new spacer
                    new label caption="Tips:"
                    new label caption="- Click [_] to minimize to dock"
                    new label caption="- Click dock entry to restore"
                    new label caption="- Double-click title to maximize"
                }
            }
        }
        mdidock=new dockrow entry_width=20
    }
}

}
}

# Surface what the app-side handlers address, then open the event flows
# they listen to (command flows regardless; toggles/changes need a sub).
tabs=w.t
binput=w.t.b.bw.input
wfont=w.t.s.o.sp.c.wfont
dfont=w.t.s.o.sp.c.dfont
grid=w.t.s.o.sp.c.grid
bgdef=w.t.s.o.sp.r.bgdef
bggreen=w.t.s.o.sp.r.bggreen
bggray=w.t.s.o.sp.r.bggray
sbgdef=w.t.ss.sp.sa.sr.sbgdef
sbggreen=w.t.ss.sp.sa.sr.sbggreen
sbggray=w.t.ss.sp.sa.sr.sbggray
mdi=w.t.mtab.mdisp.mdisa.mdi
mdistatus=w.t.mtab.mdisp.mdisa.mdi.mdicp.mdistatus
mdidock=w.t.mtab.mdisp.mdidock
''')
    b.append(main_menu_script())
    b.append(MAIN_STATUS_SCRIPT)
    return "".join(b)


def main_menu_script() -> str:
    """The primary application's menu bar (Demo, Edit, View, Window,
    Alphabet, Help). action= IDs dispatch back as command events."""
    b = [r'''
mb=new menubar children={
    new menu caption="&Demo" children={
        new menuitem caption="&New" shortcut="^N" action=demo.file.new
        new menuitem caption="&Open..." shortcut="^O"
        new menuitem caption="&Save" shortcut="^S"
    }
    new menu caption="&Edit" children={
        new menuitem caption="Cu&t" shortcut="^X" action=demo.edit.cut
        new menuitem caption="&Copy" shortcut="^C" action=demo.edit.copy
        new menuitem caption="&Paste" shortcut="^V" action=demo.edit.paste
        new menuitem separator
        new menuitem caption="Select &All" action=demo.edit.selectall
        new menuitem separator
        new menuitem caption="&Raw Key Input" shortcut="^\\" action=demo.edit.rawkey
    }
    new menu caption="&View" children={
        new menuitem caption="&Toolbar" checkable checked
        new menuitem caption="&Status Bar" checkable checked
        new menuitem separator
        new menuitem caption="&Light/Dark Theme" shortcut="^T" action=demo.view.theme
        new menuitem separator
        new menuitem caption="Show A&nnouncements in Status Bar" checkable action=demo.view.announce
        new menuitem caption="Speak Announcements" checkable action=demo.view.speak
    }
    new menu caption="&Window" children={
        new menuitem caption="&New Window" action=demo.window.new
        new menuitem separator
        new menuitem caption="&Tile" action=demo.window.tile
        new menuitem caption="&Cascade" action=demo.window.cascade
    }
    new menu caption="&Alphabet" children={''']
    for i in range(26):
        letter = chr(ord('A') + i)
        b.append('\n\t\tnew menuitem caption="&%s - Letter %s"' % (letter, letter))
        if i == 2:  # separator after "Letter C"
            b.append("\n\t\tnew menuitem separator")
    b.append(r'''
    }
    new menu caption="&Help" children={
        new menuitem caption="&About" action=demo.help.about
    }
}
''')
    return "".join(b)


# The primary application's status bar.
MAIN_STATUS_SCRIPT = r'''
sb=new statusbar children={
    new section children={
        new span text="Ready - Press "
        new span text="F10" fg=red bg=white
        new span text=" for menu, Tab to navigate, "
        new span text="Ctrl+Q" fg=red bg=white
        new span text=" to quit"
    }
}
'''

# A second window built entirely from protocol text.
PROTOCOL_WINDOW_SCRIPT = r'''
alias C="caption"
pw=new window title="Protocol Demo" x=64 y=64 width=448 height=256 children={
    root=new panel layout=vbox children={
        new label C="This window's content was built from protocol text." wrap
        pstatus=new label C="Interact below; events appear here."
        new separator
        cb=new checkbox C="Tri-state checkbox (watch the label above)" tristate
        inp=new textinput placeholder="Type here..."
        combo=new combobox children={new item C="Alpha"; new item C="Beta"; new item C="Gamma"} selected=0
        btn=new button C="Dispatch demo.hello" action=demo.hello
    }
}
pstatus=pw.root.pstatus
pcb=pw.root.cb
pinp=pw.root.inp
pcombo=pw.root.combo
'''


def demo_terminal_script(n: int) -> str:
    """The "Demo Window" (Demo > New): a control panel over an embedded
    shell terminal. feed= streams a banner in before the shell starts."""
    off = 40 + n * 16
    return (
        r'''
dw%d=new window title="Demo Window" x=%d y=%d width=480 height=320 tearable children={
    dsp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
        dtp=new panel layout=vbox spacing=8 children={
            new label caption="This is a child window."
            new textinput placeholder="Type something..."
            dclose=new button caption="Close"
        }
        dterm=new terminal
    }
}
dwin=dw%d
dcloser=dw%d.dsp.dtp.dclose
dterm=dw%d.dsp.dterm
set dterm feed="\e[1;36mThis banner arrived as protocol text.\e[0m\r\n\r\n"
set dterm shell
''' % (n, off, off, n, n, n)
    )


# The About message box.
ABOUT_DIALOG_SCRIPT = r'''
dlg=new messagebox title="About KittyTK" icon=information ok text="KittyTK Demo\n\nA comprehensive cross-surface UI toolkit.\n\nVersion 0.1.0"
'''


def secondary_build_script(n: int) -> str:
    """A whole secondary application: a window with a control panel over a
    PurfecTerm, its own menu bar and status bar."""
    offset = (n - 1) % 5
    x = (offset * 3 + 5) * 8
    y = (offset * 2 + 3) * 16
    return (
        r'''
w=new window title="App %d Window" x=%d y=%d width=480 height=320 tearable main children={
    sp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
        tp=new panel layout=vbox spacing=8 children={
            new label caption="This window belongs to Application #%d"
            new label caption="Notice the menu bar and status bar change\nwhen this window is focused."
            new textinput placeholder="Enter text here..."
            closebtn=new button caption="Close Window"
        }
        term=new terminal
    }
}
closer=w.sp.tp.closebtn
term=w.sp.term
set term shell
mb=new menubar children={
    new menu caption="&App %d" children={
        new menuitem caption="&Close Window" shortcut="^W" action=demo.app.close
    }
    new menu caption="&Edit" children={
        new menuitem caption="Cu&t" shortcut="^X" action=demo.app.cut
        new menuitem caption="&Copy" shortcut="^C" action=demo.app.copy
        new menuitem caption="&Paste" shortcut="^V" action=demo.app.paste
        new menuitem separator
        new menuitem caption="Select &All" action=demo.app.selectall
        new menuitem separator
        new menuitem caption="&Raw Key Input" shortcut="^\\" action=demo.app.rawkey
    }
    new menu caption="&Info" children={
        new menuitem caption="&About This App" action=demo.app.info
    }
    new menu caption="&Help" children={
        new menuitem caption="&About" action=demo.app.about
    }
}
sb=new statusbar children={new section children={new span text="Secondary Application #%d"}}
''' % (n, x, y, n, n, n)
    )


def mdi_child_script(n: int) -> str:
    """Spawn one document window inside the MDI pane, wired through click
    events (no per-child command IDs to collide)."""
    offset = (n - 1) % 5
    return (
        r'''
set mdi children={d%d=new window title="Document %d" x=%d y=%d width=240 height=128 children={
    p=new panel layout=vbox spacing=8 children={
        new label caption="Document #%d"
        new textinput placeholder="Enter document content..."
        bp=new panel layout=hbox spacing=8 children={
            nb=new button caption="New"
            cl=new button caption="Close"
        }
    }
}}
wwin=mdi.d%d
wnew=mdi.d%d.p.bp.nb
wclose=mdi.d%d.p.bp.cl
''' % (n, n, (offset * 2 + 1) * 8, (offset + 1) * 16, n, n, n, n)
    )
