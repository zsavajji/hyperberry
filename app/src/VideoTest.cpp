#include "VideoTest.h"

#include <string.h>
#include <time.h>
#include <unistd.h>
#include <QCoreApplication>
#include <QDebug>
#include <QFile>
#include <QMetaObject>

// The clip scripts/gen-test-video.sh makes: raw Annex-B, so the access units are cut here.
static const int ClipWidth = 640;
static const int ClipHeight = 480;
static const int ClipFps = 30;
static const char *ClipPath = "/assets/video/test640.264";

static double nowMs()
{
    timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

VideoTest::VideoTest(QObject *parent)
    : QObject(parent)
    , m_decoder(&m_window)
    , m_player(this)
    , m_timer(this)
    , m_playing(false)
    , m_detileMean(0)
    , m_detileMax(0)
    , m_pass(0)
{
    memset(&m_stats, 0, sizeof m_stats);
    m_stats.firstFrameMs = -1;
    m_timer.setInterval(1000);
    connect(&m_timer, SIGNAL(timeout()), SLOT(refresh()));
}

VideoTest::~VideoTest()
{
    stop();
}

void VideoTest::start(const QString &windowId)
{
    stop();
    m_error.clear();
    m_pass = 0;
    memset(&m_stats, 0, sizeof m_stats);
    m_stats.firstFrameMs = -1;
    m_detileMean = m_detileMax = 0;
    m_stop = 0;
    if (!m_window.create(windowId, ClipWidth, ClipHeight)) {
        m_error = tr("The video window could not be made; see the log.");
        emit runningChanged();
        return;
    }
    m_playing = true;
    m_player.start();
    m_timer.start();
    emit runningChanged();
    emit statsChanged();
}

void VideoTest::stop()
{
    m_stop = 1;
    m_timer.stop();
    m_player.wait(); // play() closes the decoder before it returns
    m_window.destroy();
    m_playing = false;
    emit runningChanged();
}

void VideoTest::setError(const QString &text)
{
    m_error = text;
    emit runningChanged();
}

void VideoTest::ended()
{
    m_playing = false;
    m_timer.stop();
    emit runningChanged();
}

void VideoTest::refresh()
{
    if (!m_decoder.isOpen())
        return;
    m_stats = m_decoder.stats();
    m_window.detileStats(&m_detileMean, &m_detileMax);
    emit statsChanged();
}

// Sleeps to the given time; false if stop() was asked meanwhile.
bool VideoTest::pace(double dueMs)
{
    for (;;) {
        if (m_stop)
            return false;
        const double left = dueMs - nowMs();
        if (left <= 0)
            return true;
        usleep(left > 5 ? 5000 : useconds_t(left * 1000) + 1);
    }
}

void VideoTest::summarize(int pass)
{
    const VideoDecoder::Stats s = m_decoder.stats();
    double detileMean, detileMax;
    m_window.detileStats(&detileMean, &detileMax);
    qDebug("video test: pass %d: in %d out %d, %.1f fps, first frame %.0f ms, latency mean %.1f max %.1f ms, "
           "cpu %.0f%% of a core (whole process), de-tile %.2f ms (max %.2f, %s), errors %d",
           pass, s.framesIn, s.framesOut, s.wallMs > 0 ? s.framesOut * 1000.0 / s.wallMs : 0.0, s.firstFrameMs,
           s.latencyMeanMs, s.latencyMaxMs, s.wallMs > 0 ? 100 * s.cpuMs / s.wallMs : 0.0,
           detileMean, detileMax, m_window.isNv12() ? "NV12 window" : "RGBX window", s.errors);
}

void VideoTest::play()
{
    QFile file(QCoreApplication::applicationDirPath() + ClipPath);
    if (!file.open(QIODevice::ReadOnly)) {
        qWarning("video test: cannot read %s", qPrintable(file.fileName()));
        QMetaObject::invokeMethod(this, "setError", Qt::QueuedConnection, Q_ARG(QString, tr("The test clip is missing.")));
        return;
    }
    const QList<QByteArray> units = VideoDecoder::splitAccessUnits(file.readAll());
    int largest = 0;
    foreach (const QByteArray &u, units)
        largest = qMax(largest, u.size());
    qDebug("video test: %d access units, the largest %d bytes", units.size(), largest);
    if (units.isEmpty() || !m_decoder.open(ClipWidth, ClipHeight, ClipFps, largest)) {
        QMetaObject::invokeMethod(this, "setError", Qt::QueuedConnection,
                                  Q_ARG(QString, tr("The hardware decoder did not open; see the log.")));
        return;
    }
    m_window.resetStats();
    const qint64 framePeriodUs = 1000000 / ClipFps;
    for (int pass = 1; !m_stop; pass++) {
        const double start = nowMs();
        for (int i = 0; i < units.size(); i++) {
            if (!pace(start + i * 1000.0 / ClipFps))
                break;
            // Timestamps go on rising through the passes; each pass starts on an IDR.
            m_decoder.feed(units[i], (qint64(pass - 1) * units.size() + i) * framePeriodUs);
        }
        if (m_stop)
            break;
        // Let the last pictures out before counting; the pass stops counting at that point.
        const double drained = nowMs() + 500;
        while (m_decoder.stats().framesOut < m_decoder.stats().framesIn && nowMs() < drained && pace(nowMs() + 5)) {
        }
        summarize(pass);
        m_pass = pass;
        m_decoder.resetStats();
        m_window.resetStats();
    }
    m_decoder.close();
}
