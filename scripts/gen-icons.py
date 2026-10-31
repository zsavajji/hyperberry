"""Draws the chat list's small marks into app/assets/images/: ticks, clock, mute and pin.

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


if __name__ == "__main__":
    main(sys.argv[1])
