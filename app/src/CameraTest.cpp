#include "CameraTest.h"

#include <bb/cascades/Application>
#include <bb/cascades/Window>
#include <string.h>
#include <unistd.h>
#include <QDebug>

static const int Bitrate = 500000;
static const int KeyframeSeconds = 2;
// Sizes the decoder's input buffers: a 640x480 keyframe at 500 kbit/s is a few tens of KB.
static const int MaxUnit = 128 * 1024;

CameraTest::CameraTest(QObject *parent)
    : QObject(parent)
    , m_decoder(&m_window)
    , m_encoder(this)
    , m_runner(this)
    , m_timer(this)
    , m_running(false)
    , m_front(true)
    , m_small(false)
    , m_fps(30)
    , m_ticks(0)
{
    memset(&m_enc, 0, sizeof m_enc);
    memset(&m_dec, 0, sizeof m_dec);
    m_timer.setInterval(1000);
    connect(&m_timer, SIGNAL(timeout()), SLOT(refresh()));
}

CameraTest::~CameraTest()
{
    stop();
}

QString CameraTest::modeText() const
{
    return QString("%1x%2 @ %3, %4").arg(width()).arg(height()).arg(m_fps).arg(m_front ? tr("front") : tr("rear"));
}

void CameraTest::start(const QString &videoWindowId, const QString &viewfinderWindowId)
{
    stop();
    m_videoId = videoWindowId;
    m_vfId = viewfinderWindowId;
    m_error.clear();
    m_stop = 0;
    m_feedFailed = 0;
    m_ticks = 0;
    memset(&m_enc, 0, sizeof m_enc);
    memset(&m_dec, 0, sizeof m_dec);
    const bb::cascades::Window *main = bb::cascades::Application::instance()->mainWindow();
    if (!main || !m_window.create(videoWindowId, width(), height(), false, false)) {
        m_error = tr("The video window could not be made; see the log.");
        emit runningChanged();
        return;
    }
    m_group = main->groupId().toUtf8();
    m_running = true;
    m_runner.start();
    m_timer.start();
    emit runningChanged();
    emit statsChanged();
}

void CameraTest::stop()
{
    m_stop = 1;
    m_timer.stop();
    m_runner.wait(); // work() closes the camera, then the decoder, before it returns
    m_window.destroy();
    m_running = false;
    emit runningChanged();
}

void CameraTest::restart()
{
    if (m_running)
        start(m_videoId, m_vfId); // never touch the window from two threads
}

void CameraTest::setFront(bool on)
{
    if (on == m_front)
        return;
    m_front = on;
    emit optionsChanged();
    restart();
}

void CameraTest::setSmall(bool on)
{
    if (on == m_small)
        return;
    m_small = on;
    emit optionsChanged();
    restart();
}

void CameraTest::setFps(int fps)
{
    if (fps == m_fps)
        return;
    m_fps = fps;
    emit optionsChanged();
    restart();
}

void CameraTest::setError(const QString &text)
{
    m_error = text;
    emit runningChanged();
}

void CameraTest::ended()
{
    m_running = false;
    m_timer.stop();
    emit runningChanged();
}

// On the camera library's thread. A unit the decoder has no free buffer for is dropped (counted),
// which a live call would do too: a late picture is worse than none.
void CameraTest::accessUnit(const QByteArray &annexB, qint64 tsUs, bool, int)
{
    if (m_decoder.isOpen() && !m_decoder.feed(annexB, tsUs))
        m_feedFailed.ref();
}

void CameraTest::work()
{
    if (!m_decoder.open(width(), height(), m_fps, MaxUnit)) {
        QMetaObject::invokeMethod(this, "setError", Qt::QueuedConnection,
                                  Q_ARG(QString, tr("The hardware decoder did not open; see the log.")));
        return;
    }
    m_window.resetStats();
    if (!m_encoder.start(m_front, width(), height(), m_fps, Bitrate, KeyframeSeconds, m_group, m_vfId.toUtf8())) {
        QMetaObject::invokeMethod(this, "setError", Qt::QueuedConnection,
                                  Q_ARG(QString, tr("The camera did not start; see the log.")));
        m_decoder.close();
        return;
    }
    m_decoder.resetStats();
    while (!m_stop)
        usleep(20000);
    m_encoder.stop(); // before the decoder closes: no callback may feed a closed decoder
    m_decoder.close();
}

void CameraTest::refresh()
{
    if (!m_decoder.isOpen() || !m_encoder.isRunning())
        return;
    m_enc = m_encoder.stats();
    m_dec = m_decoder.stats();
    emit statsChanged();
    if (++m_ticks % 10 == 0) {
        summarize();
        m_encoder.resetStats();
        m_decoder.resetStats();
        m_window.resetStats();
        m_feedFailed = 0;
    }
}

void CameraTest::summarize()
{
    double detileMean, detileMax;
    m_window.detileStats(&detileMean, &detileMax);
    qDebug("camera test: %s: encoder %.1f fps %.0f kbit/s, %d keyframes, frame mean %d max %d bytes, "
           "callback gap mean %.1f max %.1f ms, camera-to-callback mean %.1f max %.1f ms, camera dropped %d | "
           "decoder in %d out %d (%d refused), latency mean %.1f max %.1f ms, de-tile %.2f ms | "
           "cpu %.0f%% of a core (whole process), errors %d",
           qPrintable(modeText()), encFps(), encKbps(), keyframes(), meanFrameBytes(), maxFrameBytes(),
           m_enc.intervalMeanMs, m_enc.intervalMaxMs, m_enc.ageMeanMs, m_enc.ageMaxMs, m_enc.dropped,
           m_dec.framesIn, m_dec.framesOut, int(m_feedFailed), m_dec.latencyMeanMs, m_dec.latencyMaxMs,
           detileMean, cpuPercent(), m_dec.errors);
}
