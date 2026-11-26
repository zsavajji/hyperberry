#ifndef OUTBOX_H
#define OUTBOX_H

#include <QHash>
#include <QObject>
#include <QString>
#include <QVariant>
#include <QVariantList>
#include <QVariantMap>

class EngineLink;

// Every send that makes a message of ours (msg.send, media.send, voice.send, sticker.send, poll.create, msg.forward,
// story.reply) goes through here, whether the engine is up or not (docs/protocol.md, "Sending: the outgoing pipeline"). An
// entry is a map of id (the message's, chosen here: the engine takes it as the row's id, so a send made again after a crash
// or a dropped link makes no second message), cmd, chat (where the message goes), args (the request's) and ts (unix seconds,
// when it was made); oldest first, kept in outbox.json so that closing the app doesn't lose them. send() posts an entry at
// once when the engine is up, else the next hello does; the engine's answer, any but "the engine isn't there", removes it,
// as its row is in chats.db from then on. MessageModel and ChatListModel show the entries meanwhile.
class Outbox : public QObject
{
    Q_OBJECT
public:
    Outbox(EngineLink *engine, QObject *parent = 0);

    // The kind (messages.kind) and text of the row the engine makes for an entry, as chats.db will have them: what its bubble
    // and the chat list's last line say. A forward's are its source's (read from chats.db, kind 0 and "" when it isn't there).
    static void row(const QVariantMap &entry, int *kind, QString *text);

    // Keeps cmd with args, sends it when the engine is up, and returns the message's new id, which is also in the entry's
    // args ("newId" for msg.forward and story.reply, else "id"). With the engine up a media.send goes with the user's own path:
    // the engine keeps its own link or copy of the file when it takes the send. Made while the engine is down, the file is
    // copied to outbox/<id>/ under its own name (the engine names a document by it) on a worker thread, as the picked file
    // may go and the cache's PNG-as-JPEG is overwritten by the next of its name while the message waits. The entry has the
    // original's path and args.original (the user's file) meanwhile, and no engine sends it; once the copy is done args.path
    // is the copy, and a copy that failed leaves the original's path and drops args.original. The engine gets args.original
    // with the copy as its path (the row's path goes back to the original once the message is sent). The copy goes with
    // the entry, at the engine's answer: the engine has its own file by then. An app closed in the middle of a copy left a
    // partial file in outbox/<id>/ and an entry with args.original and the original's path: when the outbox is loaded (the
    // constructor) each such entry is copying again, from a clean directory, on the same worker; with the original gone by
    // then it keeps the original's path, which the engine fails (copy_failed). Also then, on a worker, the outbox/<id>/
    // directories that no entry owns are removed.
    Q_INVOKABLE QString send(const QString &cmd, const QVariantMap &args);
    // Drops the entry and the copy of its file. takenBack: the send is cancelled, never made, so a voice note's WAV goes too
    // (the engine deletes it itself once it has the note).
    void remove(const QString &id, bool takenBack);
    bool has(const QString &id) const;
    // The entries going to chat, oldest first.
    QVariantList entries(const QString &chat) const;
    bool isEmpty() const { return m_entries.isEmpty(); }

signals:
    // The entries going to this chat changed.
    void changed(const QString &chat);
    // The engine accepted the send of message id (cmd, its result): for the callers that wait for it (story.reply).
    void replied(const QString &id, const QString &cmd, const QVariant &result);

private slots:
    void onCopied();
    void flush();
    void onReplied(const QString &req, const QString &cmd, const QVariant &result);
    void onFailed(const QString &req, const QString &cmd, const QString &code);
    void onLinked();

private:
    void save() const;
    // Sends the entry if the engine is up and its file is ready; otherwise the next hello (flush) does.
    void post(const QVariantMap &entry);
    // Copies the file from to outbox/<id>/ on a worker; onCopied ends it.
    void startCopy(const QString &id, const QString &from);

    EngineLink *m_engine;
    QVariantList m_entries;
    QHash<QString, QString> m_sent; // the requests out for an entry, and the entries' ids
    QHash<QString, QString> m_copying; // the entries whose file is being copied, and the copy's path
};

#endif
