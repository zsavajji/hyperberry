#include "CallVideo.h"

#include "EngineLink.h"
#include "LinkIo.h"
#include "Nv12Rotate.h"

#include <bb/cascades/Application>
#include <bb/cascades/ForeignWindowControl>
#include <bb/cascades/Window>
#include <errno.h>
#include <screen/screen.h>
#include <signal.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>
#include <QCoreApplication>
#include <QDebug>
#include <QElapsedTimer>
#include <QEvent>
#include <QMetaObject>
#include <QMutexLocker>

// The picture both ways. Each is a knob. A size or frame rate the decoder isn't told right costs a
// 270 ms output reconfiguration. Width and Height are the decoder's and the window's, and stay: the
// decoder follows the peer's size by itself (their camera is another Q10, or a phone whose encoder
// meowcaller negotiated down to 640x480). Our camera's size is the step's (Steps), at the same frame
// rate and keyframe interval.
static const int Width = 640, Height = 480;
static const int Fps = 15;
static const int KeyframeSeconds = 2;   // our IDR interval; also the longest a lost frame shows
static const int MaxUnit = 256 * 1024;  // sizes the decoder's input buffers: a 640x480 IDR is a few tens of KB

// A record is a 16-byte little-endian header and the payload (docs/protocol2.md); a longer one ends the link.
static const int HeaderBytes = 16;
static const quint32 MaxRecord = 1 << 20;

static const int PollMs = 20;           // how often the camera thread looks at what is wanted
static const int RetryMs = 3000;        // a camera that would not open is not tried again sooner
static const int TickMs = 500;          // picture size check

// Our camera follows the bandwidth the peer allows us (the engine's type-3 record, WhatsApp's RTCP PRST
// about every 5 s), in steps of size. floorBps: below it for StepDownMs, the camera goes down a step.
// enterBps: the allowance it needs, for StepUpMs, to come up into this step from the one below it.
// capBps: the most the camera is set to at this step. Settings' videoQuality caps the size: the step
// never has a lower index than it. The numbers are Tengram's (its Gate V3), where they follow Telegram's estimate.
struct Step
{
    int width, height;
    int capBps, floorBps, enterBps;
};
static const Step Steps[] = {
    { 1280, 720, 1000000, 700000, 850000 },
    { 640, 480, 600000, 250000, 400000 },
    { 320, 240, 300000, 0, 0 },
};
static const int StepCount = 3;
static const int StartStep = 1;         // the camera starts at 640x480 (or at the cap, if that is smaller)
static const int StepDownMs = 2000;     // the allowance below the floor this long
static const int StepUpMs = 8000;       // the allowance at the entry this long
static const int StepHoldMs = 8000;     // at most one change of size in this time
static const int MinBitrate = 100000;
static const int RateEveryMs = 2000;    // the camera's bit rate is changed at most this often,
static const int RatePercent = 15;      // and only when the allowance moved it this much
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
    , m_encoder(this)
    , m_reader(this, false)
    , m_camera(this, true)
    , m_timer(this)
    , m_fd(-1)
    , m_running(false)
    , m_wide(false)
    , m_cropped(false)
    , m_picW(0)
    , m_picH(0)
    , m_tileMask(0)
    , m_ticks(0)
{
    m_front = 1;
    m_quality = 1;
    m_target = 0;
    m_step = StartStep;
    m_cameraBps = 0;
    m_crop = 0;
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

void CallVideo::setQuality(int quality)
{
    m_quality = qBound(0, quality, 2);
}

// Queued from the camera thread each time a camera starts: the self-view's shape is the running step's,
// not the setting's (the step is below the cap while the allowance is low).
void CallVideo::setWide(bool on)
{
    if (m_wide == on)
        return;
    m_wide = on;
    emit wideChanged();
}

void CallVideo::setCrop(bool on)
{
    m_crop = on ? 1 : 0;
    if (m_cropped == on)
        return;
    m_cropped = on;
    emit croppedChanged();
}

void CallVideo::setRunning(bool on)
{
    if (m_running == on)
        return;
    m_running = on;
    if (on) {
        m_ticks = 0;
        m_recordsIn = 0;
        for (int i = 0; i < Tiles; i++)
            m_tile[i].refused = 0;
        m_timer.start();
    } else {
        m_timer.stop();
    }
    emit runningChanged();
}

void CallVideo::start(const QString &callId, const QStringList &remoteWindowIds, const QString &viewfinderWindowId)
{
    stop();
    signal(SIGPIPE, SIG_IGN); // a send to a closed link must fail, not kill the app
    const bb::cascades::Window *main = bb::cascades::Application::instance()->mainWindow();
    // Tile 0 is the 1:1 call's picture: without its window there is no video. A window of the
    // others that can't be made leaves that tile without a picture, and its records are dropped.
    for (int i = 0; i < Tiles; i++) {
        Tile &t = m_tile[i];
        t.state = Tile::Idle;
        t.hasWindow = main && i < remoteWindowIds.size() && t.window.create(remoteWindowIds.at(i), Width, Height, false, false);
        if (!t.hasWindow && i > 0)
            qWarning("call video: the window for tile %d could not be made", i);
    }
    if (!m_tile[0].hasWindow) {
        qWarning("call video: the window for the peer's picture could not be made");
        stop(); // the windows of the other tiles
        return;
    }
    m_group = main->groupId().toUtf8();
    m_viewfinderId = viewfinderWindowId.toUtf8();
    m_callId = callId;
    m_stopping = 0;
    m_linked = 0;
    m_keyframeWanted = 0;
    m_target = 0; // none yet in this call
    setWide(false); // a call's camera starts at 640x480 or 320x240, never 16:9
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
    m_reader.wait();  // it closes the link and the decoders before it ends
    m_camera.wait();  // and this one the camera
    for (int i = 0; i < Tiles; i++) {
        m_tile[i].decoder.close();
        m_tile[i].window.destroy();
        m_tile[i].hasWindow = false;
    }
    // What the threads queued before they ended: a stale "running" must not arrive after this.
    QCoreApplication::removePostedEvents(this, QEvent::MetaCall);
    setRunning(false);
    if (m_picW || m_picH || m_tileMask) {
        m_picW = m_picH = m_tileMask = 0;
        emit pictureChanged();
    }
}

void CallVideo::logWindows(const QString &why)
{
    for (int i = 0; i < Tiles; i++) {
        if (m_tile[i].hasWindow)
            m_tile[i].window.logPosition(qPrintable(QString("tile %1, %2").arg(i).arg(why)));
    }
}

// The camera library made the viewfinder window as big as the video (1280x720, say); the control
// shows all of it, squeezed into the control's shape, unless the source viewport is the centre part
// with the control's aspect (9:16), which is what the peer sees too.
void CallVideo::cropFinder(QObject *finder)
{
    bb::cascades::ForeignWindowControl *control = qobject_cast<bb::cascades::ForeignWindowControl *>(finder);
    if (!m_cropped || !control)
        return;
    screen_window_t win = control->windowHandle();
    if (!win)
        return;
    int buffer[2], pos[2], size[2];
    screen_context_t context = 0;
    if (screen_get_window_property_iv(win, SCREEN_PROPERTY_BUFFER_SIZE, buffer)) {
        qWarning("call video: finder buffer size unknown: %s", strerror(errno));
        return;
    }
    centreCrop(buffer[0], buffer[1], 9, 16, &pos[0], &pos[1], &size[0], &size[1]);
    if (screen_set_window_property_iv(win, SCREEN_PROPERTY_SOURCE_POSITION, pos)
        || screen_set_window_property_iv(win, SCREEN_PROPERTY_SOURCE_SIZE, size)
        || screen_get_window_property_pv(win, SCREEN_PROPERTY_CONTEXT, reinterpret_cast<void **>(&context))
        || screen_flush_context(context, 0)) {
        qWarning("call video: finder viewport %dx%d at %d,%d refused: %s", size[0], size[1], pos[0], pos[1], strerror(errno));
        return;
    }
    qDebug("call video: finder shows %dx%d at %d,%d of %dx%d", size[0], size[1], pos[0], pos[1], buffer[0], buffer[1]);
}

void CallVideo::setTileViewSize(int tile, int w, int h)
{
    if (tile >= 0 && tile < Tiles)
        m_tile[tile].window.setViewSize(w, h);
}

void CallVideo::tick()
{
    int w = 0, h = 0, mask = 0;
    for (int i = 0; i < Tiles; i++) {
        int tw = 0, th = 0;
        m_tile[i].window.pictureSize(&tw, &th);
        if (tw > 0)
            mask |= 1 << i;
        if (i == 0) {
            w = tw;
            h = th;
        }
    }
    if (w != m_picW || h != m_picH || mask != m_tileMask) {
        m_picW = w;
        m_picH = h;
        m_tileMask = mask;
        emit pictureChanged();
    }
    if (++m_ticks % SummaryTicks == 0)
        summarize();
}

// One line per open decoder, as many as there are tiles with a picture coming in; tile 0's always.
void CallVideo::summarize()
{
    CameraEncoder::Stats e;
    memset(&e, 0, sizeof e);
    if (m_encoder.isRunning())
        e = m_encoder.stats();
    for (int i = 0; i < Tiles; i++) {
        VideoDecoder &decoder = m_tile[i].decoder;
        if (i > 0 && !decoder.isOpen())
            continue;
        VideoDecoder::Stats d;
        memset(&d, 0, sizeof d);
        if (decoder.isOpen()) {
            d = decoder.stats();
            decoder.resetStats();
        }
        qDebug("call video: encoder %.1f fps %.0f kbit/s %d keyframes (step %d, target %d kbit/s, camera set %d kbit/s) "
               "| link in %d records | tile %d decoder in %d out %d "
               "(%d refused), latency mean %.1f max %.1f ms | cpu %.0f%% of a core (whole process)",
               e.wallMs > 0 ? e.frames * 1000.0 / e.wallMs : 0, e.wallMs > 0 ? e.bytes * 8 / e.wallMs : 0, e.keyframes,
               int(m_step), int(m_target) / 1000, int(m_cameraBps) / 1000, int(m_recordsIn), i, d.framesIn, d.framesOut,
               int(m_tile[i].refused), d.latencyMeanMs, d.latencyMaxMs,
               d.wallMs > 0 ? 100 * d.cpuMs / d.wallMs : 0);
        m_tile[i].refused = 0;
    }
    if (m_encoder.isRunning())
        m_encoder.resetStats();
    m_recordsIn = 0;
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

// Opens a tile's decoder on the reader thread; false after logging why when it won't (the tile's
// records are then dropped, our picture still goes out). midStream: the stream is already running,
// so the engine is asked for the tile's next keyframe (flags' high byte = the tile, no payload).
bool CallVideo::openTile(int tile, bool midStream)
{
    Tile &t = m_tile[tile];
    if (!t.decoder.open(Width, Height, Fps, MaxUnit)) {
        qWarning("call video: the hardware decoder did not open for tile %d; no picture from there", tile);
        t.state = Tile::Dropped;
        return false;
    }
    t.state = Tile::Decoding;
    if (midStream)
        sendRecord(2, 0, tile << 8, 0, QByteArray());
    return true;
}

// The reader thread: opens the link and tile 0's decoder, then the engine's records to the decoders.
void CallVideo::readLoop()
{
    if (openLink()) {
        {
            QMutexLocker lock(&m_sendLock);
            m_linked = 1;
        }
        QMetaObject::invokeMethod(this, "setRunning", Qt::QueuedConnection, Q_ARG(bool, true));
        openTile(0, false);
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
                const int tile = int(getLe(head + 6, 2) >> 8);
                if (tile >= Tiles || !m_tile[tile].hasWindow)
                    continue;
                Tile &t = m_tile[tile];
                // The turn is the frame's own, and takes effect for the pictures decoded after it
                // (the decoder holds a few frames, so a change shows a few pictures late: harmless).
                // WhatsApp counts its quarter turns anticlockwise, the window clockwise: turned as
                // sent, Android's front camera (1) was upside down and its rear one (3) too (Q10, 0.0.1.206).
                t.window.setRotation((4 - (unsigned char)head[5]) & 3);
                if (t.state == Tile::Idle) {
                    openTile(tile, true); // the first record of a tile after 0
                } else if (t.state == Tile::Decoding && t.decoder.broken()) {
                    qWarning("call video: the decoder of tile %d stalled; opening it again", tile);
                    openTile(tile, true);
                }
                if (t.state == Tile::Decoding && !t.decoder.feed(payload, qint64(getLe(head + 8, 8))))
                    t.refused.ref(); // no free input buffer: this picture is lost, the next IDR mends it
            } else if (type == 2) {
                m_keyframeWanted = 1; // passed to the encoder by the camera thread, which owns its handle
            } else if (type == 3 && length >= 4) {
                // The peer's allowance for our video (u32, bit/s), for the camera thread. 0 means "none yet"
                // there, so an allowance of 0 is 1: below every floor.
                const quint32 bps = quint32(getLe(payload.constData(), 4));
                m_target = int(qBound<quint32>(1, bps, 100000000));
            }
        }
    }
    closeLink();
    for (int i = 0; i < Tiles; i++)
        m_tile[i].decoder.close();
    QMetaObject::invokeMethod(this, "setRunning", Qt::QueuedConnection, Q_ARG(bool, false));
}

// The bit rate the camera is set to at a step: the allowance, capped by the step; before any, 2/3 of the cap.
static int bitrateFor(int step, int target)
{
    const int cap = Steps[step].capBps;
    if (target <= 0)
        return cap * 2 / 3;
    return qMax(MinBitrate, qMin(target, cap));
}

// The camera thread: the camera is open while we send and the link is up, and only then. Opening it
// takes seconds, so it can't happen on the UI thread; the encoder's callbacks come on its own.
// It also chooses the step (the size) and the bit rate from the peer's allowance: the step goes down
// when the allowance stays below its floor and up when it stays at the next step's entry, never above
// the cap (m_quality; a cap that is now below the step brings it down at once). A step change restarts
// the camera, as the front/rear switch and the crop do. The bit rate is applied at each start and,
// while encoding, when the allowance has moved it by RatePercent, if the phone takes that
// (CameraEncoder::setBitrate).
void CallVideo::cameraLoop()
{
    QElapsedTimer clock;
    clock.start();
    bool front = true;
    bool crop = false;                  // the crop the running camera was started with
    int step = qMax(StartStep, int(m_quality)); // the step the camera runs at, or ran at last
    int startedStep = step;             // the step of the last start: another one is a change of size
    int cameraBps = 0;                  // what the camera is set to
    bool midEncodeRefused = false;      // the phone won't take a bit rate while encoding: not tried again this call
    qint64 lowSince = -1, highSince = -1; // since when the allowance is below the floor / at the entry; -1: it isn't
    qint64 changedAt = -StepHoldMs, rateAt = 0;
    int cooldown = 0; // polls left before another try, after a failure
    while (!m_stopping) {
        const qint64 now = clock.elapsed();
        const bool want = m_sending && m_linked;
        const bool wantFront = m_front;
        const bool wantCrop = m_crop != 0;
        const int target = m_target;    // 0: none yet
        const int cap = m_quality;
        int wantStep = step;
        if (m_encoder.isRunning() && target > 0) {
            const bool low = step < StepCount - 1 && target < Steps[step].floorBps;
            const bool high = step > cap && target >= Steps[step - 1].enterBps;
            lowSince = low ? (lowSince < 0 ? now : lowSince) : -1;
            highSince = high ? (highSince < 0 ? now : highSince) : -1;
            if (now - changedAt >= StepHoldMs) {
                if (lowSince >= 0 && now - lowSince >= StepDownMs)
                    wantStep = step + 1;
                else if (highSince >= 0 && now - highSince >= StepUpMs)
                    wantStep = step - 1;
            }
        } else {
            lowSince = highSince = -1;
        }
        wantStep = qMax(wantStep, cap);
        if (m_encoder.isRunning() && (!want || wantFront != front || wantCrop != crop || wantStep != step)) {
            if (wantStep != step)
                qDebug("call video: step %d -> %d (%dx%d), target %d kbit/s", step, wantStep, Steps[wantStep].width,
                       Steps[wantStep].height, target / 1000);
            m_encoder.stop();
        }
        if (!m_encoder.isRunning() && want && cooldown == 0) {
            front = wantFront;
            crop = wantCrop;
            step = wantStep;
            const int bps = bitrateFor(step, target);
            if (m_encoder.start(front, Steps[step].width, Steps[step].height, Fps, bps, KeyframeSeconds, m_group,
                                m_viewfinderId, crop)) {
                cameraBps = bps;
                m_cameraBps = bps;
                m_step = step;
                QMetaObject::invokeMethod(this, "setWide", Qt::QueuedConnection,
                                          Q_ARG(bool, Steps[step].width * 3 != Steps[step].height * 4));
                rateAt = clock.elapsed(); // the start took seconds
                if (step != startedStep) {
                    changedAt = rateAt;
                    startedStep = step;
                }
            } else {
                cooldown = RetryMs / PollMs;
            }
        }
        if (cooldown > 0)
            cooldown--;
        if (m_encoder.isRunning() && !midEncodeRefused && now - rateAt >= RateEveryMs) {
            const int bps = bitrateFor(step, target);
            if (qAbs(bps - cameraBps) * 100 >= RatePercent * cameraBps) {
                rateAt = now;
                if (m_encoder.setBitrate(bps)) {
                    cameraBps = bps;
                    m_cameraBps = bps;
                } else {
                    midEncodeRefused = true;
                    qWarning("call video: the camera won't change its bit rate while encoding; it follows the allowance "
                             "at the next start");
                }
            }
        }
        if (m_keyframeWanted.fetchAndStoreOrdered(0) && m_encoder.isRunning())
            m_encoder.requestKeyframe();
        usleep(PollMs * 1000);
    }
    m_encoder.stop(); // returns once no callback runs
}

// On the camera library's thread: one record, the encoder's unit as it is.
void CallVideo::accessUnit(const QByteArray &annexB, qint64 tsUs, bool keyframe, int orientation)
{
    sendRecord(1, orientation, keyframe ? 1 : 0, tsUs, annexB);
}

// One record to the engine, from the camera library's thread or the reader's.
void CallVideo::sendRecord(int type, int orientation, int flags, qint64 tsUs, const QByteArray &payload)
{
    if (!m_linked || quint32(payload.size()) > MaxRecord)
        return;
    QByteArray record(HeaderBytes + payload.size(), 0);
    char *p = record.data();
    putLe(p, quint32(payload.size()), 4);
    p[4] = char(type);
    p[5] = char(orientation);
    putLe(p + 6, flags, 2);
    putLe(p + 8, quint64(tsUs), 8);
    memcpy(p + HeaderBytes, payload.constData(), payload.size());
    QMutexLocker lock(&m_sendLock);
    if (!m_linked) // closeLink() got in first: the descriptor may be gone
        return;
    if (!LinkIo::sendAll(m_fd, record.constData(), record.size())) {
        // The reader sees the link end by itself; until then nothing more goes out.
        qWarning("call video: a send failed, errno %d; no more video out", errno);
        m_linked = 0;
    }
}
