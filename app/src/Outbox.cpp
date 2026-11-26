#include "Outbox.h"

#include "EngineLink.h"

#include <bb/data/JsonDataAccess>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QFutureWatcher>
#include <QRegExp>
#include <QSqlDatabase>
#include <QSqlQuery>
#include <QUuid>
#include <QtConcurrentRun>
#include <cstdio>

using namespace bb::data;

static QString outboxFile() { return QDir::homePath() + "/outbox.json"; }

Outbox::Outbox(QObject *parent)
    : QObject(parent)
{
    QFile file(outboxFile());
    if (!file.open(QIODevice::ReadOnly))
        return;
    JsonDataAccess json;
    const QVariant doc = json.loadFromBuffer(file.readAll());
    if (!json.hasError() && doc.type() == QVariant::List)
        m_entries = doc.toList();
}

bool Outbox::takes(const QString &cmd)
{
    return cmd == "msg.send" || cmd == "media.send" || cmd == "voice.send" || cmd == "sticker.send"
           || cmd == "poll.create" || cmd == "msg.forward" || cmd == "story.reply";
}

// By the extension, as the engine's sendKind says (engine/media_send.go): photo 1, video 2, audio file 8, document 4;
// `file` (As file) makes a document of any.
static int mediaKind(const QVariantMap &args)
{
    const QString ext = QFileInfo(args["path"].toString()).suffix().toLower();
    if (args["file"].toBool())
        return 4;
    if (ext == "jpg" || ext == "jpeg")
        return 1;
    if (ext == "mp4" || ext == "3gp" || ext == "mov")
        return 2;
    if (ext == "mp3" || ext == "m4a" || ext == "aac" || ext == "amr" || ext == "ogg" || ext == "wav")
        return 8;
    return 4;
}

void Outbox::row(const QVariantMap &entry, int *kind, QString *text)
{
    const QString cmd = entry["cmd"].toString();
    const QVariantMap a = entry["args"].toMap();
    *kind = 0;
    *text = a["text"].toString(); // msg.send, story.reply
    if (cmd == "media.send") {
        *kind = mediaKind(a);
        // A photo's or a video's caption; a document is named by its file; an audio file has no text.
        *text = *kind == 4 ? QFileInfo(a["path"].toString()).fileName()
                           : *kind == 8 ? QString() : a["caption"].toString();
    } else if (cmd == "voice.send") {
        *kind = 3;
        text->clear();
    } else if (cmd == "sticker.send") {
        *kind = 5;
        text->clear();
    } else if (cmd == "poll.create") {
        *kind = 11;
        *text = a["question"].toString();
    } else if (cmd == "msg.forward") {
        text->clear();
        QSqlDatabase db = QSqlDatabase::database("chats", false);
        if (!db.isOpen())
            return;
        QSqlQuery q(db);
        q.prepare("SELECT kind, text FROM messages WHERE chat = ? AND id = ?");
        q.addBindValue(a["chat"].toString());
        q.addBindValue(a["id"].toString());
        if (q.exec() && q.next()) {
            *kind = q.value(0).toInt();
            *text = q.value(1).toString();
        }
    }
}

// Run on a worker thread: the file goes to its new name whole or not at all, so a partial copy never looks done.
static bool copyMedia(const QString &from, const QString &to)
{
    const QString part = to + ".part";
    if (QDir().mkpath(QFileInfo(to).path()) && QFile::copy(from, part) && QFile::rename(part, to))
        return true;
    QFile::remove(part);
    return false;
}

// The engine's own ids look so (WhatsApp Web's too): "3EB0" and 16 hex digits.
static QString newId()
{
    return "3EB0" + QUuid::createUuid().toString().remove(QRegExp("[{}-]")).toUpper().left(16);
}

// The chat the message goes to: a forward's is its target, a story reply's the poster's.
static QString chatOf(const QString &cmd, const QVariantMap &args)
{
    return args[cmd == "msg.forward" ? "to" : cmd == "story.reply" ? "poster" : "chat"].toString();
}

QString Outbox::add(const QString &cmd, const QVariantMap &args)
{
    const QString id = newId();
    QVariantMap a = args;
    a[cmd == "msg.forward" || cmd == "story.reply" ? "newId" : "id"] = id;
    QVariantMap entry;
    entry["id"] = id;
    entry["cmd"] = cmd;
    entry["chat"] = chatOf(cmd, a);
    entry["args"] = a;
    entry["ts"] = qlonglong(QDateTime::currentDateTime().toTime_t());
    m_entries << entry;
    save();
    if (cmd == "media.send") {
        // The copy is made on a worker (a big video would freeze the UI): the entry is here at once, with the original's
        // path, and onCopied switches it to the copy and lets it go to the engine, or keeps the original if the copy failed.
        const QString from = a["path"].toString();
        const QString to = QDir::homePath() + "/outbox/" + id + "/" + QFileInfo(from).fileName();
        m_copying.insert(id, to);
        QFutureWatcher<bool> *watcher = new QFutureWatcher<bool>(this);
        watcher->setProperty("id", id);
        connect(watcher, SIGNAL(finished()), this, SLOT(onCopied()));
        watcher->setFuture(QtConcurrent::run(copyMedia, from, to));
    }
    emit changed(entry["chat"].toString());
    return id;
}

void Outbox::onCopied()
{
    QFutureWatcher<bool> *watcher = static_cast<QFutureWatcher<bool> *>(sender());
    watcher->deleteLater();
    const QString id = watcher->property("id").toString();
    const QString to = m_copying.take(id);
    const bool ok = watcher->result();
    for (int i = 0; i < m_entries.size(); ++i) {
        QVariantMap entry = m_entries[i].toMap();
        if (entry["id"].toString() != id)
            continue;
        if (ok) {
            QVariantMap a = entry["args"].toMap();
            a["original"] = a["path"]; // the engine puts the row's path back to it once the message is sent, and removes the copy
            a["path"] = to;
            entry["args"] = a;
            m_entries[i] = entry;
            save();
            emit changed(entry["chat"].toString());
        } else {
            qWarning() << "outbox: could not copy" << entry["args"].toMap()["path"].toString();
            QDir().rmdir(QFileInfo(to).path());
        }
        emit copied(id);
        return;
    }
    if (ok) { // taken back while it copied
        QFile::remove(to);
        QDir().rmdir(QFileInfo(to).path());
    }
}

void Outbox::remove(const QString &id, bool files)
{
    for (int i = 0; i < m_entries.size(); ++i) {
        const QVariantMap entry = m_entries[i].toMap();
        if (entry["id"].toString() != id)
            continue;
        if (files) {
            const QString path = entry["args"].toMap()["path"].toString();
            if (entry["cmd"].toString() == "voice.send") {
                QFile::remove(path);
            } else if (path.startsWith(QDir::homePath() + "/outbox/")) {
                QFile::remove(path);
                QDir().rmdir(QFileInfo(path).path());
            }
        }
        m_entries.removeAt(i);
        save();
        emit changed(entry["chat"].toString());
        return;
    }
}

bool Outbox::has(const QString &id) const
{
    foreach (const QVariant &entry, m_entries) {
        if (entry.toMap()["id"].toString() == id)
            return true;
    }
    return false;
}

QVariantList Outbox::entries(const QString &chat) const
{
    QVariantList list;
    foreach (const QVariant &entry, m_entries) {
        if (entry.toMap()["chat"].toString() == chat)
            list << entry;
    }
    return list;
}

// Written whole to outbox.json.tmp and renamed over the file, so a kill in the middle leaves the old one.
void Outbox::save() const
{
    QByteArray buf;
    JsonDataAccess json;
    json.saveToBuffer(wholeNumbers(m_entries), &buf);
    const QString path = outboxFile();
    QFile file(path + ".tmp");
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate) || file.write(buf) != buf.size()
        || !file.flush()) {
        qWarning() << "outbox: could not write" << file.fileName();
        return;
    }
    file.close();
    if (std::rename(QFile::encodeName(file.fileName()).constData(), QFile::encodeName(path).constData()) != 0)
        qWarning() << "outbox: could not rename" << file.fileName();
}
