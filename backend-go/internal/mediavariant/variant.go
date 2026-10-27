// Package mediavariant makes smaller copies of cached images for the board.
// It never changes the original file; callers store the result in a derived
// cache and fall back to the original whenever a copy cannot be made.
package mediavariant

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"slices"

	xdraw "golang.org/x/image/draw"

	"github.com/yexca/kano-navi/backend-go/internal/mediaformat"
)

// Widths are the only sizes the server makes. A fixed list keeps the cache
// bounded and lets browsers pick from srcset without inventing sizes.
var Widths = []int{320, 640, 960, 1280, 1920}

// ErrUnsupported means the original should be served instead: an animated or
// undecodable format, an image no wider than the request, or one too large
// to decode safely.
var ErrUnsupported = errors.New("variant unsupported")

const jpegQuality = 82

// ValidWidth reports whether width is one of Widths.
func ValidWidth(width int) bool { return slices.Contains(Widths, width) }

// Resize scales content down to width, keeping its aspect ratio. Opaque images
// become JPEG and images with transparency become PNG. The result is returned
// only when it is smaller than the original.
func Resize(content []byte, width int) ([]byte, string, error) {
	if !ValidWidth(width) {
		return nil, "", ErrUnsupported
	}
	mime := mediaformat.Sniff(content)
	// GIF may be animated and AVIF has no decoder; both are served whole.
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/webp" {
		return nil, "", ErrUnsupported
	}
	w, h, ok := mediaformat.Dimensions(content)
	if !ok || w <= width || int64(w)*int64(h) > mediaformat.MaxPixels {
		return nil, "", ErrUnsupported
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, "", ErrUnsupported
	}
	height := max(1, int(int64(h)*int64(width)/int64(w)))
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	outMIME := "image/jpeg"
	if opaque(target) {
		err = jpeg.Encode(&out, target, &jpeg.Options{Quality: jpegQuality})
	} else {
		outMIME = "image/png"
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, target)
	}
	if err != nil || out.Len() == 0 || out.Len() >= len(content) {
		return nil, "", ErrUnsupported
	}
	return out.Bytes(), outMIME, nil
}

func opaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}
