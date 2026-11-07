#ifndef VIDEOWINDOW_H
#define VIDEOWINDOW_H

#include "VideoDecoder.h"

#include <QMutex>
#include <QString>
#include <screen/screen.h>

// A Screen child window the decoder's frames are drawn into, to be bound by a QML
// ForeignWindowControl with the same windowId (docs: .claude/skills/bb10-pitfalls, "ForeignWindowControl
// and Screen windows"). NV12 if Screen takes it, else RGBX8888 with a conversion. The sink de-tiles
// each frame straight into the window's buffer, on the decoder's output thread.
class VideoWindow : public VideoDecoder::Sink
{
public:
    VideoWindow();
    ~VideoWindow();

    // Sized to the video. UI thread only (it asks the Cascades main window for its group), and
    // after the ForeignWindowControl with this windowId exists. False after logging why.
    // rgb skips NV12 and draws RGBX; above puts the window in front of Cascades instead of under
    // its hole (the test's switches, for when nothing shows: 0.0.1.190).
    bool create(const QString &windowId, int width, int height, bool rgb, bool above);
    // Only once nothing calls frame() any more (the decoder closed).
    void destroy();
    bool isNv12() const { return m_format == SCREEN_FORMAT_NV12; }

    void frame(const VideoDecoder::Frame &frame);

    // The time frame() took to bring a frame to linear NV12 in the buffer (and to RGB, in the
    // fallback), since the last reset.
    void detileStats(double *meanMs, double *maxMs) const;
    void resetStats();

private:
    bool build(int format, const QString &windowId, const QByteArray &group, int z);
    void toLinear(const VideoDecoder::Frame &f, unsigned char *y, int yStride, unsigned char *uv, int uvStride);

    screen_context_t m_ctx;
    screen_window_t m_win;
    int m_format;
    int m_w, m_h;
    QByteArray m_scratch;      // the fallback's linear NV12
    bool m_warned;
    bool m_postLogged;
    mutable QMutex m_mu;
    double m_detileSum, m_detileMax;
    int m_detileCount;
};

#endif
