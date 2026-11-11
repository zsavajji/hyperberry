#include "VideoTest.h"

#include <string.h>
#include <time.h>
#include <unistd.h>
#include <QCoreApplication>
#include <QDebug>
#include <QFile>

// The clip scripts/gen-test-video.sh makes: raw Annex-B, so the access units are cut here.
static const int ClipWidth = 640;
static const int ClipHeight = 480;
static const int ClipFps = 30;
static const char *ClipPath = "/assets/video/test640.264";
// Between one decoder's opening and the next one's.
static const int OpenGapMs = 300;

static double nowMs()
{
    timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1e3 + t.tv_nsec / 1e6;
}

VideoTest::VideoTest(QObject *parent)
    : QObject(parent)
    , m_timer(this)
    , m_playing(false)
    , m_count(2)
    , m_largest(0)
{
    for (int i = 0; i < MaxSlots; i++) {
        m_slot[i].owner = this;
        m_slot[i].index = i;
    }
    m_timer.setInterval(1000);
    connect(&m_timer, SIGNAL(timeout()), SLOT(refresh()));
}

VideoTest::~VideoTest()
{
    stop();
}

void VideoTest::setCount(int count)
{
    count = qBound(1, count, int(MaxSlots));
    if (count == m_count)
        return;
    m_count = count;
    emit optionsChanged();
    if (m_playing)
        start(m_windowIds); // a restart: the windows are never touched from two threads
}

void VideoTest::start(const QStringList &windowIds)
{
    stop();
    m_windowIds = windowIds;
    m_error.clear();
    if (windowIds.size() < m_count) {
        m_error = tr("A window id is missing for each decoder.");
        emit runningChanged();
        return;
    }
    QFile file(QCoreApplication::applicationDirPath() + ClipPath);
    if (!file.open(QIODevice::ReadOnly)) {
        qWarning("video test: cannot read %s", qPrintable(file.fileName()));
        m_error = tr("The test clip is missing.");
        emit runningChanged();
        return;
    }
    m_units = VideoDecoder::splitAccessUnits(file.readAll());
    m_largest = 0;
    foreach (const QByteArray &u, m_units)
        m_largest = qMax(m_largest, u.size());
    qDebug("video test: %d access units, the largest %d bytes, %d decoders", m_units.size(), m_largest, m_count);
    if (m_units.isEmpty()) {
        m_error = tr("The test clip has no pictures.");
        emit runningChanged();
        return;
    }
    m_stop = 0;
    for (int i = 0; i < m_count; i++) {
        m_slot[i].opened = 0;
        m_slot[i].failed = 0;
        if (!m_slot[i].window.create(windowIds[i], ClipWidth, ClipHeight, false, false)) {
            stop(); // the windows made so far
            m_error = tr("The video window %1 could not be made; see the log.").arg(i + 1);
            emit runningChanged();
            return;
        }
    }
    m_playing = true;
    for (int i = 0; i < m_count; i++)
        m_slot[i].start();
    refresh();
    m_timer.start();
    emit runningChanged();
}

void VideoTest::stop()
{
    m_stop = 1;
    m_timer.stop();
    for (int i = 0; i < MaxSlots; i++)
        m_slot[i].wait(); // play() closes the decoder before it returns
    for (int i = 0; i < MaxSlots; i++)
        m_slot[i].window.destroy();
    m_playing = false;
    emit runningChanged();
}

void VideoTest::refresh()
{
    QStringList lines;
    double cpu = 0;
    bool haveCpu = false;
    for (int i = 0; i < m_count; i++) {
        Slot &s = m_slot[i];
        if (s.failed) {
            lines << QString("%1: did not open").arg(i + 1);
        } else if (!s.opened) {
            lines << QString::fromUtf8("%1: opening\xE2\x80\xA6").arg(i + 1);
        } else {
            const VideoDecoder::Stats st = s.decoder.stats();
            const double fps = st.wallMs > 0 ? st.framesOut * 1000.0 / st.wallMs : 0;
            lines << QString("%1: %2 fps, in %3 out %4, lat %5/%6 ms, err %7").arg(i + 1).arg(fps, 0, 'f', 1)
                         .arg(st.framesIn).arg(st.framesOut).arg(st.latencyMeanMs, 0, 'f', 0)
                         .arg(st.latencyMaxMs, 0, 'f', 0).arg(st.errors);
            if (!haveCpu && st.wallMs > 0) {
                cpu = 100 * st.cpuMs / st.wallMs;
                haveCpu = true;
            }
        }
    }
    lines << QString("CPU %1 % of a core (whole process)").arg(cpu, 0, 'f', 0);
    m_report = lines.join("\n");
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

void VideoTest::summarize(Slot *s, int pass, double firstFrameMs)
{
    const VideoDecoder::Stats st = s->decoder.stats();
    double detileMean, detileMax;
    s->window.detileStats(&detileMean, &detileMax);
    qDebug("video test: slot %d of %d: pass %d: in %d out %d, %.1f fps, first frame %.0f ms, latency mean %.1f max %.1f ms, "
           "cpu %.0f%% of a core (whole process), de-tile %.2f ms (max %.2f, %s window), errors %d",
           s->index + 1, m_count, pass, st.framesIn, st.framesOut, st.wallMs > 0 ? st.framesOut * 1000.0 / st.wallMs : 0.0,
           firstFrameMs, st.latencyMeanMs, st.latencyMaxMs, st.wallMs > 0 ? 100 * st.cpuMs / st.wallMs : 0.0,
           detileMean, detileMax, s->window.isNv12() ? "NV12" : "RGBX", st.errors);
}

// A slot's thread: opens its decoder after its turn, then plays the clip in a loop until stop().
void VideoTest::play(Slot *s)
{
    if (!pace(nowMs() + s->index * OpenGapMs))
        return;
    if (!s->decoder.open(ClipWidth, ClipHeight, ClipFps, m_largest)) {
        qWarning("video test: slot %d of %d: the decoder did not open", s->index + 1, m_count);
        s->failed = 1;
        return;
    }
    s->opened = 1;
    s->window.resetStats();
    const qint64 framePeriodUs = 1000000 / ClipFps;
    // Pass 1's first frame: a later pass's would count from the decoder's opening.
    double firstFrame = -1;
    for (int pass = 1; !m_stop; pass++) {
        const double start = nowMs();
        for (int i = 0; i < m_units.size(); i++) {
            if (!pace(start + i * 1000.0 / ClipFps))
                break;
            // Timestamps go on rising through the passes; each pass starts on an IDR.
            s->decoder.feed(m_units.at(i), (qint64(pass - 1) * m_units.size() + i) * framePeriodUs);
        }
        if (m_stop)
            break;
        // Let the last pictures out before counting; the pass stops counting at that point.
        const double drained = nowMs() + 500;
        while (s->decoder.stats().framesOut < s->decoder.stats().framesIn && nowMs() < drained && pace(nowMs() + 5)) {
        }
        if (pass == 1)
            firstFrame = s->decoder.stats().firstFrameMs;
        summarize(s, pass, firstFrame);
        s->decoder.resetStats();
        s->window.resetStats();
    }
    s->decoder.close();
    s->opened = 0;
}
