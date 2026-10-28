#ifndef GATES_H
#define GATES_H

#include <QObject>
#include <QString>

// The Phase 0 gate checks on the phone (PLAN.md §9, steps 0.3 onwards): start the engine as a
// headless service and show what it reports. Phase 2 replaces this with the real UI classes.
class Gates : public QObject
{
    Q_OBJECT
    Q_PROPERTY(QString status READ status NOTIFY changed)
    Q_PROPERTY(QString log READ log NOTIFY changed)

public:
    explicit Gates(QObject *parent = 0);

    QString status() const { return m_status; }
    QString log() const { return m_log; }

    // Invokes the engine's headless target, as a cold UI start will (docs/protocol.md).
    Q_INVOKABLE void startEngine();
    // Re-reads data/engine.json and the tail of the engine's log.
    Q_INVOKABLE void refresh();

signals:
    void changed();

private slots:
    void invoked();

private:
    QString m_status;
    QString m_log;
};

#endif
