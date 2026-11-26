#ifndef OUTBOX_H
#define OUTBOX_H

#include <QHash>
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
    // document by it) on a worker thread: the picked file may go and the cache's PNG-as-JPEG is overwritten by the next of
    // its name while the message waits. The entry has the original's path meanwhile (copying(id)); once the copy is done
    // args.path is the copy and args.original the file it was copied from (the engine points the row's path back to it
    // once it has sent the message, if it is still there, and removes the copy: engine/retry.go, dropSendFiles), and
    // copied(id) says so; a copy that failed leaves the original's path. EngineLink doesn't send an entry that is copying.
    // An app closed in the middle of a copy left a partial file in outbox/<id>/ and an entry with no args.original: when the
    // outbox is loaded (the constructor) each such entry is copying again, from a clean directory, on the same worker; with the
    // original gone by then it keeps the original's path, which the engine fails and Retry/Cancel handle. Also then, on a
    // worker, the outbox/<id>/ directories that no entry owns and no row of chats.db points into are removed.
    QString add(const QString &cmd, const QVariantMap &args);
    // Whether the copy of the entry's file is still running.
    bool copying(const QString &id) const { return m_copying.contains(id); }
    // Drops the entry. files: also the copy of its file and a voice note's WAV (a send taken back). Not when the engine took
    // the message: its row points at them from then on (and the engine deletes the WAV itself; the copy it removes once the
    // message is sent, when the original is still there).
    void remove(const QString &id, bool files);
    // Drops every entry with its files: the link ended (a logout, the device removed), and what waits belonged to that
    // account.
    void clear();
    bool has(const QString &id) const;
    // The entries going to chat, oldest first.
    QVariantList entries(const QString &chat) const;
    // All of them, oldest first.
    QVariantList all() const { return m_entries; }
    bool isEmpty() const { return m_entries.isEmpty(); }

signals:
    // The entries going to this chat changed.
    void changed(const QString &chat);
    // The copy of the entry's file ended, made or not: it can go to the engine.
    void copied(const QString &id);

private slots:
    void onCopied();

private:
    void save() const;
    // Copies the file from to outbox/<id>/ on a worker; onCopied ends it.
    void startCopy(const QString &id, const QString &from);

    QVariantList m_entries;
    QHash<QString, QString> m_copying; // the entries whose file is being copied, and the copy's path
};

#endif
