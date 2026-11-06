package mini

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Canvas draws into an RGBA frame. Coordinates are physical pixels.
type Canvas struct {
	Img *image.RGBA
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// blend mixes col over the (opaque) pixel at (x,y) with coverage a.
func (c *Canvas) blend(x, y int, col color.RGBA, a float64) {
	if a <= 0 || !(image.Point{x, y}).In(c.Img.Rect) {
		return
	}
	i := c.Img.PixOffset(x, y)
	p := c.Img.Pix[i : i+4 : i+4]
	p[0] = uint8(float64(p[0]) + (float64(col.R)-float64(p[0]))*a + 0.5)
	p[1] = uint8(float64(p[1]) + (float64(col.G)-float64(p[1]))*a + 0.5)
	p[2] = uint8(float64(p[2]) + (float64(col.B)-float64(p[2]))*a + 0.5)
	p[3] = 255
}

func (c *Canvas) Clear(col color.RGBA) {
	b := c.Img.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := c.Img.Pix[c.Img.PixOffset(b.Min.X, y):c.Img.PixOffset(b.Max.X, y)]
		for i := 0; i+3 < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = col.R, col.G, col.B, 255
		}
	}
}

func (c *Canvas) FillRect(r image.Rectangle, col color.RGBA) {
	r = r.Intersect(c.Img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := c.Img.PixOffset(x, y)
			c.Img.Pix[i], c.Img.Pix[i+1], c.Img.Pix[i+2], c.Img.Pix[i+3] = col.R, col.G, col.B, 255
		}
	}
}

// RRect draws an anti-aliased rounded rectangle with a bw-pixel border
// (bw == 0 for none).
func (c *Canvas) RRect(r image.Rectangle, rad float64, fill, border color.RGBA, bw float64) {
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	hx, hy := float64(r.Dx())/2, float64(r.Dy())/2
	if rad > hx {
		rad = hx
	}
	if rad > hy {
		rad = hy
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			qx := math.Abs(float64(x)+0.5-cx) - (hx - rad)
			qy := math.Abs(float64(y)+0.5-cy) - (hy - rad)
			sd := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - rad
			outer := clamp01(0.5 - sd)
			if outer <= 0 {
				continue
			}
			if bw > 0 {
				c.blend(x, y, border, outer)
				c.blend(x, y, fill, clamp01(0.5-(sd+bw)))
			} else {
				c.blend(x, y, fill, outer)
			}
		}
	}
}

// Line draws an anti-aliased segment of width w.
func (c *Canvas) Line(x0, y0, x1, y1, w float64, col color.RGBA) {
	minX, maxX := math.Min(x0, x1)-w-1, math.Max(x0, x1)+w+1
	minY, maxY := math.Min(y0, y1)-w-1, math.Max(y0, y1)+w+1
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	for y := int(minY); y <= int(maxY); y++ {
		for x := int(minX); x <= int(maxX); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := 0.0
			if l2 > 0 {
				t = clamp01(((px-x0)*dx + (py-y0)*dy) / l2)
			}
			d := math.Hypot(px-(x0+t*dx), py-(y0+t*dy))
			c.blend(x, y, col, clamp01(w/2+0.5-d))
		}
	}
}

// Text draws s with its baseline at y.
func (c *Canvas) Text(x, y int, s string, f font.Face, col color.RGBA) {
	c.TextIn(c.Img, x, y, s, f, col)
}

// TextIn draws into dst (typically a SubImage, which clips the text).
func (c *Canvas) TextIn(dst *image.RGBA, x, y int, s string, f font.Face, col color.RGBA) {
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: f,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// Sub returns a clipped view of the frame.
func (c *Canvas) Sub(r image.Rectangle) *image.RGBA {
	return c.Img.SubImage(r.Intersect(c.Img.Rect)).(*image.RGBA)
}
