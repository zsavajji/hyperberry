#include "StickerPlayer.h"

#include "Settings.h"
#include "Sticker.h"

#include <bb/ImageData>
#include <bb/PixelFormat>
#include <bb/cascades/Application>
#include <QCoreApplication>
#include <QElapsedTimer>
#include <QFile>
#include <QMetaObject>
#include <QThread>
#include <QTimer>
#include <rlottie_capi.h>
#include <string.h>
#include <webp/demux.h>

using Sticker::kStickerPx;

// The thread's side: holds the loaded sticker and draws a frame at each tick of its timer. Everything but the signals and
// the slots runs on the worker's thread.
class StickerWorker : public QObject
{
    Q_OBJECT
public:
    StickerWorker(QAtomicInt *inFlight, QAtomicInt *latest)
        : m_inFlight(inFlight), m_latest(latest), m_animation(0), m_decoder(0), m_width(0), m_height(0), m_token(0),
          m_frame(0), m_frames(0), m_step(1), m_stamp(0), m_loop(false)
    {
        // A child, so it moves to the thread with the worker. A frame says when the next comes, so it fires once.
        m_timer = new QTimer(this);
        m_timer->setSingleShot(true);
        connect(m_timer, SIGNAL(timeout()), this, SLOT(tick()));
    }

    ~StickerWorker() { unload(); }

public slots:
    // Plays the sticker at path from its first frame. The one of the last path stays loaded, so a tap that plays the same
    // sticker again parses nothing.
    void start(int token, const QString &path, bool loop)
    {
        m_timer->stop();
        // Replaced or stopped before the worker got to it: parsing it would be wasted (a fling past ten stickers parsed all ten).
        if (token != int(*m_latest))
            return;
        m_token = token;
        if (path != m_path) {
            unload();
            if (!load(path)) {
                unload();
                emit finished(token);
                return;
            }
            m_path = path;
        }
        m_loop = loop;
        rewind();
        m_timer->start(kMinMs);
    }

    // Stops drawing; the sticker stays loaded.
    void halt() { m_timer->stop(); }

signals:
    // A frame as bytes R, G, B, A, premultiplied; token is the start()'s.
    void frameReady(int token, const QImage &frame);
    // A failed load, or the last frame of a sticker that plays once.
    void finished(int token);

private slots:
    void tick()
    {
        QElapsedTimer spent;
        spent.start();
        int delay = kMinMs;
        QImage frame;
        // The UI hasn't taken the last frame yet: draw nothing (a WebP still has to be decoded: each frame builds on the
        // last), but go on, or the animation would slow down with it.
        const bool draw = !int(*m_inFlight);
        bool last;
        if (m_animation) {
            if (draw) {
                lottie_animation_render(m_animation, m_frame, reinterpret_cast<uint32_t *>(m_canvas.bits()), kStickerPx,
                                        kStickerPx, m_canvas.bytesPerLine());
                frame = m_canvas;
            }
            m_frame += m_step;
            last = m_frame >= m_frames;
        } else {
            uint8_t *pixels = 0; // the decoder's, overwritten by the next frame
            int stamp = 0;       // when the frame ends, in ms from the start
            if (!WebPAnimDecoderGetNext(m_decoder, &pixels, &stamp)) {
                emit finished(m_token);
                return;
            }
            if (draw) {
                frame = QImage(pixels, m_width, m_height, m_width * 4, QImage::Format_ARGB32_Premultiplied);
                if (m_width > kStickerPx || m_height > kStickerPx)
                    frame = frame.scaled(kStickerPx, kStickerPx, Qt::KeepAspectRatio, Qt::SmoothTransformation);
            }
            delay = qMax(delay, stamp - m_stamp);
            m_stamp = stamp;
            last = !WebPAnimDecoderHasMoreFrames(m_decoder);
        }
        if (!frame.isNull()) {
            m_inFlight->fetchAndStoreOrdered(1);
            // B, G, R, A in memory become R, G, B, A, which ImageData's RGBA_Premultiplied wants.
            emit frameReady(m_token, frame.rgbSwapped());
        }
        if (last) {
            if (!m_loop) {
                emit finished(m_token);
                return;
            }
            rewind();
        }
        m_timer->start(qMax(1, delay - int(spent.elapsed())));
    }

private:
    static const int kMinMs = 33; // 30 fps at most

    bool load(const QString &path)
    {
        if (Sticker::isLottie(path)) {
            const QByteArray json = Sticker::lottieJson(path);
            if (!json.isEmpty())
                m_animation = lottie_animation_from_data(json.constData(), path.toUtf8().constData(), "");
            m_frames = m_animation ? int(lottie_animation_get_totalframe(m_animation)) : 0;
            if (m_frames <= 0)
                return false;
            // A 60 fps sticker draws every other frame.
            m_step = qMax(1, qRound(lottie_animation_get_framerate(m_animation) / 30.0));
            m_canvas = QImage(kStickerPx, kStickerPx, QImage::Format_ARGB32_Premultiplied);
            m_canvas.fill(0);
            return true;
        }
        QFile f(path);
        if (!f.open(QIODevice::ReadOnly))
            return false;
        m_file = f.readAll(); // the decoder reads it in place until it is deleted
        WebPData data = { reinterpret_cast<const uint8_t *>(m_file.constData()), size_t(m_file.size()) };
        // The decoder allocates the whole canvas for whatever a sender claims, as in Sticker.cpp's firstFrame.
        WebPAnimDecoderOptions options;
        if (!WebPGetInfo(data.bytes, data.size, &m_width, &m_height) || m_width > 2048 || m_height > 2048
            || !WebPAnimDecoderOptionsInit(&options))
            return false;
        options.color_mode = MODE_bgrA; // QImage::Format_ARGB32_Premultiplied
        m_decoder = WebPAnimDecoderNew(&data, &options);
        return m_decoder != 0;
    }

    void unload()
    {
        if (m_animation)
            lottie_animation_destroy(m_animation);
        if (m_decoder)
            WebPAnimDecoderDelete(m_decoder);
        m_animation = 0;
        m_decoder = 0;
        m_path.clear();
        m_file.clear();
    }

    void rewind()
    {
        m_frame = 0;
        m_stamp = 0;
        if (m_decoder)
            WebPAnimDecoderReset(m_decoder);
    }

    QAtomicInt *m_inFlight; // the player's
    QAtomicInt *m_latest;   // the player's: the newest token it gave out
    QString m_path;         // of what is loaded
    // A Lottie: the animation, drawn into m_canvas. A WebP: the decoder, over m_file's bytes.
    Lottie_Animation *m_animation;
    WebPAnimDecoder *m_decoder;
    QByteArray m_file;
    QImage m_canvas;
    int m_width;
    int m_height;
    int m_token;
    int m_frame;
    int m_frames;
    int m_step;
    int m_stamp; // the WebP frame's end, to take its length from the next
    bool m_loop;
    QTimer *m_timer;
};

// The one player, once made; setCovered() reads it without making one.
static StickerPlayer *s_player = 0;

bool StickerPlayer::s_covered = false;

StickerPlayer *StickerPlayer::instance()
{
    if (!s_player)
        s_player = new StickerPlayer(QCoreApplication::instance());
    return s_player;
}

StickerPlayer::StickerPlayer(QObject *parent)
    : QObject(parent), m_thread(new QThread(this)), m_worker(new StickerWorker(&m_inFlight, &m_latest)), m_token(0),
      m_loop(false), m_latest(0), m_inFlight(0)
{
    m_worker->moveToThread(m_thread);
    // The worker dies on its own thread, when the thread ends (~StickerPlayer).
    connect(m_thread, SIGNAL(finished()), m_worker, SLOT(deleteLater()));
    connect(m_worker, SIGNAL(frameReady(int, QImage)), this, SLOT(onFrame(int, QImage)));
    connect(m_worker, SIGNAL(finished(int)), this, SLOT(onFinished(int)));
    m_thread->start();
    // Nothing plays behind the thumbnail or with the screen off.
    bb::cascades::Application *app = bb::cascades::Application::instance();
    connect(app, SIGNAL(thumbnail()), this, SLOT(stop()));
    connect(app, SIGNAL(invisible()), this, SLOT(stop()));
}

StickerPlayer::~StickerPlayer()
{
    m_thread->quit();
    m_thread->wait();
}

void StickerPlayer::play(bb::cascades::ImageView *view, const bb::cascades::Image &still, const QString &path, bool loop)
{
    if (m_view && m_view != view)
        stop();
    m_view = view;
    m_still = still;
    m_path = path;
    m_loop = loop;
    // Under another page: it starts when a chat is on top again (setCovered).
    if (s_covered)
        return;
    begin();
}

void StickerPlayer::begin()
{
    ++m_token;
    m_latest.fetchAndStoreOrdered(m_token);
    QMetaObject::invokeMethod(m_worker, "start", Qt::QueuedConnection, Q_ARG(int, m_token), Q_ARG(QString, m_path),
                              Q_ARG(bool, m_loop));
}

void StickerPlayer::setCovered(bool covered)
{
    if (covered == s_covered)
        return;
    s_covered = covered;
    if (!s_player || !s_player->m_view)
        return;
    if (covered) {
        // Paused: the view shows its still; m_view, m_path and m_loop stay so it can start again.
        ++s_player->m_token;
        s_player->m_latest.fetchAndStoreOrdered(s_player->m_token);
        QMetaObject::invokeMethod(s_player->m_worker, "halt", Qt::QueuedConnection);
        s_player->m_view->setImage(s_player->m_still);
    } else {
        s_player->begin();
    }
}

void StickerPlayer::release(bb::cascades::ImageView *view)
{
    if (m_view && m_view == view)
        stop();
}

bool StickerPlayer::playing(bb::cascades::ImageView *view) const
{
    return view && m_view == view;
}

void StickerPlayer::stop()
{
    ++m_token; // what the worker has queued is stale now
    m_latest.fetchAndStoreOrdered(m_token);
    QMetaObject::invokeMethod(m_worker, "halt", Qt::QueuedConnection);
    if (m_view)
        m_view->setImage(m_still);
    m_view = 0;
}

void StickerPlayer::onFrame(int token, const QImage &frame)
{
    m_inFlight.fetchAndStoreOrdered(0); // taken, drawn or not: the worker may send the next
    if (token != m_token)
        return;
    // The view went with its card, or Off was chosen while it played.
    if (!m_view || Settings::stickerPlayMode() == 2) {
        stop();
        return;
    }
    bb::ImageData image(bb::PixelFormat::RGBA_Premultiplied, frame.width(), frame.height());
    unsigned char *pixels = image.pixels();
    const int stride = image.bytesPerLine();
    for (int y = 0; y < frame.height(); ++y)
        memcpy(pixels + y * stride, frame.constScanLine(y), frame.width() * 4);
    m_view->setImage(bb::cascades::Image(image));
}

void StickerPlayer::onFinished(int token)
{
    // Back to the still: the card puts it back anyway at the row's next update, and a jump then would look like a glitch.
    // A sticker's last frame is usually its first.
    if (token == m_token && m_view) {
        m_view->setImage(m_still);
        m_view = 0;
    }
}

#include "StickerPlayer.moc"
