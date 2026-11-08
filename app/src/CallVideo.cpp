#include "CallVideo.h"

#include "EngineLink.h"
#include "LinkIo.h"

#include <bb/cascades/Application>
#include <bb/cascades/Window>
#include <errno.h>
#include <signal.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>
#include <QCoreApplication>
#include <QDebug>
#include <QEvent>
#include <QMetaObject>
#include <QMutexLocker>

// The picture both ways. Each is a knob. A size or frame rate the decoder isn't told right costs a
// 270 ms output reconfiguration, so the same numbers go to the camera and to the decoder: the peer's
// (their camera is another Q10, or a phone whose encoder meowcaller negotiated down to this).
static const int Width = 640, Height = 480;
static const int Fps = 15;
static const int Bitrate = 400000;      // bit/s, CBR
static const int KeyframeSeconds = 2;   // our IDR interval; also the longest a lost frame shows
static const int MaxUnit = 256 * 1024;  // sizes the decoder's input buffers: a 640x480 IDR is a few tens of KB

// A record is a 16-byte little-endian header and the payload (docs/protocol2.md); a longer one ends the link.
static const int HeaderBytes = 16;
static const quint32 MaxRecord = 1 << 20;

static const int PollMs = 20;           // how often the camera thread looks at what is wanted
static const int RetryMs = 3000;        // a camera that would not open is not tried again sooner
static const int TickMs = 500;          // picture size check
static const int SummaryTicks = 20;     // one `call video:` line per 10 s

static void putLe(char *p, quint64 v, int bytes)
{
    for (int i = 0; i < bytes; i++)
        p[i] = char(v >> (8 * i));
}

static quint64 getLe(const char *p, int bytes)
{
    quint64 v = 0;
    for (int i = bytes - 1; i >= 0; i--)
        v = (v << 8) | (unsigned char)p[i];
    return v;
}

CallVideo::CallVideo(EngineLink *engine, QObject *parent)
    : QObject(parent)
    , m_engine(engine)
    , m_decoder(&m_window)
    , m_encoder(this)
    , m_reader(this, false)
    , m_camera(this, true)
    , m_timer(this)
    , m_fd(-1)
    , m_running(false)
    , m_picW(0)
    , m_picH(0)
    , m_ticks(0)
{
    m_front = 1;
    m_timer.setInterval(TickMs);
    connect(&m_timer, SIGNAL(timeout()), SLOT(tick()));
}

CallVideo::~CallVideo()
{
    stop();
}

void CallVideo::Worker::run()
{
    if (m_camera)
        m_owner->cameraLoop();
    else
        m_owner->readLoop();
}

void CallVideo::setSending(bool on)
{
    if (sending() == on)
        return;
    m_sending = on ? 1 : 0;
    emit sendingChanged();
}

void CallVideo::setFront(bool on)
{
    if (front() == on)
        return;
    m_front = on ? 1 : 0;
    emit frontChanged();
}

void CallVideo::setRunning(bool on)
{
    if (m_running == on)
        return;
    m_running = on;
    if (on) {
        m_ticks = 0;
        m_recordsIn = 0;
        m_feedRefused = 0;
        m_timer.start();
    } else {
        m_timer.stop();
    }
    emit runningChanged();
}

void CallVideo::start(const QString &callId, const QString &remoteWindowId, const QString &viewfinderWindowId)
{
    stop();
    signal(SIGPIPE, SIG_IGN); // a send to a closed link must fail, not kill the app
    const bb::cascades::Window *main = bb::cascades::Application::instance()->mainWindow();
    if (!main || !m_window.create(remoteWindowId, Width, Height, false, false)) {
        qWarning("call video: the window for the peer's picture could not be made");
        return;
    }
    m_group = main->groupId().toUtf8();
    m_viewfinderId = viewfinderWindowId.toUtf8();
    m_callId = callId;
    m_stopping = 0;
    m_linked = 0;
    m_keyframeWanted = 0;
    m_reader.start();
    m_camera.start();
}

void CallVideo::stop()
{
    m_stopping = 1;
    // The socket's shutdown wakes the reader, or a send in the encoder's callback.
    {
        QMutexLocker lock(&m_fdLock);
        if (m_fd >= 0)
            ::shutdown(m_fd, SHUT_RDWR);
    }
    m_reader.wait();  // it closes the link and the decoder before it ends
    m_camera.wait();  // and this one the camera
    m_decoder.close();
    m_window.destroy();
    // What the threads queued before they ended: a stale "running" must not arrive after this.
    QCoreApplication::removePostedEvents(this, QEvent::MetaCall);
    setRunning(false);
    if (m_picW || m_picH) {
        m_picW = m_picH = 0;
        emit pictureChanged();
    }
}

void CallVideo::tick()
{
    int w = 0, h = 0;
    m_window.pictureSize(&w, &h);
    if (w != m_picW || h != m_picH) {
        m_picW = w;
        m_picH = h;
        emit pictureChanged();
    }
    if (++m_ticks % SummaryTicks == 0)
        summarize();
}

void CallVideo::summarize()
{
    CameraEncoder::Stats e;
    VideoDecoder::Stats d;
    memset(&e, 0, sizeof e);
    memset(&d, 0, sizeof d);
    if (m_encoder.isRunning())
        e = m_encoder.stats();
    if (m_decoder.isOpen())
        d = m_decoder.stats();
    qDebug("call video: encoder %.1f fps %.0f kbit/s %d keyframes | link in %d records | decoder in %d out %d "
           "(%d refused), latency mean %.1f max %.1f ms | cpu %.0f%% of a core (whole process)",
           e.wallMs > 0 ? e.frames * 1000.0 / e.wallMs : 0, e.wallMs > 0 ? e.bytes * 8 / e.wallMs : 0, e.keyframes,
           int(m_recordsIn), d.framesIn, d.framesOut, int(m_feedRefused), d.latencyMeanMs, d.latencyMaxMs,
           d.wallMs > 0 ? 100 * d.cpuMs / d.wallMs : 0);
    if (m_encoder.isRunning())
        m_encoder.resetStats();
    if (m_decoder.isOpen())
        m_decoder.resetStats();
    m_recordsIn = 0;
    m_feedRefused = 0;
}

// Connects and says hello, as the audio link does. m_fd is set from the first moment so stop() can
// shut it down while we wait (the reply has a 3 s timeout besides).
bool CallVideo::openLink()
{
    const int port = m_engine->port();
    if (port <= 0) {
        qWarning("call video: the engine's port is unknown");
        return false;
    }
    const int fd = ::socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) {
        qWarning("call video: socket failed, errno %d", errno);
        return false;
    }
    {
        QMutexLocker lock(&m_fdLock);
        m_fd = fd;
    }
    if (m_stopping)
        return false;
    if (!LinkIo::connectLoopback(fd, port)) {
        qWarning("call video: connect failed, errno %d", errno);
        return false;
    }
    return LinkIo::hello(fd, m_engine->token(), "v", "video", m_callId, "call video");
}

void CallVideo::closeLink()
{
    {
        QMutexLocker lock(&m_fdLock);
        if (m_fd >= 0)
            ::shutdown(m_fd, SHUT_RDWR); // a send that is under way fails now, and lets go of m_sendLock
    }
    QMutexLocker send(&m_sendLock);
    m_linked = 0;
    QMutexLocker lock(&m_fdLock);
    if (m_fd >= 0)
        ::close(m_fd);
    m_fd = -1;
}

// The reader thread: opens the link and the decoder, then the engine's records to the decoder.
void CallVideo::readLoop()
{
    if (openLink()) {
        {
            QMutexLocker lock(&m_sendLock);
            m_linked = 1;
        }
        QMetaObject::invokeMethod(this, "setRunning", Qt::QueuedConnection, Q_ARG(bool, true));
        // Without a decoder our picture still goes out; the records are read and dropped.
        bool decoding = m_decoder.open(Width, Height, Fps, MaxUnit);
        if (!decoding)
            qWarning("call video: the hardware decoder did not open; no picture from the peer");
        char head[HeaderBytes];
        QByteArray payload;
        while (!m_stopping && LinkIo::recvAll(m_fd, head, HeaderBytes)) { // false: the engine closed it, or stop()
            const quint32 length = quint32(getLe(head, 4));
            if (length > MaxRecord) {
                qWarning("call video: a record of %u bytes; closing the link", unsigned(length));
                break;
            }
            payload.resize(int(length));
            if (length && !LinkIo::recvAll(m_fd, payload.data(), int(length)))
                break;
            m_recordsIn.ref();
            const int type = (unsigned char)head[4];
            if (type == 1) {
                // The turn is the frame's own, and takes effect for the pictures decoded after it
                // (the decoder holds a few frames, so a change shows a few pictures late: harmless).
                // WhatsApp counts its quarter turns anticlockwise, the window clockwise: turned as
                // sent, Android's front camera (1) was upside down and its rear one (3) too (Q10, 0.0.1.206).
                m_window.setRotation((4 - (unsigned char)head[5]) & 3);
                if (decoding && m_decoder.broken()) {
                    qWarning("call video: the decoder stalled; opening it again");
                    decoding = m_decoder.open(Width, Height, Fps, MaxUnit);
                }
                if (decoding && !m_decoder.feed(payload, qint64(getLe(head + 8, 8))))
                    m_feedRefused.ref(); // no free input buffer: this picture is lost, the next IDR mends it
            } else if (type == 2) {
                m_keyframeWanted = 1; // passed to the encoder by the camera thread, which owns its handle
            }
        }
    }
    closeLink();
    m_decoder.close();
    QMetaObject::invokeMethod(this, "setRunning", Qt::QueuedConnection, Q_ARG(bool, false));
}

// The camera thread: the camera is open while we send and the link is up, and only then. Opening it
// takes seconds, so it can't happen on the UI thread; the encoder's callbacks come on its own.
void CallVideo::cameraLoop()
{
    bool front = true;
    int cooldown = 0; // polls left before another try, after a failure
    while (!m_stopping) {
        const bool want = m_sending && m_linked;
        const bool wantFront = m_front;
        if (m_encoder.isRunning() && (!want || wantFront != front))
            m_encoder.stop();
        if (!m_encoder.isRunning() && want && cooldown == 0) {
            front = wantFront;
            if (!m_encoder.start(front, Width, Height, Fps, Bitrate, KeyframeSeconds, m_group, m_viewfinderId))
                cooldown = RetryMs / PollMs;
        }
        if (cooldown > 0)
            cooldown--;
        if (m_keyframeWanted.fetchAndStoreOrdered(0) && m_encoder.isRunning())
            m_encoder.requestKeyframe();
        usleep(PollMs * 1000);
    }
    m_encoder.stop(); // returns once no callback runs
}

// On the camera library's thread: one record, the encoder's unit as it is.
void CallVideo::accessUnit(const QByteArray &annexB, qint64 tsUs, bool keyframe, int orientation)
{
    if (!m_linked || quint32(annexB.size()) > MaxRecord)
        return;
    QByteArray record(HeaderBytes + annexB.size(), 0);
    char *p = record.data();
    putLe(p, quint32(annexB.size()), 4);
    p[4] = 1;
    p[5] = char(orientation);
    putLe(p + 6, keyframe ? 1 : 0, 2);
    putLe(p + 8, quint64(tsUs), 8);
    memcpy(p + HeaderBytes, annexB.constData(), annexB.size());
    QMutexLocker lock(&m_sendLock);
    if (!m_linked) // closeLink() got in first: the descriptor may be gone
        return;
    if (!LinkIo::sendAll(m_fd, record.constData(), record.size())) {
        // The reader sees the link end by itself; until then nothing more goes out.
        qWarning("call video: a send failed, errno %d; no more video out", errno);
        m_linked = 0;
    }
}
