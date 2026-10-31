"""Draws the chat list's small marks into app/assets/images/: ticks, clock, mute, pin and mic,
the action bar's record icon, and the voice bubble's play, pause and retry discs.

They're white on transparent, so QML tints them with ImageView.filterColor (grey, or the
primary colour for read). Each is drawn at 8x and scaled down to its size on the Q10: 3 du,
27 px tall (PLAN.md §7). Run by hand when a mark changes, in the image fetch-emoji.sh builds:
    docker run --rm -v "$PWD:/src" hyperberry-font python -I /src/scripts/gen-icons.py /src/app/assets/images
"""
import sys

from PIL import Image, ImageDraw

S = 8  # supersampling
H = 27  # 3 du on the Q10


def canvas(w):
    img = Image.new("RGBA", (w * S, H * S), (0, 0, 0, 0))
    return img, ImageDraw.Draw(img)


def pts(*xy):
    return [(x * H * S, y * H * S) for x, y in xy]


def save(img, out, name):
    img.resize((img.width // S, img.height // S), Image.LANCZOS).save(f"{out}/{name}.png")


def main(out):
    white, w = (255, 255, 255, 255), round(0.11 * H * S)

    img, d = canvas(H)
    d.line(pts((0.14, 0.52), (0.38, 0.76), (0.86, 0.26)), white, w, joint="curve")
    save(img, out, "tick")

    # The second tick's short stroke starts past the first tick's long one, as WhatsApp draws it.
    img, d = canvas(round(H * 1.3))
    d.line(pts((0.10, 0.52), (0.34, 0.76), (0.82, 0.26)), white, w, joint="curve")
    d.line(pts((0.54, 0.66), (0.64, 0.76), (1.12, 0.26)), white, w, joint="curve")
    save(img, out, "ticks")

    img, d = canvas(H)
    d.ellipse(pts((0.12, 0.12), (0.88, 0.88)), outline=white, width=round(0.09 * H * S))
    d.line(pts((0.5, 0.26), (0.5, 0.52), (0.68, 0.62)), white, round(0.08 * H * S), joint="curve")
    save(img, out, "clock")

    # A speaker with a cross beside it.
    img, d = canvas(H)
    d.polygon(pts((0.06, 0.38), (0.22, 0.38), (0.46, 0.16), (0.46, 0.84), (0.22, 0.62), (0.06, 0.62)), white)
    x = round(0.08 * H * S)
    d.line(pts((0.6, 0.36), (0.9, 0.64)), white, x)
    d.line(pts((0.6, 0.64), (0.9, 0.36)), white, x)
    save(img, out, "mute")

    # A pushpin, drawn upright and turned 45 degrees.
    img, d = canvas(H)
    d.rectangle(pts((0.36, 0.08), (0.64, 0.16)), white)
    d.polygon(pts((0.40, 0.16), (0.60, 0.16), (0.62, 0.44), (0.76, 0.58), (0.24, 0.58), (0.38, 0.44)), white)
    d.line(pts((0.5, 0.58), (0.5, 0.94)), white, round(0.07 * H * S))
    save(img.rotate(-45, Image.BICUBIC), out, "pin")

    # A microphone: capsule, cradle and stand.
    img, d = canvas(H)
    d.rounded_rectangle(pts((0.36, 0.06), (0.64, 0.56)), round(0.14 * H * S), white)
    d.arc(pts((0.24, 0.30), (0.76, 0.72)), 0, 180, white, round(0.07 * H * S))
    d.line(pts((0.5, 0.72), (0.5, 0.92)), white, round(0.07 * H * S))
    d.line(pts((0.36, 0.92), (0.64, 0.92)), white, round(0.07 * H * S))
    save(img, out, "mic")

    # The action bar's record icon: the same microphone, 60% of an 81 px box (the system's action
    # icons), white on transparent.
    B = 81
    img = Image.new("RGBA", (B * S, B * S), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    m = lambda *xy: [((0.2 + x * 0.6) * B * S, (0.2 + y * 0.6) * B * S) for x, y in xy]
    d.rounded_rectangle(m((0.36, 0.0), (0.64, 0.58)), round(0.14 * 0.6 * B * S), white)
    d.arc(m((0.22, 0.30), (0.78, 0.76)), 0, 180, white, round(0.07 * 0.6 * B * S))
    d.line(m((0.5, 0.76), (0.5, 1.0)), white, round(0.07 * 0.6 * B * S))
    d.line(m((0.34, 1.0), (0.66, 1.0)), white, round(0.07 * 0.6 * B * S))
    img.resize((B, B), Image.LANCZOS).save(f"{out}/record.png")

    # The chat list's ⋮ Search, Top and Bottom, in the same box: /usr/share/icons has none of
    # them, and an action with no icon shows a cog (Q10, 2026-10-31).
    w = round(0.09 * 0.6 * B * S)
    for name in ("search", "top", "bottom"):
        img = Image.new("RGBA", (B * S, B * S), (0, 0, 0, 0))
        d = ImageDraw.Draw(img)
        if name == "search":
            d.ellipse(m((0.0, 0.0), (0.66, 0.66)), None, white, w)
            d.line(m((0.58, 0.58), (1.0, 1.0)), white, round(1.4 * w))
        else:
            bar, tip, tail = (0.04, 0.16, 0.96) if name == "top" else (0.96, 0.84, 0.04)
            d.line(m((0.1, bar), (0.9, bar)), white, w)
            d.line(m((0.5, tip), (0.5, tail)), white, w)
            wing = 0.3 if name == "top" else -0.3
            d.line(m((0.5, tip), (0.22, tip + wing)), white, w)
            d.line(m((0.5, tip), (0.78, tip + wing)), white, w)
        img.resize((B, B), Image.LANCZOS).save(f"{out}/{name}.png")

    # The bubble's buttons: a white disc 72 px across (8 du), a dark glyph. Not tinted.
    dark = (0x1a, 0x1a, 0x1a, 255)
    for name in ("play", "pause", "retry"):
        D = 72
        img = Image.new("RGBA", (D * S, D * S), (0, 0, 0, 0))
        d = ImageDraw.Draw(img)
        d.ellipse((0, 0, D * S - 1, D * S - 1), white)
        p = lambda *xy: [(x * D * S, y * D * S) for x, y in xy]
        if name == "play":
            d.polygon(p((0.40, 0.30), (0.40, 0.70), (0.72, 0.50)), dark)
        elif name == "pause":
            d.rectangle(p((0.34, 0.30), (0.46, 0.70)), dark)
            d.rectangle(p((0.54, 0.30), (0.66, 0.70)), dark)
        else:
            d.arc(p((0.28, 0.28), (0.72, 0.72)), 40, 330, dark, round(0.08 * D * S))
            d.polygon(p((0.70, 0.22), (0.76, 0.46), (0.52, 0.40)), dark)
        img.resize((D, D), Image.LANCZOS).save(f"{out}/{name}.png")


if __name__ == "__main__":
    main(sys.argv[1])
