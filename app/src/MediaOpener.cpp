#include "MediaOpener.h"

#include "EngineLink.h"

#include <bb/system/InvokeReplyError>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QDebug>
#include <QUrl>

using namespace bb::system;

MediaOpener::MediaOpener(QObject *parent)
    : QObject(parent)
{
}

QObject *MediaOpener::link() const
{
    return m_engine;
}

// A QPointer, as ContactInfo's: at app exit the page may outlive the EngineLink.
void MediaOpener::setLink(QObject *link)
{
    m_engine = qobject_cast<EngineLink *>(link);
    if (m_engine) {
        connect(m_engine, SIGNAL(replied(QString, QString, QVariant)), this,
                SLOT(onReplied(QString, QString, QVariant)));
        connect(m_engine, SIGNAL(failureChanged()), this, SLOT(onFailure()));
    }
}

QString MediaOpener::loadingId() const
{
    return m_loadingId;
}

void MediaOpener::setLoadingId(const QString &id)
{
    if (id == m_loadingId)
        return;
    m_loadingId = id;
    emit loadingIdChanged();
}

void MediaOpener::get(const QString &chat, const QString &id, const QString &kind)
{
    if (!m_engine)
        return;
    QVariantMap args;
    args["chat"] = chat;
    args["id"] = id;
    m_kind = kind;
    // "" when the engine is down: request() has set the failure, and nothing is loading.
    m_reqId = m_engine->request("media.get", args);
    setLoadingId(m_reqId.isEmpty() ? QString() : id);
}

void MediaOpener::onReplied(const QString &id, const QString &cmd, const QVariant &result)
{
    if (cmd != "media.get" || id != m_reqId)
        return;
    const QString msgId = m_loadingId;
    m_reqId.clear();
    setLoadingId(QString());
    emit ready(msgId, m_kind, result.toMap()["path"].toString());
}

// ponytail: a failure names no request, so an older one's, after a newer get, ends the newer
// one's spinner too; the page's toast shows either way.
void MediaOpener::onFailure()
{
    if (m_loadingId.isEmpty() || !m_engine
        || m_engine->failure().value("cmd").toString() != "media.get")
        return;
    m_reqId.clear();
    setLoadingId(QString());
}

// The file is in Downloads, not our sandbox, because another app can't read the sandbox
// (phase6.md, choice 1). No target: the system picks the app by the file's type.
bool MediaOpener::openFile(const QString &path)
{
    InvokeRequest request;
    request.setAction("bb.action.OPEN");
    request.setUri(QUrl::fromLocalFile(path));
    InvokeTargetReply *reply = m_invoker.invoke(request);
    if (!reply) {
        qWarning() << "media: no reply from the system to OPEN" << path;
        return false;
    }
    connect(reply, SIGNAL(finished()), this, SLOT(onInvoked()));
    return true;
}

void MediaOpener::onInvoked()
{
    InvokeTargetReply *reply = qobject_cast<InvokeTargetReply *>(sender());
    if (!reply)
        return;
    if (reply->error() != InvokeReplyError::None) {
        qWarning() << "media: OPEN failed, error" << reply->error();
        emit openFailed(); // no app for the type, or no access to Downloads
    }
    reply->deleteLater();
}
