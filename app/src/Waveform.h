#ifndef WAVEFORM_H
#define WAVEFORM_H

#include <QByteArray>
#include <QVariant>

// A voice note's waveform (step 4.3b), drawn into an image for ImageView as QrImage draws its code.
namespace Waveform {
// 32 bars, 6 px wide with a 6 px gap and slightly rounded corners (2 px, anti-aliased), 45 px tall (the Q10's 5 du) in
// 381 px, white on transparent so QML tints it with filterColor. The BLOB is 64 values of 0 to 100; another length is
// resampled, an empty one gives all-minimum bars. Each bar is the higher of the two 64ths it covers.
QVariant image(const QByteArray &values);
// Only the bars whose first 64th is among the first `bars` 64ths, at their places: the played part, laid over the grey
// whole.
QVariant image(const QByteArray &values, int bars);
}

#endif
