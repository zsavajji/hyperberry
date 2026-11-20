"""Draws the call screen's images (step 7.4) into app/assets/images/, in the stock BB10 Phone
app's look (measured from the user's screenshots, 720x720, 1 du = 9 px): call_bg (the gradient with
the default-contact silhouette), call_fade (a photo's fade to black), call_track (the answer bar:
capsule, green and red arcs, the red handset, the chevrons; 665x148), call_knob, call_btn (the
round button's disc) and the white glyphs call_message, call_speaker, call_mute,
call_unmute, call_end, call_video, call_video_off, call_switch, call_preview and call_preview_off, which QML tints with
ImageView.filterColor. Everything is drawn at 8x and scaled down. The people drawer's
(step 7e V3) are call_drawer_shadow (the shadow the page casts on the sidebar: 12x8, clear at its
left, black at 20 % at its right, stretched vertically) and the hint's (Hint.qml, the lock screen's tutorial look): call_hint_info (the white
disc with the italic "i") and call_hint_chevrons (two white chevrons pointing up, turned in QML).
Run by hand when one changes, in the image fetch-emoji.sh builds:
    docker run --rm -v "$PWD:/src" hyperberry-font python -I /src/scripts/gen-call-images.py /src/app/assets/images
"""
import colorsys
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


def accent(rgb):
    """The stock screen's purple turned to HyperBerry's primary colour, #1bd862 (the user's ask,
    2026-11-07): its hue, with the stock colour's own saturation and brightness kept."""
    h, s, v = colorsys.rgb_to_hsv(*[c / 255 for c in rgb])
    hue = colorsys.rgb_to_hsv(0x1b / 255, 0xd8 / 255, 0x62 / 255)[0]
    return tuple(c * 255 for c in colorsys.hsv_to_rgb(hue, s, v))


def gradient(y):
    t = min(1, max(0, (y - 300) / 210))
    return tuple(v * (1 - t) for v in accent((0x1b, 0x13, 0x2a)))


def background(out):
    """The stock #1b132a in the primary colour's hue, flat to y 300, then linear to black at 510; the
    default-contact silhouette fades into it."""
    W = 720
    bg = Image.new("RGB", (W, W))
    sil = Image.new("RGB", (W, W))
    for y in range(W):
        g = gradient(y)
        if y < 384:
            c = accent(lerp(SHADE, y))
        elif y < 400:  # into the background
            u = (y - 384) / 16
            c = tuple(a + (b - a) * u for a, b in zip(accent(lerp(SHADE, 384)), g))
        else:  # the shoulders are only a hint
            k = min((y - 400) / 50, 1) * min(1, max(0, (520 - y) / 70))
            c = tuple(a + k * h for a, h in zip(g, accent((13, 12, 16))))
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
    green, red = hexa("#1bd862"), hexa("#e83838")  # answer in the primary colour (the user's ask)
    d.arc(sc(1.5, 1.5, 146.5, 146.5), 90, 270, green, 4 * S)
    d.arc(sc(W - 146.5, 1.5, W - 1.5, 146.5), 270, 90, red, 4 * S)
    put(img, handset(85, 32, hexa("#f83030")), 583, 74)
    # Brighter toward the middle on the green side, dimmer away from it on the red.
    chevrons(d, 168, ["#0b5627", "#10823b", "#16b351"], True)  # the primary at 40, 60 and 83 %, as the stock's
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


def mic():
    img, d = new(50, 50)
    d.rounded_rectangle(sc(18, 4, 32, 30), 7 * S, WHITE)
    d.arc(sc(11, 14, 39, 38), 0, 180, WHITE, 3 * S)
    d.rectangle(sc(23.5, 38, 26.5, 44), WHITE)
    d.rectangle(sc(15, 43, 35, 46), WHITE)
    return img, d


def mute(out):
    img, d = mic()
    d.line(sc(6, 5, 44, 46), CLEAR, 8 * S)
    d.line(sc(6, 5, 44, 46), WHITE, 3 * S)
    save(img, out, "call_mute")


def unmute(out):
    """The mic without its slash: what the Mute toggle shows while muted, as the stock one does."""
    save(mic()[0], out, "call_unmute")


def end(out):
    """The stock End call glyph (the user's screenshot, 2026-11-20): a wide, shallow handset, thick on
    top, its ear and mouth pads ovals tipped outward, and the cradle's short bar between them; 60x32."""
    w = 60
    img, d = new(w, 32)
    d.ellipse(sc(3, 1, w - 3, 35), WHITE)
    d.ellipse(sc(13, 9.5, w - 13, 39.5), CLEAR)
    d.rectangle(sc(0, 17, w, 32), CLEAR)
    pad, pd = new(19, 11)
    pd.ellipse(sc(0, 0, 19, 11), WHITE)
    for cx, angle in ((10, -24), (w - 10, 24)):
        put(img, pad, cx, 17.5, angle)
    d.rounded_rectangle(sc(w / 2 - 10, 23, w / 2 + 10, 28), 2.5 * S, WHITE)
    save(img, out, "call_end")


def camcorder():
    """A rounded body and the lens's trapezoid on its right."""
    img, d = new(50, 50)
    d.rounded_rectangle(sc(3, 13, 35, 37), 4 * S, WHITE)
    d.polygon(sc(38, 22, 47, 14, 47, 36, 38, 28), WHITE)
    return img, d


def video(out):
    save(camcorder()[0], out, "call_video")


def video_off(out):
    """The camcorder struck through, a gap around the stroke as the mute one has."""
    img, d = camcorder()
    d.line(sc(6, 5, 44, 46), CLEAR, 8 * S)
    d.line(sc(6, 5, 44, 46), WHITE, 3 * S)
    save(img, out, "call_video_off")


def arrow(d, a, r):
    """An arrowhead at angle a degrees (clockwise from 3 o'clock) of the circle of radius r round
    (25, 25), pointing the way round (clockwise)."""
    import math
    t = math.radians(a)
    px, py = 25 + r * math.cos(t), 25 + r * math.sin(t)
    nx, ny = math.cos(t), math.sin(t)  # outwards
    tx, ty = -math.sin(t), math.cos(t)  # clockwise
    d.polygon(sc(px + tx * 7, py + ty * 7, px + nx * 5.5, py + ny * 5.5, px - nx * 5.5, py - ny * 5.5), WHITE)


def switch(out):
    """A camera with two arrows going round it."""
    img, d = new(50, 50)
    d.rounded_rectangle(sc(14, 19, 36, 34), 3 * S, WHITE)
    d.rectangle(sc(20, 15.5, 28, 20), WHITE)
    d.ellipse(sc(21, 22, 29, 30), CLEAR)
    d.ellipse(sc(23, 24, 27, 28), WHITE)
    d.arc(sc(4.5, 4.5, 45.5, 45.5), 205, 322, WHITE, 3 * S)
    d.arc(sc(4.5, 4.5, 45.5, 45.5), 25, 142, WHITE, 3 * S)
    arrow(d, 322, 21)
    arrow(d, 142, 21)
    save(img, out, "call_switch")


def drawer_shadow(out):
    """The shadow the page casts on the people drawer's sidebar, 12x8: the sidebar's last 12 px, clear
    at the left and black at 20 % at the right (the page's edge; the stock sidebar goes from #1e1e1e to
    about #181818 there); QML stretches it down the sidebar's height."""
    img = Image.new("RGBA", (12, 8))
    px = img.load()
    for x in range(12):
        a = round(255 * 0.2 * (x + 1) / 12)
        for y in range(8):
            px[x, y] = (0, 0, 0, a)
    img.save(f"{out}/call_drawer_shadow.png")


def brush(d, p0, p1, p2, w0, w1, color):
    """A stroke along the quadratic curve p0, p1, p2 (final px), its width going from w0 to w1: round
    dabs close together, the way a pen's thick and thin strokes are."""
    steps = 240
    for i in range(steps + 1):
        t = i / steps
        x = (1 - t) ** 2 * p0[0] + 2 * t * (1 - t) * p1[0] + t * t * p2[0]
        y = (1 - t) ** 2 * p0[1] + 2 * t * (1 - t) * p1[1] + t * t * p2[1]
        r = (w0 + (w1 - w0) * t) / 2
        d.ellipse(sc(x - r, y - r, x + r, y + r), color)


def hint_info(out):
    """The hint's badge, 96x96: a white disc with a serif italic lowercase i in #66661F. Drawn by
    hand (no font here): a slanted stem, the flag at its top left, the foot's hook to the right and
    the dot, each a brushstroke thick where a pen's would be and thin at its end."""
    img, d = new(96, 96)
    ink = hexa("#66661F")
    d.ellipse(sc(0, 0, 96, 96), WHITE)
    mid = lambda a, b: ((a[0] + b[0]) / 2, (a[1] + b[1]) / 2)
    top, foot = (52, 40), (44.5, 68)
    brush(d, top, mid(top, foot), foot, 7.6, 7.6, ink)  # the stem
    brush(d, (52.5, 40.5), (43, 41), (38.5, 49), 5.5, 2.2, ink)  # the flag
    brush(d, foot, (43.5, 73.5), (56, 65), 6.5, 2.4, ink)  # the foot
    d.ellipse(sc(47.2, 22, 58.8, 33.6), ink)  # the dot
    save(img, out, "call_hint_info")


def hint_chevrons(out):
    """Two white chevrons stacked, pointing up, as the lock screen's tutorial hints have; 110x110, so
    that turned a quarter it keeps its box."""
    img, d = new(110, 110)
    for top in (9, 49):
        pts = sc(8, top + 41, 55, top, 102, top + 41)
        d.line(pts, WHITE, 9 * S, joint="curve")
        for x, y in ((8, top + 41), (102, top + 41)):
            d.ellipse(sc(x - 4.5, y - 4.5, x + 4.5, y + 4.5), WHITE)
    save(img, out, "call_hint_chevrons")


def pip():
    """Our own preview: a screen's outline, and a small filled picture at its lower right."""
    img, d = new(50, 50)
    d.rounded_rectangle(sc(3, 9, 47, 41), 4 * S, outline=WHITE, width=3 * S)
    d.rounded_rectangle(sc(24, 24, 41, 36), 2 * S, WHITE)
    return img, d


def preview(out):
    save(pip()[0], out, "call_preview")


def preview_off(out):
    """The preview struck through, as the camcorder is."""
    img, d = pip()
    d.line(sc(6, 5, 44, 46), CLEAR, 8 * S)
    d.line(sc(6, 5, 44, 46), WHITE, 3 * S)
    save(img, out, "call_preview_off")


if __name__ == "__main__":
    for f in (background, fade, track, knob, button, message, speaker, mute, unmute, end, video, video_off, switch,
              drawer_shadow, hint_info, hint_chevrons, preview, preview_off):
        f(sys.argv[1])
