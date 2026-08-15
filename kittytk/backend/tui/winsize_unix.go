//go:build !windows

package tui

import "golang.org/x/sys/unix"

// ttyPixelSize reports the terminal window's size in PIXELS, from the same
// TIOCGWINSZ the kernel keeps for every tty (ws_xpixel/ws_ypixel). 0,0 when
// the terminal never filled those fields in, which many do not.
//
// This is a second, independent channel to the same fact CSI 16 t reports, and
// it is worth having precisely because it fails differently: the escape query
// needs the terminal to recognise it AND to send a reply back through whatever
// sits between us, which a multiplexer or a terminal that ignores what it does
// not know will simply swallow. The ioctl asks the kernel about our own tty
// and cannot be intercepted. It is also what most programs that draw pictures
// use, so agreeing with it is agreeing with them.
func ttyPixelSize(fd int) (wPx, hPx int) {
	if fd < 0 {
		return 0, 0
	}
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0, 0
	}
	return int(ws.Xpixel), int(ws.Ypixel)
}
