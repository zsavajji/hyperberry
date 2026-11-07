"""Draws the call screen's images (step 7.4) into app/assets/images/, in the stock BB10 Phone
app's look (measured from the user's screenshots, 720x720, 1 du = 9 px): call_bg (the gradient with
the default-contact silhouette), call_fade (a photo's fade to black), call_track (the answer bar:
capsule, green and red arcs, the red handset, the chevrons; 665x148), call_knob, call_btn (the
round button's disc) and the white glyphs call_silence, call_message, call_speaker, call_mute and
call_end, which QML tints with ImageView.filterColor. Everything is drawn at 8x and scaled down.
Run by hand when one changes, in the image fetch-emoji.sh builds:
    docker run --rm -v "$PWD:/src" hyperberry-font python -I /src/scripts/gen-call-images.py /src/app/assets/images
"""
import sys

from PIL import Image, ImageDraw, ImageFilter

S = 8
WHITE = (255, 255, 255, 255)
CLEAR = (0, 0, 0, 0)


def new(w, h):
    img = Image.new("RGBA", (round(w * S), round(h * S)), CLEAR)
    return img, ImageDraw.Draw(img)


def save(img, out, name):
    img.resize((img.width // S, img.height // S), Image.LANCZOS).save(f"{out}/{name}.png")


def hexa(c):
    return tuple(int(c[i:i + 2], 16) for i in (1, 3, 5)) + (255,)


def sc(*xy):
    return [v * S for v in xy]


def handset(w, h, color):
    """A phone handset lying flat: an arch with a pad at each end, hanging down (the hang-up
    one); returned at 8x. Rotated 135 degrees it is the answer one."""
    img, d = new(w, h)
    d.ellipse(sc(0, 0, w, 2 * h), color)
    d.rectangle(sc(0, 0.78 * h, w, 2 * h), CLEAR)
    d.ellipse(sc(0.17 * w, 0.34 * h, 0.83 * w, 2.4 * h), CLEAR)
    d.rounded_rectangle(sc(0, 0.38 * h, 0.27 * w, h), 0.1 * h * S, color)
    d.rounded_rectangle(sc(0.73 * w, 0.38 * h, w, h), 0.1 * h * S, color)
    return img


def put(img, part, cx, cy, angle=0):
    """Pastes a supersampled part centred on (cx, cy) in final px, turned by angle degrees clockwise."""
    if angle:
        part = part.rotate(-angle, Image.BICUBIC, expand=True)
    img.alpha_composite(part, (round(cx * S - part.width / 2), round(cy * S - part.height / 2)))


def lerp(table, y):
    """Cosine interpolation of (y, v...) rows: smooth, flat at each row."""
    import math
    if y <= table[0][0]:
        return table[0][1:]
    for a, b in zip(table, table[1:]):
        if y <= b[0]:
            t = (1 - math.cos(math.pi * (y - a[0]) / (b[0] - a[0]))) / 2
            return tuple(p + (q - p) * t for p, q in zip(a[1:], b[1:]))
    return table[-1][1:]


# The silhouette's colour (y, r, g, b), measured from the stock screen; its shape is in background().
SHADE = [(0, 0x45, 0x41, 0x52), (190, 0x45, 0x41, 0x52), (192, 0x43, 0x3f, 0x50), (224, 0x3f, 0x3b, 0x4a),
         (256, 0x38, 0x36, 0x43), (288, 0x32, 0x2f, 0x3a), (320, 0x2b, 0x28, 0x33), (352, 0x23, 0x22, 0x2a),
         (368, 0x1e, 0x1d, 0x23), (384, 0x1a, 0x19, 0x1f)]


def gradient(y):
    t = min(1, max(0, (y - 300) / 210))
    return tuple(v * (1 - t) for v in (0x1b, 0x13, 0x2a))


def background(out):
    """Flat #1b132a to y 300, linear to black at 510; the default-contact silhouette fades into it."""
    W = 720
    bg = Image.new("RGB", (W, W))
    sil = Image.new("RGB", (W, W))
    for y in range(W):
        g = gradient(y)
        if y < 384:
            c = lerp(SHADE, y)
        elif y < 400:  # into the background
            u = (y - 384) / 16
            c = tuple(a + (b - a) * u for a, b in zip(lerp(SHADE, 384), g))
        else:  # the shoulders are only a hint
            k = min((y - 400) / 50, 1) * min(1, max(0, (520 - y) / 70))
            c = tuple(a + k * h for a, h in zip(g, (13, 12, 16)))
        ImageDraw.Draw(bg).line((0, y, W, y), tuple(round(v) for v in g))
        ImageDraw.Draw(sil).line((0, y, W, y), tuple(round(v) for v in c))
    # The shape, fitted to the stock outline's rows: a flat-topped skull (190 px wide 9 px below
    # its top at 87; corners of 80, a dome as the stock one), the jaw rounding into the neck (418-578 at y 352, 432-563 at
    # 362: corners of 110), the ears, the neck, and the shoulders' wide curve (305-695 at y 465).
    m, d = new(W, W)
    d.rounded_rectangle(sc(380, 87, 617, 300), 80 * S, WHITE)
    d.rounded_rectangle(sc(380, 160, 617, 380), 110 * S, WHITE)
    d.rounded_rectangle(sc(368, 205, 392, 275), 12 * S, WHITE)  # the ears
    d.rounded_rectangle(sc(605, 205, 629, 275), 12 * S, WHITE)
    d.rectangle(sc(432, 340, 563, 440), WHITE)  # the neck
    d.ellipse(sc(140, 418, 860, 1018), WHITE)  # the shoulders
    m = m.resize((W, W), Image.LANCZOS).split()[3].filter(ImageFilter.GaussianBlur(1))
    bg.paste(sil, (0, 0), m)
    bg.save(f"{out}/call_bg.png")


def fade(out):
    """Transparent at the top, black at the bottom: over a photo's lower part."""
    img = Image.new("RGBA", (720, 260))
    px = img.load()
    for y in range(260):
        a = round(255 * (y / 259) ** 1.4)
        for x in range(720):
            px[x, y] = (0, 0, 0, a)
    img.save(f"{out}/call_fade.png")


def chevrons(d, x0, colors, left):
    for i, c in enumerate(colors):
        x = x0 + i * 31
        a, b = (x + 22, x) if left else (x, x + 22)
        d.line(sc(a, 57, b, 74, a, 91), hexa(c), 5 * S, joint="curve")


def track(out):
    W, H = 665, 148
    img, d = new(W, H)
    d.rounded_rectangle(sc(0, 0, W, H), 74 * S, hexa("#1e1e1e"))
    green, red = hexa("#789038"), hexa("#e83838")
    d.arc(sc(1.5, 1.5, 146.5, 146.5), 90, 270, green, 4 * S)
    d.arc(sc(W - 146.5, 1.5, W - 1.5, 146.5), 270, 90, red, 4 * S)
    put(img, handset(85, 32, hexa("#f83030")), 583, 74)
    # Brighter toward the middle on the green side, dimmer away from it on the red.
    chevrons(d, 168, ["#2e3a20", "#46552d", "#63773c"], True)
    chevrons(d, 413, ["#983e3d", "#753132", "#592527"], False)
    save(img, out, "call_track")


def knob(out):
    img, d = new(110, 110)
    d.ellipse(sc(0, 0, 110, 110), hexa("#e8e8e8"))
    d.ellipse(sc(4, 4, 106, 106), hexa("#1e1e1e"))
    d.ellipse(sc(11, 11, 99, 99), hexa("#949494"))
    save(img, out, "call_knob")


def button(out):
    img, d = new(104, 104)
    d.ellipse(sc(0, 0, 104, 104), hexa("#2a2a2c"))
    d.ellipse(sc(2, 2, 102, 102), hexa("#040404"))
    save(img, out, "call_btn")


def silence(out):
    """Beamed notes struck through, a gap around the stroke."""
    img, d = new(50, 50)
    d.ellipse(sc(9, 33, 21, 43), WHITE)
    d.ellipse(sc(28, 29, 40, 39), WHITE)
    d.rectangle(sc(19, 10, 22, 38), WHITE)
    d.rectangle(sc(38, 6, 41, 34), WHITE)
    d.polygon(sc(19, 8, 41, 3, 41, 10, 19, 15), WHITE)
    d.line(sc(43, 3, 6, 47), CLEAR, 8 * S)
    d.line(sc(43, 3, 6, 47), WHITE, 3 * S)
    save(img, out, "call_silence")


def message(out):
    img, d = new(50, 50)
    d.rounded_rectangle(sc(3, 7, 47, 37), 4 * S, WHITE)
    d.polygon(sc(9, 35, 23, 35, 12, 46), WHITE)
    for y, x1 in ((15, 40), (21, 40), (27, 30)):
        d.line(sc(10, y, x1, y), CLEAR, 3 * S)
    save(img, out, "call_message")


def speaker(out):
    img, d = new(50, 50)
    d.polygon(sc(3, 19, 12, 19, 25, 8, 25, 42, 12, 31, 3, 31), WHITE)
    d.arc(sc(16, 15, 34, 35), -55, 55, WHITE, 3 * S)
    d.arc(sc(14, 6, 46, 44), -55, 55, WHITE, 3 * S)
    save(img, out, "call_speaker")


def mute(out):
    img, d = new(50, 50)
    d.rounded_rectangle(sc(18, 4, 32, 30), 7 * S, WHITE)
    d.arc(sc(11, 14, 39, 38), 0, 180, WHITE, 3 * S)
    d.rectangle(sc(23.5, 38, 26.5, 44), WHITE)
    d.rectangle(sc(15, 43, 35, 46), WHITE)
    d.line(sc(6, 5, 44, 46), CLEAR, 8 * S)
    d.line(sc(6, 5, 44, 46), WHITE, 3 * S)
    save(img, out, "call_mute")


def end(out):
    save(handset(62, 22, WHITE), out, "call_end")


if __name__ == "__main__":
    for f in (background, fade, track, knob, button, silence, message, speaker, mute, end):
        f(sys.argv[1])
