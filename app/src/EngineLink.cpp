#include "EngineLink.h"

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
{
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

void EngineLink::start()
{
    tryConnect();
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
    if (m_invokedAt.isValid() && m_invokedAt.elapsed() < 3000)
        return;
    InvokeRequest request;
    request.setTarget("com.zsavajji.hyperberry.service");
    request.setAction("com.zsavajji.hyperberry.START");
    InvokeTargetReply *reply = m_invoker->invoke(request);
    m_invokedAt.start();
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
            }
        } else if (map.contains("ev")) {
            const QString ev = map["ev"].toString();
            if (ev == "link")
                setLink(map);
            else if (ev == "conn")
                setConn(map["state"].toString());
            else if (ev == "chats")
                emit chatsChanged(map["jids"].toStringList());
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

void EngineLink::request(const QString &cmd, const QVariantMap &args)
{
    if (m_failure.value("cmd").toString() == cmd) {
        m_failure.clear();
        emit failureChanged();
    }
    if (!m_connected) {
        fail(cmd, "no_engine", tr("The engine isn't running."));
        return;
    }
    QVariantMap map = args;
    const QString id = QString::number(++m_counter);
    map["@"] = id;
    map["cmd"] = cmd;
    m_pending.insert(id, cmd);
    send(map);
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
