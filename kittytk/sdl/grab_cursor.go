package sdl

// The closed hand shown while something is carried (core.CursorGrabbing).
// SDL has no system cursor for it, so it is drawn here: X is the outline, o
// the fill, anything else transparent. It is KittyTK's own picture rather
// than the system theme's, and this table is the one place to replace it.
var grabHandMask = [...]string{
	"................",
	"................",
	"................",
	"....XX.XX.XX....",
	"...XooXooXooXX..",
	"...XooooooooXoX.",
	"....XoooooooooX.",
	"...XXoooooooooX.",
	"..XooooooooooX..",
	"..XooooooooooX..",
	"...XoooooooooX..",
	"....XooooooooX..",
	".....XoooooooX..",
	"......XooooooX..",
	"......XooooooX..",
	"......XXXXXXXX..",
}

// grabHandHotX and grabHandHotY are the point of the mask that is the
// pointer's position: the middle of the palm.
const grabHandHotX, grabHandHotY = 8, 9

// grabHandPixels is the mask as RGBA bytes, each mask pixel drawn scale
// pixels square, with its width and height. Scale 2 is the picture SDL shows
// on a display with two device pixels to the point.
func grabHandPixels(scale int) (pix []byte, w, h int) {
	w, h = len(grabHandMask[0])*scale, len(grabHandMask)*scale
	pix = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		row := grabHandMask[y/scale]
		for x := 0; x < w; x++ {
			var r, g, b, a byte
			switch row[x/scale] {
			case 'X':
				a = 0xff
			case 'o':
				r, g, b, a = 0xff, 0xff, 0xff, 0xff
			}
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = r, g, b, a
		}
	}
	return pix, w, h
}
