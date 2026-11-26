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
static void sweepOrphans(QStringList entries, QDateTime started);

Outbox::Outbox(EngineLink *engine, QObject *parent)
    : QObject(parent)
    , m_engine(engine)
{
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(flush()));
    connect(engine, SIGNAL(replied(QString, QString, QVariant)), this, SLOT(onReplied(QString, QString, QVariant)));
    connect(engine, SIGNAL(failed(QString, QString, QString)), this, SLOT(onFailed(QString, QString, QString)));
    connect(engine, SIGNAL(linkedChanged()), this, SLOT(onLinked()));
    const QDateTime started = QDateTime::currentDateTime();
    QFile file(outboxFile());
    if (file.open(QIODevice::ReadOnly)) {
        JsonDataAccess json;
        const QVariant doc = json.loadFromBuffer(file.readAll());
        if (!json.hasError() && doc.type() == QVariant::List)
            m_entries = doc.toList();
    }
    // An entry made while the engine was down has args.original, the user's file, and its path is the copy in outbox/<id>/
    // once the copy is done. An app closed while it copied left a partial file there and the entry with the original's
    // path: copy it again, from a clean directory (copyMedia).
    QStringList ids;
    foreach (const QVariant &v, m_entries) {
        const QVariantMap entry = v.toMap();
        const QVariantMap a = entry["args"].toMap();
        const QString from = a["path"].toString();
        ids << entry["id"].toString();
        if (entry["cmd"].toString() == "media.send" && !a["original"].toString().isEmpty()
            && !from.startsWith(QDir::homePath() + "/outbox/"))
            startCopy(entry["id"].toString(), from);
    }
    QtConcurrent::run(sweepOrphans, ids, started);
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

// Removes the files of a directory (not the directory).
static void emptyDir(const QString &path)
{
    QDir dir(path);
    foreach (const QString &name, dir.entryList(QDir::Files | QDir::Hidden))
        dir.remove(name);
}

// Run on a worker thread: the file goes to its new name whole or not at all, so a partial copy never looks done. What the
// copy's directory holds goes first: the partial file of an app that was closed in the middle of the copy (a new send's
// directory is empty or not there).
static bool copyMedia(const QString &from, const QString &to)
{
    emptyDir(QFileInfo(to).path());
    const QString part = to + ".part";
    if (QDir().mkpath(QFileInfo(to).path()) && QFile::copy(from, part) && QFile::rename(part, to))
        return true;
    QFile::remove(part);
    return false;
}

// Run on a worker thread: the outbox/<id>/ directories that no entry owns (the ids) are leftovers, of a send taken back while it
// copied or of an app closed after the engine's answer and before the copy was deleted: the engine keeps its own file from
// its answer on, so no row of chats.db points into outbox/ and nothing else is checked. A directory touched since the app
// started (started) is one being made now, and stays.
static void sweepOrphans(QStringList entries, QDateTime started)
{
    const QString root = QDir::homePath() + "/outbox/";
    foreach (const QString &id, QDir(root).entryList(QDir::Dirs | QDir::NoDotAndDotDot)) {
        if (entries.contains(id) || QFileInfo(root + id).lastModified() >= started.addSecs(-2))
            continue;
        qDebug() << "outbox: removing the leftover" << (root + id);
        emptyDir(root + id);
        QDir().rmdir(root + id);
    }
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

QString Outbox::send(const QString &cmd, const QVariantMap &args)
{
    const QString id = newId();
    QVariantMap a = args;
    a[cmd == "msg.forward" || cmd == "story.reply" ? "newId" : "id"] = id;
    QVariantMap entry;
    entry["id"] = id;
    entry["cmd"] = cmd;
    entry["chat"] = chatOf(cmd, a);
    entry["ts"] = qlonglong(QDateTime::currentDateTime().toTime_t());
    // A media.send made with the engine up goes with the user's own path: the engine keeps its own link or copy of the file
    // when it takes the send. Made while it is down, the file is copied here, as the picked file may go and the cache's
    // PNG-as-JPEG be overwritten while the message waits. The copy is made on a worker (a big video would freeze the UI):
    // the entry is here at once, with the original's path and args.original, and onCopied switches it to the copy and lets it
    // go to the engine, or keeps the original's path if the copy failed.
    const bool copy = cmd == "media.send" && !m_engine->connected();
    if (copy)
        a["original"] = a["path"];
    entry["args"] = a;
    m_entries << entry;
    save();
    if (copy)
        startCopy(id, a["path"].toString());
    emit changed(entry["chat"].toString());
    post(entry);
    return id;
}

// An entry that waits for its file isn't sent: onCopied does it.
void Outbox::post(const QVariantMap &entry)
{
    const QString id = entry["id"].toString();
    if (!m_engine->connected() || m_copying.contains(id))
        return;
    // Quiet: a link that drops under it is no failure of the send (it goes at the next hello); onFailed shows the real ones.
    m_sent.insert(m_engine->requestQuiet(entry["cmd"].toString(), entry["args"].toMap()), id);
}

// The engine is up: what waited goes now, oldest first, after the hello. An entry stays until the engine answers: the app
// can be closed before, and the message's id makes the send again harmless.
void Outbox::flush()
{
    foreach (const QVariant &entry, m_entries)
        post(entry.toMap());
}

// An answer takes the entry off: the engine has the message, or has refused it for a reason that won't change, and its row
// or failure shows as for any send. "no_engine" is the link dropping under the request: the entry stays for the next hello.
void Outbox::onReplied(const QString &req, const QString &cmd, const QVariant &result)
{
    const QString id = m_sent.take(req);
    if (id.isEmpty())
        return;
    remove(id, false);
    emit replied(id, cmd, result);
}

void Outbox::onFailed(const QString &req, const QString &cmd, const QString &code)
{
    const QString id = m_sent.take(req);
    if (id.isEmpty() || code == "no_engine")
        return;
    remove(id, false);
    m_engine->fail(cmd, code, code); // the engine's refusal: the page's toast for cmd shows it, as for a request
}

// The link ended (a logout, the device removed): what waits belonged to that account (the user, 2026-11-26).
void Outbox::onLinked()
{
    while (!m_engine->linked() && !m_entries.isEmpty())
        remove(m_entries.first().toMap()["id"].toString(), true);
}

void Outbox::startCopy(const QString &id, const QString &from)
{
    const QString to = QDir::homePath() + "/outbox/" + id + "/" + QFileInfo(from).fileName();
    m_copying.insert(id, to);
    QFutureWatcher<bool> *watcher = new QFutureWatcher<bool>(this);
    watcher->setProperty("id", id);
    connect(watcher, SIGNAL(finished()), this, SLOT(onCopied()));
    watcher->setFuture(QtConcurrent::run(copyMedia, from, to));
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
        QVariantMap a = entry["args"].toMap();
        if (ok) {
            a["path"] = to; // the engine puts the row's path back to args.original once the message is sent
        } else {
            qWarning() << "outbox: could not copy" << a["path"].toString();
            QDir().rmdir(QFileInfo(to).path());
            a.remove("original"); // sent with the user's file, as one made with the engine up
        }
        entry["args"] = a;
        m_entries[i] = entry;
        save();
        emit changed(entry["chat"].toString());
        post(entry);
        return;
    }
    if (ok) { // taken back while it copied
        QFile::remove(to);
        QDir().rmdir(QFileInfo(to).path());
    }
}

// The UI's copy of the file goes with the entry: when the engine has answered it keeps its own, and a send taken back has none.
// A voice note's WAV goes only when taken back; once the engine has the note it deletes the WAV itself.
void Outbox::remove(const QString &id, bool takenBack)
{
    for (int i = 0; i < m_entries.size(); ++i) {
        const QVariantMap entry = m_entries[i].toMap();
        if (entry["id"].toString() != id)
            continue;
        const QString path = entry["args"].toMap()["path"].toString();
        if (path.startsWith(QDir::homePath() + "/outbox/")) {
            QFile::remove(path);
            QDir().rmdir(QFileInfo(path).path());
        } else if (takenBack && entry["cmd"].toString() == "voice.send") {
            QFile::remove(path);
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
