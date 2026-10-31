#include "ChatController.h"

#include "EngineLink.h"
#include "MessageModel.h"

#include <bb/utility/i18n/Formatting>
#include <QDate>
#include <QDateTime>
#include <QLocale>

using namespace bb::utility::i18n;

ChatController::ChatController(QObject *parent)
    : QObject(parent)
    , m_messages(0)
    , m_group(false)
    , m_composing(false)
    , m_region(bb::system::LocaleType::Region)
{
    // WhatsApp's typing has no guaranteed end: clear it after 25 s without an update.
    m_idle.setSingleShot(true);
    m_idle.setInterval(25000);
    connect(&m_idle, SIGNAL(timeout()), this, SLOT(typingIdle()));
    // WhatsApp's typing state times out on the other side: re-send "on" at most every 5 s while
    // typing continues (step 3.2).
    m_composeRepeat.setSingleShot(true);
    m_composeRepeat.setInterval(5000);
    // After 5 s without a key, send "off" so the other side never shows a stale "typing…".
    m_composePause.setSingleShot(true);
    m_composePause.setInterval(5000);
    connect(&m_composePause, SIGNAL(timeout()), this, SLOT(composePaused()));
}

ChatController::~ChatController()
{
    if (m_engine && m_engine->connected() && !m_jid.isEmpty()) {
        QVariantMap args;
        args["chat"] = m_jid;
        if (m_composing) {
            QVariantMap off = args;
            off["on"] = false;
            m_engine->request("chat.typing", off);
        }
        m_engine->request("chat.close", args);
    }
}

QObject *ChatController::link() const
{
    return m_engine;
}

// At app exit the scene (and this) may outlive the EngineLink on main()'s stack: a QPointer.
void ChatController::setLink(QObject *link)
{
    m_engine = qobject_cast<EngineLink *>(link);
}

bb::cascades::DataModel *ChatController::messages() const
{
    return m_messages;
}

QString ChatController::status() const
{
    return m_typing.isEmpty() ? m_presence : m_typing;
}

void ChatController::open(const QVariantMap &chat)
{
    if (m_messages || !m_engine)
        return;
    m_jid = chat["jid"].toString();
    m_group = chat["kind"].toInt() == 1;
    m_messages = new MessageModel(m_engine, m_jid, m_group, this);
    emit messagesChanged();
    connect(m_engine, SIGNAL(typing(QString, QString, bool, bool)), this,
            SLOT(onTyping(QString, QString, bool, bool)));
    connect(m_engine, SIGNAL(presence(QString, bool, qint64)), this,
            SLOT(onPresence(QString, bool, qint64)));
    connect(m_engine, SIGNAL(connectedChanged()), this, SLOT(onConnected()));
    sendOpen();
}

void ChatController::nearTop(const QVariantList &firstVisible)
{
    if (m_messages)
        m_messages->nearTop(firstVisible);
}

bool ChatController::send(const QString &text)
{
    if (!m_engine || !m_engine->connected())
        return false;
    setComposing(false);
    QVariantMap args;
    args["chat"] = m_jid;
    args["text"] = text;
    m_engine->request("msg.send", args);
    return true;
}

void ChatController::composing(const QString &text)
{
    if (text.isEmpty()) {
        setComposing(false);
        return;
    }
    if (!m_composing || !m_composeRepeat.isActive())
        setComposing(true);
    m_composePause.start();
}

void ChatController::composePaused()
{
    setComposing(false);
}

void ChatController::setComposing(bool on)
{
    QVariantMap args;
    args["chat"] = m_jid;
    args["on"] = on;
    if (on) {
        if (m_engine && m_engine->connected())
            m_engine->request("chat.typing", args);
        m_composing = true;
        m_composeRepeat.start();
        return;
    }
    if (!m_composing)
        return;
    m_composing = false;
    m_composeRepeat.stop();
    m_composePause.stop();
    if (m_engine && m_engine->connected())
        m_engine->request("chat.typing", args);
}

void ChatController::sendOpen()
{
    QVariantMap args;
    args["chat"] = m_jid;
    m_engine->request("chat.open", args);
}

// An engine restart loses the presence subscription, and at a cold start the first chat.open
// failed with no_engine.
void ChatController::onConnected()
{
    if (m_engine && m_engine->connected())
        sendOpen();
}

void ChatController::setTyping(const QString &text)
{
    if (text == m_typing)
        return;
    m_typing = text;
    emit statusChanged();
}

// ponytail: in a group only the last one typing shows; keep a set of names if it matters.
void ChatController::onTyping(const QString &chat, const QString &who, bool on, bool voice)
{
    if (chat != m_jid)
        return;
    if (!on) {
        m_idle.stop();
        setTyping(QString());
        return;
    }
    m_idle.start();
    if (m_group)
        setTyping(voice ? tr("%1 is recording audio…").arg(who) : tr("%1 is typing…").arg(who));
    else
        setTyping(voice ? tr("recording audio…") : tr("typing…"));
}

void ChatController::typingIdle()
{
    setTyping(QString());
}

// "N participants" for a group comes with group.info in 3.5.
void ChatController::onPresence(const QString &chat, bool online, qint64 lastSeen)
{
    if (chat != m_jid || m_group)
        return;
    QString text;
    if (online) {
        text = tr("online");
    } else if (lastSeen > 0) {
        const QLocale locale = m_region.locale();
        const QDateTime when = QDateTime::fromTime_t(lastSeen);
        const QString time = locale.toString(when.time(), timeFormat(locale, DateFormat::Short));
        const int age = when.date().daysTo(QDate::currentDate());
        if (age == 0)
            text = tr("last seen today at %1").arg(time);
        else if (age == 1)
            text = tr("last seen yesterday at %1").arg(time);
        else
            text = tr("last seen %1 at %2").arg(locale.toString(when.date(), QLocale::ShortFormat), time);
    }
    if (text != m_presence) {
        m_presence = text;
        emit statusChanged();
    }
}
