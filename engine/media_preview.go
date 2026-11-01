package main

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"
)

// previewSide is the longer side of a photo's preview, in pixels.
const previewSide = 320

// boxScale shrinks src so its longer side is side pixels, each new pixel the average of the
// block of source pixels it covers. An image that already fits comes back as it is.
// ponytail: the generic image.Image path costs a second or two on a 12 MP photo on the Q10 (in
// the background); a YCbCr fast path reading the planes if it shows.
func boxScale(src image.Image, side int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= side {
		return src
	}
	nw, nh := max(w*side/long, 1), max(h*side/long, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0 := y * h / nh
		y1 := max((y+1)*h/nh, y0+1)
		for x := 0; x < nw; x++ {
			x0 := x * w / nw
			x1 := max((x+1)*w/nw, x0+1)
			var r, g, bl, a uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = byte(r/n>>8), byte(g/n>>8), byte(bl/n>>8), byte(a/n>>8)
		}
	}
	return dst
}

// previewOf decodes a photo (JPEG or PNG) and gives its preview as a JPEG, with the photo's own
// size. EXIF orientation is ignored: WhatsApp sends photos already turned.
func previewOf(data []byte) (jpg []byte, w, h int, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, boxScale(img, previewSide), &jpeg.Options{Quality: 70}); err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}
