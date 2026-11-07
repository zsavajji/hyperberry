#include "VideoWindow.h"

#include <bb/cascades/Application>
#include <bb/cascades/Window>
#include <errno.h>
#include <string.h>
#include <time.h>
#include <QDebug>
#include <QMutexLocker>

static double nowMs()
{
    timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

VideoWindow::VideoWindow()
    : m_ctx(0)
    , m_win(0)
    , m_format(0)
    , m_w(0)
    , m_h(0)
    , m_warned(false)
    , m_detileSum(0)
    , m_detileMax(0)
    , m_detileCount(0)
{
}

VideoWindow::~VideoWindow()
{
    destroy();
}

bool VideoWindow::build(int format, const QString &windowId, const QByteArray &group)
{
    const char *name = format == SCREEN_FORMAT_NV12 ? "NV12" : "RGBX8888";
    if (screen_create_window_type(&m_win, m_ctx, SCREEN_CHILD_WINDOW)) {
        qWarning("video window: screen_create_window_type: %s", strerror(errno));
        m_win = 0;
        return false;
    }
    // Write-only from the CPU, and native: the compositor reads the buffer in place.
    const int usage = SCREEN_USAGE_WRITE | SCREEN_USAGE_NATIVE;
    const int size[2] = { m_w, m_h };
    const int one = 1, below = -1, noFocus = SCREEN_SENSITIVITY_NO_FOCUS;
    const QByteArray id = windowId.toUtf8();
    if (screen_set_window_property_iv(m_win, SCREEN_PROPERTY_FORMAT, &format)
        || screen_set_window_property_iv(m_win, SCREEN_PROPERTY_USAGE, &usage)
        || screen_set_window_property_iv(m_win, SCREEN_PROPERTY_BUFFER_SIZE, size)
        || screen_create_window_buffers(m_win, 2)) {
        qWarning("video window: %s buffers %dx%d refused: %s", name, m_w, m_h, strerror(errno));
        screen_destroy_window(m_win);
        m_win = 0;
        return false;
    }
    // The three things the control needs to adopt the window (a child window, in the app's group,
    // carrying the control's id), the group joined only now that the control exists.
    if (screen_join_window_group(m_win, group.constData())
        || screen_set_window_property_cv(m_win, SCREEN_PROPERTY_ID_STRING, id.size() + 1, id.constData())) {
        qWarning("video window: joining the group or setting the id failed: %s", strerror(errno));
        screen_destroy_window(m_win);
        m_win = 0;
        return false;
    }
    // Under Cascades' hole (the SDK's way; touches don't reach it, none are wanted), and never
    // the keyboard's owner; the pitfall is a tap in the child taking keys from Cascades.
    screen_set_window_property_iv(m_win, SCREEN_PROPERTY_ZORDER, &below);
    screen_set_window_property_iv(m_win, SCREEN_PROPERTY_SENSITIVITY, &noFocus);
    screen_set_window_property_iv(m_win, SCREEN_PROPERTY_VISIBLE, &one);
    screen_flush_context(m_ctx, 0);
    qDebug("video window: %dx%d %s, id %s, group %s", m_w, m_h, name, id.constData(), group.constData());
    return true;
}

bool VideoWindow::create(const QString &windowId, int width, int height)
{
    destroy();
    m_w = width;
    m_h = height;
    m_warned = false;
    if (screen_create_context(&m_ctx, SCREEN_APPLICATION_CONTEXT)) {
        qWarning("video window: screen_create_context: %s", strerror(errno));
        m_ctx = 0;
        return false;
    }
    const bb::cascades::Window *main = bb::cascades::Application::instance()->mainWindow();
    if (!main) {
        qWarning("video window: no main window");
        destroy();
        return false;
    }
    const QByteArray group = main->groupId().toUtf8();
    if (build(SCREEN_FORMAT_NV12, windowId, group)) {
        m_format = SCREEN_FORMAT_NV12;
    } else if (build(SCREEN_FORMAT_RGBX8888, windowId, group)) {
        m_format = SCREEN_FORMAT_RGBX8888;
        m_scratch.resize(m_w * m_h * 3 / 2);
    } else {
        destroy();
        return false;
    }
    resetStats();
    return true;
}

void VideoWindow::destroy()
{
    if (m_win)
        screen_destroy_window(m_win);
    if (m_ctx)
        screen_destroy_context(m_ctx);
    m_win = 0;
    m_ctx = 0;
    m_format = 0;
}

void VideoWindow::detileStats(double *meanMs, double *maxMs) const
{
    QMutexLocker lock(&m_mu);
    *meanMs = m_detileCount ? m_detileSum / m_detileCount : 0;
    *maxMs = m_detileMax;
}

void VideoWindow::resetStats()
{
    QMutexLocker lock(&m_mu);
    m_detileSum = m_detileMax = 0;
    m_detileCount = 0;
}

void VideoWindow::toLinear(const VideoDecoder::Frame &f, unsigned char *y, int yStride, unsigned char *uv, int uvStride)
{
    if (f.colorFormat == VideoDecoder::TiledNV12) {
        if (!VideoDecoder::detile(f, y, yStride, uv, uvStride) && !m_warned) {
            m_warned = true;
            qWarning("video window: a %d byte buffer is too short for a %dx%d tiled frame", f.size, f.width, f.height);
        }
        return;
    }
    // Linear NV12: the chroma plane follows sliceHeight luma rows.
    const unsigned char *src = f.data;
    const unsigned char *srcUv = f.data + size_t(f.stride) * f.sliceHeight;
    for (int r = 0; r < f.height; r++)
        memcpy(y + size_t(r) * yStride, src + size_t(r) * f.stride, f.width);
    for (int r = 0; r < f.height / 2; r++)
        memcpy(uv + size_t(r) * uvStride, srcUv + size_t(r) * f.stride, f.width);
}

static inline unsigned char clamp8(int v)
{
    return v < 0 ? 0 : v > 255 ? 255 : (unsigned char)v;
}

void VideoWindow::frame(const VideoDecoder::Frame &f)
{
    if (!m_win)
        return;
    if (f.width != m_w || f.height != m_h || (f.colorFormat != VideoDecoder::TiledNV12 && f.colorFormat != VideoDecoder::LinearNV12)) {
        if (!m_warned)
            qWarning("video window: frame %dx%d color 0x%x does not fit the %dx%d window", f.width, f.height, f.colorFormat, m_w, m_h);
        m_warned = true;
        return;
    }
    screen_buffer_t buf = 0;
    if (screen_get_window_property_pv(m_win, SCREEN_PROPERTY_RENDER_BUFFERS, (void **)&buf) || !buf)
        return;
    unsigned char *base = 0;
    int stride = 0;
    screen_get_buffer_property_pv(buf, SCREEN_PROPERTY_POINTER, (void **)&base);
    screen_get_buffer_property_iv(buf, SCREEN_PROPERTY_STRIDE, &stride);
    if (!base || !stride)
        return;
    // Timed: the de-tiling (and the fallback's conversion), not the wait for a free buffer.
    double t0 = 0, ms = 0;
    if (m_format == SCREEN_FORMAT_NV12) {
        int offsets[3] = { 0, 0, 0 };
        screen_get_buffer_property_iv(buf, SCREEN_PROPERTY_PLANAR_OFFSETS, offsets);
        // The chroma plane is where Screen says; right behind the luma rows if it says nothing.
        t0 = nowMs();
        toLinear(f, base + offsets[0], stride, base + (offsets[1] > 0 ? offsets[1] : stride * m_h), stride);
        ms = nowMs() - t0;
    } else {
        // ponytail: plain C, BT.601, no NEON; the fallback only if Screen refuses NV12 windows.
        unsigned char *y = (unsigned char *)m_scratch.data(), *uv = y + m_w * m_h;
        t0 = nowMs();
        toLinear(f, y, m_w, uv, m_w);
        for (int r = 0; r < m_h; r++) {
            unsigned char *out = base + size_t(r) * stride;
            const unsigned char *yr = y + r * m_w, *uvr = uv + (r / 2) * m_w;
            for (int x = 0; x < m_w; x++) {
                const int u = uvr[x & ~1] - 128, v = uvr[x | 1] - 128, c = (yr[x] - 16) * 298;
                out[0] = clamp8((c + 516 * u + 128) >> 8);            // Screen's RGBX is B, G, R, X in memory
                out[1] = clamp8((c - 100 * u - 208 * v + 128) >> 8);
                out[2] = clamp8((c + 409 * v + 128) >> 8);
                out[3] = 255;
                out += 4;
            }
        }
        ms = nowMs() - t0;
    }
    screen_post_window(m_win, buf, 0, NULL, 0);
    QMutexLocker lock(&m_mu);
    m_detileSum += ms;
    m_detileCount++;
    if (ms > m_detileMax)
        m_detileMax = ms;
}
