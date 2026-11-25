#ifndef OUTBOX_H
#define OUTBOX_H

#include <QObject>
#include <QString>
#include <QVariantList>
#include <QVariantMap>

// The sends made while the app's link to the engine is down, kept in outbox.json so that closing the app doesn't lose
// them (docs/protocol.md, "Sending while WhatsApp is offline"). An entry is a map of id (the message's, chosen here: the
// engine takes it as the row's id, so a send made again after a crash makes no second message), cmd, chat (where the
// message goes), args (the request's) and ts (unix seconds, when it was made); oldest first. EngineLink adds them while
// it is down and sends them once the engine has answered hello; MessageModel and ChatListModel show them meanwhile.
class Outbox : public QObject
{
    Q_OBJECT
public:
    explicit Outbox(QObject *parent = 0);

    // Whether cmd is kept here while the engine is down: the commands that make a message of ours.
    static bool takes(const QString &cmd);
    // The kind (messages.kind) and text of the row the engine makes for an entry, as chats.db will have them: what its bubble
    // and the chat list's last line say. A forward's are its source's (read from chats.db, kind 0 and "" when it isn't there).
    static void row(const QVariantMap &entry, int *kind, QString *text);

    // Keeps cmd with args and returns the message's new id, which is also in the entry's args ("newId" for msg.forward and
    // story.reply, else "id"). A media.send's file is copied to outbox/<id>/ under its own name (the engine names a
    // document by it): the picked file may go and the cache's PNG-as-JPEG is overwritten by the next of its name while the
    // message waits.
    // ponytail: a big video is copied on the UI thread; a hard link or a worker if that stalls.
    QString add(const QString &cmd, const QVariantMap &args);
    // Drops the entry. files: also the copy of its file and a voice note's WAV (a send taken back). Not when the engine took
    // the message: its row points at them from then on (and the engine deletes the WAV itself).
    void remove(const QString &id, bool files);
    bool has(const QString &id) const;
    // The entries going to chat, oldest first.
    QVariantList entries(const QString &chat) const;
    // All of them, oldest first.
    QVariantList all() const { return m_entries; }
    bool isEmpty() const { return m_entries.isEmpty(); }

signals:
    // The entries going to this chat changed.
    void changed(const QString &chat);

private:
    void save() const;

    QVariantList m_entries;
};

#endif
