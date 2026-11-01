#include "MediaOpener.h"

#include "EngineLink.h"
#include "sizes.h"

#include <bb/cascades/Image>
#include <bb/system/InvokeReplyError>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QBuffer>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <QImageReader>
#include <QUrl>

using namespace bb::system;

MediaOpener::MediaOpener(QObject *parent)
    : QObject(parent)
    , m_region(bb::system::LocaleType::Region)
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

// A received photo lives in the sandbox (phase6.md, choice 1), where neither Pictures nor File
// Manager looks: Save puts a copy where both do. A copy already there is the answer.
QString MediaOpener::saveToPictures(const QString &path)
{
    const QString dir = "/accounts/1000/shared/photos/HyperBerry";
    if (!QDir().mkpath(dir))
        return QString();
    const QString target = dir + "/" + QFileInfo(path).fileName();
    if (QFile::exists(target) || QFile::copy(path, target))
        return target;
    return QString();
}

// ponytail: EXIF orientation isn't applied (Qt 4.8's reader ignores it): a portrait camera photo's
// thumbnail may lie on its side. Decoded on the UI thread, at reduced size.
QVariantMap MediaOpener::photoInfo(const QString &path)
{
    QVariantMap info;
    const QString ext = QFileInfo(path).suffix().toLower();
    if (ext != "jpg" && ext != "jpeg")
        return info;
    QImageReader reader(path);
    const QSize size = reader.size(); // from the header
    if (!size.isValid())
        return info;
    QSize to = size;
    if (to.width() > 96 || to.height() > 96)
        to.scale(96, 96, Qt::KeepAspectRatio);
    reader.setScaledSize(to);
    const QImage image = reader.read();
    if (image.isNull())
        return info;
    info["width"] = size.width();
    info["height"] = size.height();
    // The engine takes at most 16 KB; a busy picture may need a lower quality.
    for (int quality = 60; quality >= 40; quality -= 20) {
        QByteArray jpeg;
        QBuffer buffer(&jpeg);
        buffer.open(QIODevice::WriteOnly);
        if (image.save(&buffer, "JPEG", quality) && jpeg.size() <= 16 * 1024) {
            info["thumb"] = QString::fromLatin1(jpeg.toBase64().constData());
            break;
        }
    }
    return info;
}

// The thumbnail is photoInfo's, decoded back from its base64: one place makes it.
QVariantMap MediaOpener::fileInfo(const QString &path)
{
    QVariantMap info;
    const QFileInfo file(path);
    if (!file.isFile())
        return info;
    info["name"] = file.fileName();
    info["sizeText"] = sizeText(file.size(), m_region.locale());
    const QString ext = file.suffix().toLower();
    info["picture"] = ext == "jpg" || ext == "jpeg" || ext == "mp4" || ext == "3gp" || ext == "mov";
    const QByteArray thumb = QByteArray::fromBase64(photoInfo(path)["thumb"].toString().toLatin1());
    if (!thumb.isEmpty())
        info["image"] = QVariant::fromValue(bb::cascades::Image(thumb));
    return info;
}
