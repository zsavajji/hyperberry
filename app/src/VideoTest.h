#ifndef VIDEOTEST_H
#define VIDEOTEST_H

#include "VideoDecoder.h"
#include "VideoWindow.h"

#include <QAtomicInt>
#include <QMetaObject>
#include <QObject>
#include <QString>
#include <QThread>
#include <QTimer>

// Settings -> Video decoder test (a developer test): plays the bundled 640x480@30 H.264 clip
// through VideoDecoder into VideoWindow at its real frame rate, looping, and reports what an app
// gets from the Q10's hardware decoder (docs/plan/phase7.md, "Video calls: the hardware-decode
// gate"). Every pass ends with a `video test:` line in the UI's log. The same three pieces will take
// a call's video: feed() the access units the engine delivers instead of the clip's.
class VideoTest : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool running READ running NOTIFY runningChanged)
    // Why it can't play, "" when it can.
    Q_PROPERTY(QString error READ error NOTIFY runningChanged)
    // Since the pass began; refreshed once a second while running.
    Q_PROPERTY(int framesIn READ framesIn NOTIFY statsChanged)
    Q_PROPERTY(int framesOut READ framesOut NOTIFY statsChanged)
    Q_PROPERTY(double fps READ fps NOTIFY statsChanged)
    Q_PROPERTY(double latencyMean READ latencyMean NOTIFY statsChanged)
    Q_PROPERTY(double latencyMax READ latencyMax NOTIFY statsChanged)
    Q_PROPERTY(double firstFrameMs READ firstFrameMs NOTIFY statsChanged)
    // Of one core, this whole process (the UI included, idle but for this).
    Q_PROPERTY(double cpuPercent READ cpuPercent NOTIFY statsChanged)
    Q_PROPERTY(double detileMs READ detileMs NOTIFY statsChanged)
    Q_PROPERTY(int pass READ pass NOTIFY statsChanged)
public:
    explicit VideoTest(QObject *parent = 0);
    ~VideoTest();

    bool running() const { return m_playing; }
    QString error() const { return m_error; }
    int framesIn() const { return m_stats.framesIn; }
    int framesOut() const { return m_stats.framesOut; }
    double fps() const { return m_stats.wallMs > 0 ? m_stats.framesOut * 1000.0 / m_stats.wallMs : 0; }
    double latencyMean() const { return m_stats.latencyMeanMs; }
    double latencyMax() const { return m_stats.latencyMaxMs; }
    double firstFrameMs() const { return m_stats.firstFrameMs; }
    double cpuPercent() const { return m_stats.wallMs > 0 ? 100 * m_stats.cpuMs / m_stats.wallMs : 0; }
    double detileMs() const { return m_detileMean; }
    int pass() const { return m_pass; }

    // windowId is the ForeignWindowControl's, which must exist. Replaces any earlier run; returns
    // at once, the decoder opens on the thread.
    Q_INVOKABLE void start(const QString &windowId);
    // Joins the thread (the decoder's close, a few hundred ms at most) and frees the window.
    // Safe to call twice, and when nothing runs.
    Q_INVOKABLE void stop();

signals:
    void runningChanged();
    void statsChanged();

private slots:
    void refresh();
    void setError(const QString &text);
    void ended();

private:
    class Player : public QThread
    {
    public:
        explicit Player(VideoTest *owner) : m_owner(owner) {}
    protected:
        void run()
        {
            m_owner->play();
            QMetaObject::invokeMethod(m_owner, "ended", Qt::QueuedConnection);
        }
    private:
        VideoTest *m_owner;
    };

    void play();
    bool pace(double dueMs);
    void summarize(int pass);

    VideoWindow m_window;
    VideoDecoder m_decoder;
    Player m_player;
    QTimer m_timer;
    QAtomicInt m_stop;
    bool m_playing;
    QString m_error;
    VideoDecoder::Stats m_stats;
    double m_detileMean, m_detileMax;
    int m_pass;
};

#endif
