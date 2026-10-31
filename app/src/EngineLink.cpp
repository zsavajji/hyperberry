#include "EngineLink.h"

#include <bb/cascades/Application>
#include <bb/data/JsonDataAccess>
#include <bb/system/InvokeRequest>
#include <bb/system/InvokeTargetReply>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QHostAddress>
#include <QSettings>

using namespace bb::data;
using namespace bb::system;

EngineLink::EngineLink(QObject *parent)
    : QObject(parent)
    , m_invoker(new InvokeManager(this))
    , m_counter(0)
    , m_connected(false)
    , m_conn("offline")
    , m_fullscreen(true) // the app opens fullscreen
{
    // Qt 4.8's QElapsedTimer has no constructor: left alone it holds stack garbage, which can
    // read as "a START went out just now" on every retry, and the engine never starts (Q10,
    // 2026-10-31: stuck on "Starting…" until the app was reopened).
    m_invokedAt.invalidate();
    m_linked = QSettings(QDir::homePath() + "/settings.ini", QSettings::IniFormat).value("linked").toBool();
    m_retry.setSingleShot(true);
    m_retry.setInterval(500);
    connect(&m_retry, SIGNAL(timeout()), this, SLOT(tryConnect()));
    connect(&m_socket, SIGNAL(connected()), this, SLOT(socketConnected()));
    connect(&m_socket, SIGNAL(readyRead()), this, SLOT(readLines()));
    // One loss can fire both; lost() copes.
    connect(&m_socket, SIGNAL(disconnected()), this, SLOT(lost()));
    connect(&m_socket, SIGNAL(error(QAbstractSocket::SocketError)), this, SLOT(lost()));
}

// The socket is a member: its destructor aborts a live connection and emits disconnected(),
// which would reach lost() after m_retry is already gone.
EngineLink::~EngineLink()
{
    m_socket.disconnect(this);
}

bool EngineLink::connected() const { return m_connected; }
QVariantMap EngineLink::link() const { return m_link; }
QString EngineLink::conn() const { return m_conn; }
bool EngineLink::linked() const { return m_linked; }
QVariantMap EngineLink::failure() const { return m_failure; }

bool EngineLink::fullscreen() const { return m_fullscreen; }
QString EngineLink::openChat() const { return m_openChat; }

void EngineLink::start()
{
    // Each of these can fire twice for one change; setFullscreen acts only on a change.
    bb::cascades::Application *app = bb::cascades::Application::instance();
    connect(app, SIGNAL(fullscreen()), this, SLOT(onFullscreen()));
    connect(app, SIGNAL(thumbnail()), this, SLOT(onNotFullscreen()));
    connect(app, SIGNAL(invisible()), this, SLOT(onNotFullscreen()));
    tryConnect();
}

void EngineLink::onFullscreen() { setFullscreen(true); }
void EngineLink::onNotFullscreen() { setFullscreen(false); }

void EngineLink::setFullscreen(bool on)
{
    if (on == m_fullscreen)
        return;
    m_fullscreen = on;
    sendUiState();
    emit fullscreenChanged();
}

void EngineLink::setOpenChat(const QString &jid)
{
    if (jid == m_openChat)
        return;
    m_openChat = jid;
    sendUiState();
}

// openChat counts only while fullscreen: the notifier must speak up for a chat that's open but
// minimised. Skipped while down, and before linking, when the engine refuses it (unlinked) and
// that failure would hide the linking page's own; the hello and the link turning ready send it.
void EngineLink::sendUiState()
{
    if (!m_connected || !m_linked)
        return;
    QVariantMap args;
    args["fullscreen"] = m_fullscreen;
    args["openChat"] = m_fullscreen ? m_openChat : QString();
    request("ui.state", args);
}

void EngineLink::tryConnect()
{
    // Resets a socket left by the last try.
    m_socket.abort();
    int port = 0;
    QFile file(QDir::homePath() + "/engine.json");
    if (file.open(QIODevice::ReadOnly)) {
        JsonDataAccess json;
        const QVariant doc = json.loadFromBuffer(file.readAll());
        if (!json.hasError() && doc.type() == QVariant::Map) {
            const QVariantMap map = doc.toMap();
            port = map["port"].toInt();
            m_token = map["token"].toString();
        }
    }
    if (port == 0) {
        startEngine();
        m_retry.start();
        return;
    }
    m_socket.connectToHost(QHostAddress(QHostAddress::LocalHost), port);
}

void EngineLink::socketConnected()
{
    m_helloId = QString::number(++m_counter);
    QVariantMap hello;
    hello["@"] = m_helloId;
    hello["cmd"] = "hello";
    hello["token"] = m_token;
    hello["proto"] = 1;
    send(hello);
}

void EngineLink::startEngine()
{
    // A START that reaches an engine still exiting is lost (2.2: about 0.5 s), so another goes
    // soon; a spare one is harmless.
    if (m_invokedAt.isValid() && m_invokedAt.elapsed() < 1000)
        return;
    InvokeRequest request;
    request.setTarget("com.zsavajji.hyperberry.service");
    request.setAction("com.zsavajji.hyperberry.START");
    InvokeTargetReply *reply = m_invoker->invoke(request);
    m_invokedAt.start();
    qDebug() << "engine: START sent";
    if (!reply) {
        qWarning() << "engine: no reply from the system to START";
        return;
    }
    reply->setParent(this);
    connect(reply, SIGNAL(finished()), this, SLOT(invokeFinished()));
}

void EngineLink::invokeFinished()
{
    InvokeTargetReply *reply = qobject_cast<InvokeTargetReply *>(sender());
    if (!reply)
        return;
    if (reply->error() != InvokeReplyError::None)
        qWarning() << "engine: START failed, error" << reply->error();
    reply->deleteLater();
}

void EngineLink::readLines()
{
    while (m_socket.canReadLine()) {
        const QByteArray line = m_socket.readLine();
        JsonDataAccess json;
        const QVariant doc = json.loadFromBuffer(line);
        if (json.hasError() || doc.type() != QVariant::Map)
            continue;
        const QVariantMap map = doc.toMap();
        if (map.contains("@")) {
            const QString id = map["@"].toString();
            const bool ok = map["ok"].toBool();
            const QVariantMap error = map["error"].toMap();
            if (id == m_helloId) {
                if (ok) {
                    m_connected = true;
                    m_invokedAt.invalidate();
                    setLink(map["result"].toMap());
                    emit connectedChanged();
                    sendUiState(); // a restart loses this device's availability
                    // The engine notifies while the UI is closed, so it keeps this itself.
                    QVariantMap prefs;
                    prefs["notifyPreview"] = QSettings(QDir::homePath() + "/settings.ini", QSettings::IniFormat)
                                                 .value("notifyPreview", true).toBool();
                    request("prefs.set", prefs);
                    qDebug() << "engine: connected, link" << m_link["state"].toString()
                             << ", conn" << m_conn;
                } else {
                    qWarning() << "engine: hello refused:" << error["code"].toString()
                               << error["message"].toString();
                    m_socket.abort();
                    lost();
                    return;
                }
            } else {
                const QString cmd = m_pending.take(id);
                if (!ok)
                    fail(cmd, error["code"].toString(), error["message"].toString());
                else
                    emit replied(id, cmd, map["result"]);
            }
        } else if (map.contains("ev")) {
            const QString ev = map["ev"].toString();
            if (ev == "link")
                setLink(map);
            else if (ev == "conn")
                setConn(map["state"].toString());
            else if (ev == "chats")
                emit chatsChanged(map["jids"].toStringList());
            else if (ev == "msgs")
                emit msgsChanged(map["chat"].toString(), map["ids"].toStringList());
            else if (ev == "typing")
                emit typing(map["chat"].toString(), map["who"].toString(), map["on"].toBool(),
                            map["voice"].toBool());
            else if (ev == "presence")
                emit presence(map["chat"].toString(), map["online"].toBool(),
                              map["lastSeen"].toLongLong());
        }
    }
}

void EngineLink::setLink(const QVariantMap &link)
{
    m_link = link;
    const bool linked = link["state"].toString() == "ready";
    if (linked != m_linked) {
        m_linked = linked;
        QSettings settings(QDir::homePath() + "/settings.ini", QSettings::IniFormat);
        settings.setValue("linked", linked);
        settings.sync();
        emit linkedChanged();
        sendUiState();
    }
    emit linkChanged();
    setConn(link["conn"].toString());
}

void EngineLink::setConn(const QString &conn)
{
    if (conn == m_conn)
        return;
    m_conn = conn;
    emit connChanged();
}

void EngineLink::lost()
{
    // The socket's disconnected() and error() can both fire for one loss.
    if (m_retry.isActive())
        return;
    if (m_connected) {
        m_connected = false;
        emit connectedChanged();
        qDebug() << "engine: link lost";
    }
    const QHash<QString, QString> pending = m_pending;
    m_pending.clear();
    foreach (const QString &cmd, pending)
        fail(cmd, "no_engine", tr("The engine isn't running."));
    setConn("offline");
    // The m_link stays as it was: the next hello replaces it, so the UI doesn't flash back to
    // the linking page during a restart.
    startEngine();
    m_retry.start();
}

void EngineLink::fail(const QString &cmd, const QString &code, const QString &message)
{
    m_failure.clear();
    m_failure["cmd"] = cmd;
    m_failure["code"] = code;
    m_failure["message"] = message;
    emit failureChanged();
}

QString EngineLink::request(const QString &cmd, const QVariantMap &args)
{
    if (m_failure.value("cmd").toString() == cmd) {
        m_failure.clear();
        emit failureChanged();
    }
    if (!m_connected) {
        fail(cmd, "no_engine", tr("The engine isn't running."));
        return QString();
    }
    QVariantMap map = args;
    const QString id = QString::number(++m_counter);
    map["@"] = id;
    map["cmd"] = cmd;
    m_pending.insert(id, cmd);
    send(map);
    return id;
}

void EngineLink::send(const QVariantMap &map)
{
    QByteArray buf;
    JsonDataAccess json;
    json.saveToBuffer(QVariant(map), &buf);
    // JsonDataAccess may write indented JSON. JSON escapes newlines inside strings, so a raw one
    // can only be layout: dropping them keeps one object per line, as the engine's framing needs.
    buf.replace('\n', "");
    buf.replace('\r', "");
    buf.append('\n');
    m_socket.write(buf);
}
