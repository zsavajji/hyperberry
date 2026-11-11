#ifndef VIDEOTEST_H
#define VIDEOTEST_H

#include "VideoDecoder.h"
#include "VideoWindow.h"

#include <QAtomicInt>
#include <QByteArray>
#include <QList>
#include <QObject>
#include <QString>
#include <QStringList>
#include <QThread>
#include <QTimer>

// Settings -> Video decoders test (a developer test, step V0 of docs/plan/phase7e.md): plays the
// bundled 640x480@30 H.264 clip through 1 to 4 VideoDecoders at once, each into its own
// VideoWindow, looping, and reports per decoder whether it opened, frames, fps and latency, plus
// the process's CPU. It answers how many simultaneous decoders and windows the Q10 can run (the
// grid of a group video call). Every pass of every slot ends with a `video test:` line in the log.
class VideoTest : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool running READ running NOTIFY runningChanged)
    // Why it can't play, "" when it can.
    Q_PROPERTY(QString error READ error NOTIFY runningChanged)
    // One line per decoder and a last one for the CPU; refreshed once a second while running.
    Q_PROPERTY(QString report READ report NOTIFY statsChanged)
    // How many decoders (1..4); a change restarts a running test.
    Q_PROPERTY(int count READ count WRITE setCount NOTIFY optionsChanged)
public:
    explicit VideoTest(QObject *parent = 0);
    ~VideoTest();

    bool running() const { return m_playing; }
    QString error() const { return m_error; }
    QString report() const { return m_report; }
    int count() const { return m_count; }
    void setCount(int count);

    // windowIds are the ForeignWindowControls' (one per decoder, which must exist). Replaces any
    // earlier run; returns at once, each decoder opens on its own thread, 300 ms after the one
    // before so that a failure is the one that just opened.
    Q_INVOKABLE void start(const QStringList &windowIds);
    // Joins the threads (each closes its decoder, a few hundred ms at most) and frees the windows.
    // Safe to call twice, and when nothing runs.
    Q_INVOKABLE void stop();

signals:
    void runningChanged();
    void statsChanged();
    void optionsChanged();

private slots:
    void refresh();

private:
    static const int MaxSlots = 4;

    // One decoder, its window and the thread that feeds it.
    class Slot : public QThread
    {
    public:
        Slot() : decoder(&window), owner(0), index(0) {}
        VideoWindow window;
        VideoDecoder decoder;
        VideoTest *owner;
        int index;
        QAtomicInt opened, failed; // set by the thread, read by the UI
    protected:
        void run() { owner->play(this); }
    };

    void play(Slot *slot);
    bool pace(double dueMs);
    void summarize(Slot *slot, int pass, double firstFrameMs);

    Slot m_slot[MaxSlots];
    QTimer m_timer;
    QAtomicInt m_stop;
    bool m_playing;
    int m_count;
    QStringList m_windowIds;
    QString m_error, m_report;
    // The clip, read once and shared read-only by the threads.
    QList<QByteArray> m_units;
    int m_largest;
};

#endif
