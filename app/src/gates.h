#ifndef GATES_H
#define GATES_H

#include <QObject>
#include <QString>
#include <QTimer>

// The Phase 0 gate checks on the phone (PLAN.md §9, steps 0.3 onwards): start the engine as a
// headless service and show what it reports. Phase 2 replaces this with the real UI classes.
class Gates : public QObject
{
    Q_OBJECT
    Q_PROPERTY(QString status READ status NOTIFY changed)
    Q_PROPERTY(QString log READ log NOTIFY changed)
    Q_PROPERTY(QString sql READ sql NOTIFY changed)

public:
    explicit Gates(QObject *parent = 0);

    QString status() const { return m_status; }
    QString log() const { return m_log; }
    QString sql() const { return m_sql; }

    // Invokes the engine's headless target, as a cold UI start will (docs/protocol.md).
    Q_INVOKABLE void startEngine();
    // Re-reads data/engine.json and the tail of the engine's log.
    Q_INVOKABLE void refresh();
    // Step 0.4: the engine writes a row to data/sqltest.db every 50 ms; this reads the newest
    // one every 100 ms through QSQLITE for 5 min, counting errors.
    Q_INVOKABLE void startSqlTest();

signals:
    void changed();

private slots:
    void invoked();
    void readSql();

private:
    void invoke(const QString &action);

    QString m_status;
    QString m_log;
    QString m_sql;

    QTimer m_sqlTimer;
    QString m_sqlVersion;
    QString m_sqlError;
    qint64 m_sqlStart; // ms since epoch: rows older than this are from an earlier run
    qint64 m_sqlFirst; // ms since epoch of the first row read, 0 before
    qint64 m_newest;
    qint64 m_maxLag;
    int m_reads;
    int m_errors;
    int m_busy;
};

#endif
