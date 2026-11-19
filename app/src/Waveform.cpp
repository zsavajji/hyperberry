#include "Waveform.h"

#include <QtCore/qmath.h>

#include <bb/ImageData>
#include <bb/PixelFormat>
#include <bb/cascades/Image>

namespace {
// The BLOB's 64 values; each bar covers two of them.
const int kValues = 64;
const int kBars = kValues / 2;
const int kBarW = 6;
const int kWidth = 381; // as with the 64 bars of 3 px: the 32 of 6 px end at 378
const int kHeight = 45;
// The bars' corners, a little round.
const int kRadius = 2;

// The values 0 to 100 of the 64th at `i`, the BLOB resampled as it comes.
int value(const QByteArray &values, int i)
{
    const int n = values.size();
    return qMin(100, n ? int(values.at(i * n / kValues)) : 0);
}

// A bar as a rounded rectangle: a pixel's alpha is kRadius + 0.5 less its distance from the rectangle inset by kRadius,
// from 0 to 1, so the corners are anti-aliased and the straight sides are solid.
void bar(unsigned char *pixels, int stride, int x0, int h)
{
    const int top = (kHeight - h) / 2;
    const float cx = x0 + kBarW / 2.0f;
    const float cy = top + h / 2.0f;
    for (int y = top; y < top + h; ++y) {
        const float qy = qMax(qAbs(y + 0.5f - cy) - (h / 2.0f - kRadius), 0.0f);
        for (int x = x0; x < x0 + kBarW; ++x) {
            const float qx = qMax(qAbs(x + 0.5f - cx) - (kBarW / 2.0f - kRadius), 0.0f);
            float a = kRadius + 0.5f - qSqrt(qx * qx + qy * qy);
            a = a < 0 ? 0 : a > 1 ? 1 : a;
            for (int c = 0; c < 4; ++c) // premultiplied white: every channel is the alpha
                pixels[y * stride + 4 * x + c] = (unsigned char)(a * 255 + 0.5f);
        }
    }
}
}

QVariant Waveform::image(const QByteArray &values)
{
    return image(values, kValues);
}

QVariant Waveform::image(const QByteArray &values, int bars)
{
    bb::ImageData img(bb::PixelFormat::RGBA_Premultiplied, kWidth, kHeight);
    unsigned char *pixels = img.pixels();
    const int stride = img.bytesPerLine();
    for (int y = 0; y < kHeight; ++y)
        for (int x = 0; x < kWidth * 4; ++x)
            pixels[y * stride + x] = 0;

    for (int i = 0; i < kBars && 2 * i < bars; ++i) {
        const int v = qMax(value(values, 2 * i), value(values, 2 * i + 1));
        bar(pixels, stride, i * 2 * kBarW, qMax(kBarW, v * kHeight / 100));
    }
    return QVariant::fromValue(bb::cascades::Image(img));
}
