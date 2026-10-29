#include "gates.h"

#include <bb/system/InvokeManager>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>
#include <QStringList>
#include <QVariant>

using namespace bb::system;

Gates::Gates(QObject *parent)
    : QObject(parent)
    , m_sqlStart(0)
    , m_sqlFirst(0)
    , m_newest(0)
    , m_maxLag(0)
    , m_reads(0)
    , m_errors(0)
    , m_busy(0)
{
    m_sqlTimer.setInterval(100);
    connect(&m_sqlTimer, SIGNAL(timeout()), this, SLOT(readSql()));
    // Made before the event loop runs, so an invoke that launched the app isn't missed.
    InvokeManager *manager = new InvokeManager(this);
    connect(manager, SIGNAL(invoked(const bb::system::InvokeRequest &)),
            this, SLOT(opened(const bb::system::InvokeRequest &)));
    refresh();
}

void Gates::opened(const InvokeRequest &request)
{
    m_test = tr("Opened from a notification at %1: %2, payload \"%3\".")
                 .arg(QDateTime::currentDateTime().toString("hh:mm:ss"), request.action(),
                      QString::fromUtf8(request.data()));
    emit changed();
}

QString Gates::voiceUrl(const QString &name)
{
    const QString dir = QDir::homePath() + "/voice";
    QDir().mkpath(dir);
    return "file://" + dir + "/" + name;
}

void Gates::startEngine()
{
    invoke("com.zsavajji.hyperberry.START");
}

void Gates::invoke(const QString &action)
{
    InvokeRequest request;
    request.setTarget("com.zsavajji.hyperberry.service");
    request.setAction(action);
    InvokeManager *manager = new InvokeManager(this);
    InvokeTargetReply *reply = manager->invoke(request);
    if (!reply) {
        m_status = tr("The engine couldn't be started: no reply from the system.");
        emit changed();
        return;
    }
    reply->setParent(manager);
    connect(reply, SIGNAL(finished()), this, SLOT(invoked()));
}

void Gates::invoked()
{
    InvokeTargetReply *reply = qobject_cast<InvokeTargetReply *>(sender());
    if (reply && reply->error() != InvokeReplyError::None)
        m_status = tr("Starting the engine failed: error %1.").arg(reply->error());
    else
        refresh();
    if (reply)
        reply->parent()->deleteLater();
    emit changed();
}

void Gates::refresh()
{
    const QString home = QDir::homePath();
    QFileInfo info(home + "/engine.json");
    QFile engine(info.filePath());
    if (engine.open(QIODevice::ReadOnly)) {
        // The token stays private: show only that the file is there, and when it was written.
        m_status = tr("Engine running since %1.").arg(info.lastModified().toString("hh:mm:ss"));
    } else {
        m_status = tr("The engine isn't running (no engine.json in %1).").arg(home);
    }

    QFile logFile(home + "/logs/hyperberryd.log");
    m_log.clear();
    if (logFile.open(QIODevice::ReadOnly)) {
        if (logFile.size() > 4096)
            logFile.seek(logFile.size() - 4096);
        QStringList lines = QString::fromUtf8(logFile.readAll()).split('\n');
        m_log = QStringList(lines.mid(qMax(0, lines.size() - 16))).join("\n");
    }
    emit changed();
}

void Gates::startSqlTest()
{
    if (!QSqlDatabase::contains("sqltest")) {
        QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", "sqltest");
        db.setDatabaseName(QDir::homePath() + "/sqltest.db");
        // Read-only, as the UI will open chats.db (PLAN.md §12).
        db.setConnectOptions("QSQLITE_OPEN_READONLY");
    }
    m_sqlStart = QDateTime::currentMSecsSinceEpoch();
    m_sqlFirst = m_newest = m_maxLag = 0;
    m_reads = m_errors = m_busy = 0;
    m_sqlError.clear();
    m_test = tr("SQLite test: waiting for the engine's first row.");
    invoke("com.zsavajji.hyperberry.SQLTEST");
    m_sqlTimer.start();
    emit changed();
}

void Gates::readSql()
{
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    QSqlDatabase db = QSqlDatabase::database("sqltest", false);
    QSqlError error;
    if (!db.isOpen() && !db.open()) {
        error = db.lastError();
    } else {
        if (m_sqlVersion.isEmpty()) {
            QSqlQuery version("SELECT sqlite_version()", db);
            if (version.next())
                m_sqlVersion = version.value(0).toString();
        }
        QSqlQuery newest(db);
        newest.prepare("SELECT n, at FROM sqltest WHERE at >= ? ORDER BY n DESC LIMIT 1");
        newest.addBindValue(m_sqlStart);
        if (!newest.exec()) {
            error = newest.lastError();
        } else if (newest.next()) {
            if (!m_sqlFirst)
                m_sqlFirst = now;
            m_reads++;
            m_newest = newest.value(0).toLongLong();
            m_maxLag = qMax(m_maxLag, now - newest.value(1).toLongLong());
        }
    }

    // Before the first row the engine may not have made the file or the table yet.
    if (!m_sqlFirst) {
        m_test = tr("SQLite test: waiting for the engine's first row. %1").arg(error.text());
        emit changed();
        return;
    }
    if (error.isValid()) {
        m_errors++;
        if (error.number() == 5 || error.number() == 6) // SQLITE_BUSY, SQLITE_LOCKED
            m_busy++;
        m_sqlError = error.text();
    }
    const int seconds = (now - m_sqlFirst) / 1000;
    if (seconds >= 300)
        m_sqlTimer.stop();
    m_test = tr("SQLite %1%2: %3 reads in %4 s, newest row %5, longest lag %6 ms. "
               "Errors: %7 (busy or locked: %8). %9")
                .arg(m_sqlVersion, m_sqlTimer.isActive() ? QString() : tr(", done"))
                .arg(m_reads).arg(seconds).arg(m_newest).arg(m_maxLag)
                .arg(m_errors).arg(m_busy).arg(m_sqlError);
    emit changed();
}
