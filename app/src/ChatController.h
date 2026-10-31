#ifndef CHATCONTROLLER_H
#define CHATCONTROLLER_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <QObject>
#include <QPointer>
#include <QString>
#include <QTimer>
#include <QVariantMap>

class EngineLink;
class MessageModel;

// One open chat page (PLAN.md §7, step 3.1), made by QML in the page's attachedObjects so it
// dies with the page. It owns the page's MessageModel, tells the engine the chat is open, turns
// typing and presence events into the title bar's status line, and (step 3.2) sends messages and
// our own typing state.
class ChatController : public QObject
{
    Q_OBJECT
    // The EngineLink (a QObject so QML can pass the context property). Not named engine: in
    // `engine: engine` QML would read the controller's own property, not the context's.
    Q_PROPERTY(QObject *link READ link WRITE setLink)
    Q_PROPERTY(bb::cascades::DataModel *messages READ messages NOTIFY messagesChanged)
    // The title bar's status line: "typing…", "online", "last seen …" or "".
    Q_PROPERTY(QString status READ status NOTIFY statusChanged)
public:
    explicit ChatController(QObject *parent = 0);
    ~ChatController();

    QObject *link() const;
    void setLink(QObject *link);
    bb::cascades::DataModel *messages() const;
    QString status() const;

    // chat is a ChatListModel row (jid, kind). Once; later calls do nothing.
    Q_INVOKABLE void open(const QVariantMap &chat);
    Q_INVOKABLE void nearTop(const QVariantList &firstVisible);
    // False when the engine isn't connected: nothing was sent, so QML keeps the text.
    Q_INVOKABLE bool send(const QString &text);
    // Called on every change of the composer's text; sends our typing on and off.
    Q_INVOKABLE void composing(const QString &text);

signals:
    void messagesChanged();
    void statusChanged();

private slots:
    void onTyping(const QString &chat, const QString &who, bool on, bool voice);
    void onPresence(const QString &chat, bool online, qint64 lastSeen);
    void onConnected();
    void typingIdle();
    void composePaused();

private:
    void sendOpen();
    void setTyping(const QString &text);
    void setComposing(bool on);

    QPointer<EngineLink> m_engine;
    MessageModel *m_messages;
    QString m_jid;
    bool m_group;
    QString m_typing;
    QString m_presence;
    QTimer m_idle;
    // Our own typing state, apart from the other person's above.
    bool m_composing;
    QTimer m_composeRepeat;
    QTimer m_composePause;
    bb::system::LocaleHandler m_region;
};

#endif
