#ifndef ENGINELINK_H
#define ENGINELINK_H

#include <bb/system/InvokeManager>
#include <QAbstractSocket>
#include <QElapsedTimer>
#include <QHash>
#include <QObject>
#include <QString>
#include <QStringList>
#include <QTcpSocket>
#include <QTimer>
#include <QVariantMap>

// The UI's side of the engine link, following docs/protocol.md. It reconnects and restarts the
// engine by itself: it retries every 500 ms and sends START at most every 1 s.
class EngineLink : public QObject
{
    Q_OBJECT
    // True once the engine answered hello; false while it's down or restarting.
    Q_PROPERTY(bool connected READ connected NOTIFY connectedChanged)
    // The link body of docs/protocol.md (state, conn, qr, code, me, name), as last received.
    Q_PROPERTY(QVariantMap link READ link NOTIFY linkChanged)
    // The connection to WhatsApp: offline, connecting or ready.
    Q_PROPERTY(QString conn READ conn NOTIFY connChanged)
    // True when the last link state was ready. Kept in settings.ini, so a cold start shows the
    // chat list at once, before the engine answers (PLAN.md §12).
    Q_PROPERTY(bool linked READ linked NOTIFY linkedChanged)
    // The last request that failed, {cmd, code, message}: the engine's error code, or "no_engine"
    // when the engine was down or went down before replying; message is English, for logs. A
    // property so that pages bind to it and nothing stays connected to a destroyed page. Empty
    // until a failure; request() of the same cmd clears it.
    Q_PROPERTY(QVariantMap failure READ failure NOTIFY failureChanged)
public:
    explicit EngineLink(QObject *parent = 0);
    ~EngineLink();

    bool connected() const;
    QVariantMap link() const;
    QString conn() const;
    bool linked() const;
    QVariantMap failure() const;

    // Connects, starting the engine first when it isn't running. Call once, from main().
    void start();


    // Sends a request (docs/protocol.md "Requests"); a failure shows in failure.
    Q_INVOKABLE void request(const QString &cmd, const QVariantMap &args);

signals:
    void connectedChanged();
    void linkChanged();
    void connChanged();
    // A chats event: these rows of chats.db changed.
    void chatsChanged(const QStringList &jids);
    // A msgs event: these rows of the chat's messages changed or disappeared.
    void msgsChanged(const QString &chat, const QStringList &ids);
    // A typing event: who ("" in 1:1) started or stopped typing in the chat; voice while recording.
    void typing(const QString &chat, const QString &who, bool on, bool voice);
    // A presence event; lastSeen in unix seconds, 0 when hidden.
    void presence(const QString &chat, bool online, qint64 lastSeen);
    void linkedChanged();
    void failureChanged();

private slots:
    void tryConnect();
    void socketConnected();
    void readLines();
    void lost();
    void invokeFinished();

private:
    void startEngine();
    void send(const QVariantMap &map);
    void setLink(const QVariantMap &link);
    void setConn(const QString &conn);
    void fail(const QString &cmd, const QString &code, const QString &message);

    QTcpSocket m_socket;
    QTimer m_retry;
    bb::system::InvokeManager *m_invoker;
    QElapsedTimer m_invokedAt;
    QString m_token;
    int m_counter;
    QString m_helloId;
    QHash<QString, QString> m_pending;
    bool m_connected;
    QVariantMap m_link;
    QString m_conn;
    bool m_linked;
    QVariantMap m_failure;
};

#endif
