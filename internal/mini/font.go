package mini

import (
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

type faceKey struct {
	bold bool
	q    int // size in quarter pixels
}

var (
	fontMu      sync.Mutex
	regularFont *opentype.Font
	boldFont    *opentype.Font
	faceCache   = map[faceKey]font.Face{}
)

// face returns a cached face at the given pixel size.
func face(bold bool, px float64) font.Face {
	fontMu.Lock()
	defer fontMu.Unlock()
	if regularFont == nil {
		var err error
		if regularFont, err = opentype.Parse(goregular.TTF); err != nil {
			panic(err)
		}
		if boldFont, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
	}
	k := faceKey{bold, int(px*4 + 0.5)}
	if f, ok := faceCache[k]; ok {
		return f
	}
	src := regularFont
	if bold {
		src = boldFont
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{
		Size:    float64(k.q) / 4,
		DPI:     72, // Size is then in pixels
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	faceCache[k] = f
	return f
}

func textWidth(f font.Face, s string) int { return font.MeasureString(f, s).Ceil() }

func lineHeight(f font.Face) int {
	m := f.Metrics()
	return m.Ascent.Ceil() + m.Descent.Ceil()
}
