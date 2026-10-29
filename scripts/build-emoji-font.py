"""Derives HyperBerry's emoji font from Google's Noto Color Emoji (PLAN.md §7, step 0.7).

Cascades draws text with Monotype iType, which loads a font through a FontFaceRule but
applies none of its GSUB. On the Q10 the original took 15 s and 177 MB to draw. So:
  - keep only the glyphs the cmap reaches: the sequence glyphs need GSUB (user, 2026-10-29:
    no substitutions), and so does nothing else
  - drop the cmap below U+0100 (digits, #, *, space, (c), (R)), so text never takes them from
    here instead of Slate Pro
  - scale every colour bitmap from the font's 109 ppem down to PPEM, near the drawn size

    build-emoji-font.py <NotoColorEmoji.ttf> <HyperBerryEmoji.ttf>
"""
import io
import sys

from fontTools import subset
from fontTools.ttLib import TTFont
from PIL import Image

PPEM = 32  # the size Cascades draws emoji in body text on the Q10 (measured 2026-10-29)


def main(src, dst):
    font = TTFont(src)
    options = subset.Options()
    options.layout_features = []
    options.drop_tables += ["GSUB", "GPOS", "GDEF"]
    options.name_IDs = ["*"]
    sub = subset.Subsetter(options)
    sub.populate(unicodes=[u for u in font.getBestCmap() if u >= 0x100])
    sub.subset(font)

    (strike,) = font["CBLC"].strikes
    size = strike.bitmapSizeTable
    scale = PPEM / size.ppemX
    for bitmap in font["CBDT"].strikeData[0].values():  # format 17: small metrics + PNG
        m = bitmap.metrics
        w, h = max(1, round(m.width * scale)), max(1, round(m.height * scale))
        out = io.BytesIO()
        Image.open(io.BytesIO(bitmap.imageData)).resize((w, h), Image.LANCZOS).save(
            out, "PNG", optimize=True)
        bitmap.imageData = out.getvalue()
        m.width, m.height = w, h
        m.BearingX, m.BearingY = round(m.BearingX * scale), round(m.BearingY * scale)
        m.Advance = round(m.Advance * scale)
    size.ppemX = size.ppemY = PPEM
    for lines in (size.hori, size.vert):
        for name in ("ascender", "descender", "widthMax", "minOriginSB", "minAdvanceSB",
                     "maxBeforeBL", "minAfterBL"):
            setattr(lines, name, round(getattr(lines, name) * scale))
    font.save(dst)
    print("%s: %d glyphs, %d code points, %d ppem" % (
        dst, len(font.getGlyphOrder()), len(font.getBestCmap()), PPEM))


if __name__ == "__main__":
    main(*sys.argv[1:])
