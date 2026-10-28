#include "gates.h"

#include <bb/system/InvokeManager>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QStringList>

using namespace bb::system;

Gates::Gates(QObject *parent)
    : QObject(parent)
{
    refresh();
}

void Gates::startEngine()
{
    InvokeRequest request;
    request.setTarget("com.zsavajji.hyperberry.service");
    request.setAction("com.zsavajji.hyperberry.START");
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
