#include "Invites.h"

#include "EngineLink.h"

#include <bb/system/InvokeManager>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QRegExp>
#include <QUrl>

using namespace bb::system;

// wa.me/<number>, api.whatsapp.com/send?phone=<number>, whatsapp://send?phone=<number>: the
// number's digits, "" for anything else. text gets the link's text=.
static QString phoneLink(const QString &link, QString *text)
{
    const QUrl url(link.contains("://") ? link.trimmed() : "https://" + link.trimmed());
    const QString host = url.host().toLower();
    QString phone;
    if (host == "wa.me" || host == "www.wa.me") {
        // Only digits: wa.me/qr/<code> and wa.me/message/<code> go to the browser.
        if (QRegExp("/\\+?[0-9]+/?").exactMatch(url.path()))
            phone = url.path();
    } else if ((host == "send" && url.scheme() == "whatsapp")
               || (host.endsWith("whatsapp.com") && url.path().startsWith("/send"))) {
        phone = QUrl::fromPercentEncoding(url.encodedQueryItemValue("phone"));
    }
    phone.remove(QRegExp("[^0-9]"));
    if (phone.length() < 6 || phone.length() > 15)
        return QString();
    // Form encoding: + is a space.
    *text = QUrl::fromPercentEncoding(url.encodedQueryItemValue("text").replace('+', ' '));
    return phone;
}

Invites::Invites(EngineLink *engine, QObject *parent)
    : QObject(parent)
    , m_engine(engine)
{
    connect(m_engine, SIGNAL(replied(QString, QString, QVariant)), this,
            SLOT(onReplied(QString, QString, QVariant)));
}

// Only a cheap look: the engine does the real parsing, and says bad_request to what isn't one.
bool Invites::openLink(const QString &url)
{
    if (!m_engine)
        return false;
    QString text;
    const QString phone = phoneLink(url, &text);
    if (!phone.isEmpty()) {
        QVariantMap args;
        args["phone"] = phone;
        m_phone = phone;
        m_text = text;
        m_looking = m_engine->request("contact.lookup", args);
        return true;
    }
    if (!url.contains("chat.whatsapp.com/") && !url.contains("whatsapp.com/channel/"))
        return false;
    QVariantMap args;
    args["url"] = url;
    m_asking = m_engine->request("invite.info", args);
    return true;
}

void Invites::openExternal(const QString &url)
{
    if (openLink(url) || !m_engine)
        return;
    InvokeRequest request;
    request.setTarget("sys.browser");
    request.setAction("bb.action.OPEN");
    request.setUri(url);
    InvokeTargetReply *reply = m_engine->invoker()->invoke(request);
    if (reply)
        connect(reply, SIGNAL(finished()), reply, SLOT(deleteLater()));
}

void Invites::openCard(const QString &chat, const QString &id, const QString &name)
{
    m_cmd = "group.join";
    m_args.clear();
    m_args["chat"] = chat;
    m_args["id"] = id;
    emit offered("group", name, 0);
}

void Invites::accept()
{
    if (m_engine && !m_cmd.isEmpty())
        m_accepting = m_engine->request(m_cmd, m_args);
}

void Invites::onReplied(const QString &id, const QString &, const QVariant &result)
{
    const QVariantMap map = result.toMap();
    if (id == m_looking) {
        m_looking.clear();
        emit chatLinked(map.value("jid").toString(), m_phone, m_text);
    } else if (id == m_asking) {
        m_asking.clear();
        if (map.value("member").toBool()) {
            emit joined(map.value("jid").toString());
            return;
        }
        m_cmd = "invite.accept";
        m_args.clear();
        const char *keys[] = { "kind", "code", "jid", "name" };
        for (int i = 0; i < 4; ++i)
            m_args[keys[i]] = map.value(keys[i]);
        emit offered(map.value("kind").toString(), map.value("name").toString(),
                     map.value("count").toInt());
    } else if (id == m_accepting) {
        m_accepting.clear();
        emit joined(map.value("jid").toString());
    }
}
