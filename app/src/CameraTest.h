#ifndef CAMERATEST_H
#define CAMERATEST_H

#include "CameraEncoder.h"
#include "VideoDecoder.h"
#include "VideoWindow.h"

#include <QAtomicInt>
#include <QMetaObject>
#include <QObject>
#include <QString>
#include <QThread>
#include <QTimer>

// Settings -> Camera test (a developer test, docs/plan/phase7c.md step 7c.1): the camera encodes
// H.264 in hardware (CameraEncoder), each access unit goes straight into the hardware decoder
// (VideoDecoder) and its window (VideoWindow): camera -> encoder -> decoder -> screen, no network,
// the camera's own viewfinder in a second window beside it. The numbers refresh once a second and
// a `camera test:` line goes to the UI's log every 10 s. A call does the same with its network in
// the middle: the encoder's units out, the peer's in.
class CameraTest : public QObject, private CameraEncoder::Sink
{
    Q_OBJECT
    Q_PROPERTY(bool running READ running NOTIFY runningChanged)
    // Why it can't run, "" when it can.
    Q_PROPERTY(QString error READ error NOTIFY runningChanged)
    // Each restarts a running test.
    Q_PROPERTY(bool front READ front WRITE setFront NOTIFY optionsChanged)
    Q_PROPERTY(bool small READ small WRITE setSmall NOTIFY optionsChanged)
    Q_PROPERTY(int fps READ fps WRITE setFps NOTIFY optionsChanged)
    Q_PROPERTY(QString modeText READ modeText NOTIFY optionsChanged)
    // Since the last summary line (every 10 s); refreshed once a second while running.
    Q_PROPERTY(double encFps READ encFps NOTIFY statsChanged)
    Q_PROPERTY(double encKbps READ encKbps NOTIFY statsChanged)
    Q_PROPERTY(int keyframes READ keyframes NOTIFY statsChanged)
    Q_PROPERTY(int meanFrameBytes READ meanFrameBytes NOTIFY statsChanged)
    Q_PROPERTY(int maxFrameBytes READ maxFrameBytes NOTIFY statsChanged)
    Q_PROPERTY(double intervalMean READ intervalMean NOTIFY statsChanged)
    Q_PROPERTY(double intervalMax READ intervalMax NOTIFY statsChanged)
    Q_PROPERTY(int framesIn READ framesIn NOTIFY statsChanged)
    Q_PROPERTY(int framesOut READ framesOut NOTIFY statsChanged)
    Q_PROPERTY(double latencyMean READ latencyMean NOTIFY statsChanged)
    Q_PROPERTY(double latencyMax READ latencyMax NOTIFY statsChanged)
    // Of one core, this whole process.
    Q_PROPERTY(double cpuPercent READ cpuPercent NOTIFY statsChanged)
public:
    explicit CameraTest(QObject *parent = 0);
    ~CameraTest();

    bool running() const { return m_running; }
    QString error() const { return m_error; }
    bool front() const { return m_front; }
    bool small() const { return m_small; }
    int fps() const { return m_fps; }
    QString modeText() const;
    void setFront(bool on);
    void setSmall(bool on);
    void setFps(int fps);

    double encFps() const { return m_enc.wallMs > 0 ? m_enc.frames * 1000.0 / m_enc.wallMs : 0; }
    double encKbps() const { return m_enc.wallMs > 0 ? m_enc.bytes * 8 / m_enc.wallMs : 0; }
    int keyframes() const { return m_enc.keyframes; }
    int meanFrameBytes() const { return m_enc.frames > 0 ? int(m_enc.bytes / m_enc.frames) : 0; }
    int maxFrameBytes() const { return int(m_enc.maxFrameBytes); }
    double intervalMean() const { return m_enc.intervalMeanMs; }
    double intervalMax() const { return m_enc.intervalMaxMs; }
    int framesIn() const { return m_dec.framesIn; }
    int framesOut() const { return m_dec.framesOut; }
    double latencyMean() const { return m_dec.latencyMeanMs; }
    double latencyMax() const { return m_dec.latencyMaxMs; }
    double cpuPercent() const { return m_dec.wallMs > 0 ? 100 * m_dec.cpuMs / m_dec.wallMs : 0; }

    // Both ids are ForeignWindowControls' that exist: the decoded picture's, and the one the
    // camera's viewfinder window will be adopted by. Replaces any earlier run; returns at once,
    // the decoder and the camera open on the thread.
    Q_INVOKABLE void start(const QString &videoWindowId, const QString &viewfinderWindowId);
    // Joins the thread (camera and decoder closed, a second at most) and frees the window. Safe
    // to call twice, and when nothing runs.
    Q_INVOKABLE void stop();

signals:
    void runningChanged();
    void statsChanged();
    void optionsChanged();

private slots:
    void refresh();
    void setError(const QString &text);
    void ended();

private:
    class Runner : public QThread
    {
    public:
        explicit Runner(CameraTest *owner) : m_owner(owner) {}
    protected:
        void run()
        {
            m_owner->work();
            QMetaObject::invokeMethod(m_owner, "ended", Qt::QueuedConnection);
        }
    private:
        CameraTest *m_owner;
    };

    void accessUnit(const QByteArray &annexB, qint64 tsUs, bool keyframe);
    void work();
    void summarize();
    void restart();
    int width() const { return m_small ? 320 : 640; }
    int height() const { return m_small ? 240 : 480; }

    VideoWindow m_window;
    VideoDecoder m_decoder;
    CameraEncoder m_encoder;
    Runner m_runner;
    QTimer m_timer;
    QAtomicInt m_stop;
    QAtomicInt m_feedFailed;
    bool m_running;
    QString m_error;
    bool m_front, m_small;
    int m_fps;
    QString m_videoId, m_vfId;
    QByteArray m_group;
    int m_ticks;
    CameraEncoder::Stats m_enc;
    VideoDecoder::Stats m_dec;
};

#endif
