#include "MessageModel.h"

#include <bb/cascades/Image>

#include "BigEmoji.h"
#include "EngineLink.h"
#include "MediaData.h"
#include "MessageKinds.h"
#include "Outbox.h"
#include "Pins.h"
#include "Sticker.h"
#include "colors.h"
#include "dates.h"
#include "Waveform.h"

#include <bb/data/JsonDataAccess>
#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QLocale>
#include <QSqlDatabase>
#include <QSqlError>
#include <QtEndian>

using namespace bb::utility::i18n;

// The chats.db side of MessageModel, out of MessageModel.cpp to keep that file short: the
// connection, the query and the reading of one row into a Msg.

// The connection ChatListModel opens, read-only: only the engine writes chats.db.
static const char *const kDb = "chats";

// The three QUOTED are the quoted message's out, sender and sender_name, by its primary key. The
// reactions columns are the message's reactions, all and ours (sender '' is us); the engine makes
// that table when it migrates chats.db, before the UI reads it. The last two are a sticker's name and
// whether it is one of the favourites (sticker_faves, also the engine's).
#define QUOTED(col) "(SELECT " col " FROM messages q WHERE q.chat = messages.chat AND q.id = messages.quote_id)"
static const char *const kColumns = "rowid, id, ts, out, state, sender, sender_name, kind, "
                                    "text, quote_preview, edited, deleted, "
                                    QUOTED("out") ", " QUOTED("sender") ", " QUOTED("sender_name") ", duration, waveform, media_state, played, "
                                    "thumb, width, height, size, path, "
                                    "(SELECT group_concat(emoji, ' ') FROM reactions r WHERE r.chat = messages.chat AND r.msg = messages.id), "
                                    "(SELECT emoji FROM reactions r WHERE r.chat = messages.chat AND r.msg = messages.id AND r.sender = ''), extra, starred, quote_id, "
                                    "tr, tr_lang, tr_from, sticker, "
                                    "EXISTS(SELECT 1 FROM sticker_faves f WHERE f.hash = messages.sticker)";
#undef QUOTED
// How many columns that is: read() takes them as a list, so a row that isn't in chats.db (an outbox entry's) is read the
// same way.
static const int kColumnCount = 34;

static QVariantList columns(QSqlQuery &q)
{
    QVariantList v;
    for (int i = 0; i < kColumnCount; ++i)
        v << q.value(i);
    return v;
}

MessageModel::Msg MessageModel::read(const QVariantList &q)
{
    Msg m;
    m.pending = false;
    m.rowid = q.value(0).toLongLong();
    m.id = q.value(1).toString();
    m.ts = q.value(2).toLongLong();
    const QDateTime when = QDateTime::fromTime_t(m.ts);
    m.day = when.date().toJulianDay();
    const bool out = q.value(3).toInt() != 0;
    const int kind = q.value(7).toInt();
    m.kind = kind;
    m.who = q.value(5).isNull() ? q.value(6).toString() : q.value(5).toString();
    m.data["readOnly"] = m_chat.endsWith("@newsletter"); // a channel: no reply, reaction, delete or vote (step 6.6g)
    m.data["album"] = QVariantList(); // MessageModel::albumData fills it for an album; never undefined in QML
    m.data["outbox"] = false; // pending() says true: the engine has no such row yet, so its menu offers what needs none
    const QString text = q.value(8).toString();
    const bool deleted = q.value(11).toInt() != 0;
    const QLocale &locale = m_locale;
    const QString time = locale.toString(when.time(), m_clock);
    // The wording of kinds 6, 7 and 9 to 14 (kind 6 and 13 are service lines).
    QString label, shown = text;
    const QVariantMap extra = MessageKinds::parse(q.value(26).toString());
    m.service = MessageKinds::fill(kind, m_chat, m.id, text, extra, out,
                                   q.value(6).toString(), time, locale, m.data, label, shown);
    if (m.service) {
        m.data["text"] = shown;
        m.data["bigEmoji"] = 0;
        m.data["html"] = QString(); // set on every row (below too), so QML never reads undefined
        return m;
    }
    // Photos, videos, stickers, audio files and documents are drawn by MediaBubble: no label, and a
    // document's text (its name) is the bubble's. A caption stays in the text.
    if (kind == 5 && !deleted && q.value(17).toInt() == 1)
        Sticker::fetch(m_engine, m_chat, m.id, m_asked); // a history sticker, asked once per page
    // Covered until a tap takes the cover off (unspoil), for as long as this chat stays open.
    const bool covered = m.data["spoiler"].toBool() && !deleted && !m_revealed.contains(m.id);
    m.data["spoiler"] = covered;
    if (MediaData::fill(m.data, m.id, deleted ? 0 : kind, text, q.value(19).toByteArray(),
                        q.value(20).toInt(), q.value(21).toInt(), q.value(22).toLongLong(),
                        q.value(15).toInt(), q.value(17).toInt(), q.value(23).toString(), locale,
                        covered)
        && (kind == 4 || kind == 8))
        shown = QString();
    if (deleted) {
        label = QString();
        shown = tr("This message was deleted");
    }
    QString sender;
    if (m_group && !out)
        sender = q.value(6).toString();

    m.data["id"] = m.id;
    m.data["out"] = out;
    m.data["state"] = q.value(4).toInt();
    m.data["edited"] = q.value(10).toInt() != 0;
    m.data["starred"] = q.value(27).toInt() != 0;
    // A sticker's name, which the menu's favourite action needs, and whether it is a favourite (the card's menu says which).
    m.data["sticker"] = q.value(32).toString();
    m.data["fave"] = q.value(33).toInt() != 0;
    m.data["deleted"] = deleted;
    // Translate (docs/plan/translate.md): texts and captions. An incoming one in a chat with a language shows its
    // translation once the engine has written it for that language; until then it wants one (wantTr), which is asked for
    // when the list draws the row (data()).
    const QString trLang = q.value(30).toString();
    const bool canTranslate = !deleted && (kind == 0 || kind == 1 || kind == 2) && !text.isEmpty();
    const bool translated = canTranslate && !out && !m_translate.isEmpty() && trLang == m_translate
                            && !q.value(29).toString().isEmpty();
    m.data["wantTr"] = canTranslate && !out && !m_translate.isEmpty() && trLang != m_translate;
    m.data["canTranslate"] = canTranslate;
    m.data["translated"] = translated;
    m.data["original"] = translated ? text : QString();
    if (translated)
        shown = q.value(29).toString();
    // The chip under the card: up to three different emoji, and how many reactions there are.
    const QStringList all = deleted ? QStringList() : q.value(24).toString().split(' ', QString::SkipEmptyParts);
    QStringList kinds;
    foreach (const QString &e, all) {
        if (kinds.size() < 3 && !kinds.contains(e))
            kinds << e;
    }
    m.data["reactions"] = kinds.join(QString());
    m.data["reactionCount"] = all.size();
    m.data["myReaction"] = deleted ? QString() : q.value(25).toString();
    // Delete for everyone: ours, sent, and inside WhatsApp's limit of about 2.5 days.
    // ponytail: read when the row is read, so a page left open for days can offer it a little late.
    const int state = q.value(4).toInt();
    m.data["revocable"] = out && !deleted && state >= 1 && state <= 3
                          && m_now - m.ts < 60 * 3600;
    // Edit: our own sent text or caption (a photo's or video's), inside WhatsApp's 15 minutes (the server allows 20).
    m.data["editable"] = out && !deleted && (kind == 0 || ((kind == 1 || kind == 2) && !text.isEmpty())) && state >= 1 && state <= 3
                         && m_now - m.ts < 15 * 60;
    // Forward: what the engine sends again (text, photo, video, voice, document, sticker, audio file); a
    // media row of ours waits for its upload (sending) and isn't one that failed.
    const bool media = kind == 1 || kind == 2 || kind == 3 || kind == 4 || kind == 5 || kind == 8;
    m.data["forwardable"] = !deleted && (kind == 0 || media) && !(out && media && (state == 0 || state == 4));
    m.data["time"] = time;
    // The long-press menu's title: the day in full and the time.
    m.data["stamp"] = longDay(when.date(), locale) + ", " + time;
    m.data["label"] = label;
    const bool voice = kind == 3 && !deleted;
    m.data["voice"] = voice;
    if (voice) {
        m.data["duration"] = q.value(15).toInt();
        m.data["mediaState"] = q.value(17).toInt();
        m.data["played"] = q.value(18).toInt() != 0;
        m.data["wave"] = Waveform::image(q.value(16).toByteArray());
        // As numbers: bytes could reach QML as a string, cut at the first 0 (a silent bar).
        QVariantList wave;
        foreach (char v, q.value(16).toByteArray())
            wave << int(v);
        m.data["waveData"] = wave;
    }
    m.data["text"] = shown;
    m.data["bigEmoji"] = kind == 0 && !deleted ? bigEmoji(shown) : 0; // 1 to 3 emoji alone: MessageCard draws them big
    // The same text with its mentions linked, "" for none (MessageKinds::html).
    m.data["html"] = MessageKinds::html(shown, extra);
    m.data["quote"] = q.value(9).toString();
    m.data["quoteId"] = q.value(28).toString(); // a tap on the quote jumps to it
    m.data["sender"] = sender;
    QString color;
    if (!sender.isEmpty()) {
        const QString key = q.value(5).isNull() ? sender : q.value(5).toString();
        color = pickColor(key, kSenders, kSenderCount);
    }
    m.data["senderColor"] = color;
    // In groups a quote says whose message it was, in that person's colour; ours is "You", in
    // the primary colour (quoteColor ""). ponytail: only when the quoted message is in
    // chats.db; store ContextInfo's participant (a quote_sender column) if older quotes need it.
    QString quoteName, quoteColor;
    if (m_group && !q.value(9).toString().isEmpty() && !q.value(12).isNull()) {
        if (q.value(12).toInt() != 0) {
            quoteName = tr("You");
        } else if (!q.value(14).toString().isEmpty()) {
            quoteName = q.value(14).toString();
            const QString key = q.value(13).isNull() ? quoteName : q.value(13).toString();
            quoteColor = pickColor(key, kSenders, kSenderCount);
        }
    }
    m.data["quoteName"] = quoteName;
    m.data["quoteColor"] = quoteColor;
    // A reply to a story, or a like on one of ours (docs/protocol.md, Story quotes): the quote shows the story's picture
    // and a tap on it plays the story. An Image even when empty, so a recycled card never keeps another row's.
    bool storyQuote = false;
    QString storyPoster;
    QByteArray storyThumb;
    if (!q.value(9).toString().isEmpty() && extra.value("story").toBool()) {
        storyQuote = true;
        storyPoster = extra.value("sposter").toString();
        storyThumb = QByteArray::fromBase64(extra.value("qthumb").toString().toLatin1());
    }
    m.data["quoteStory"] = storyQuote;
    m.data["storyPoster"] = storyPoster;
    m.data["hasQuoteThumb"] = !storyThumb.isEmpty();
    m.data["quoteThumb"] = QVariant::fromValue(storyThumb.isEmpty() ? bb::cascades::Image() : bb::cascades::Image(storyThumb));
    return m;
}

static QSqlDatabase database();

// The language incoming messages are shown in, "" for off (and when chats.db has no such row).
QString MessageModel::readTranslate() const
{
    QSqlDatabase db = database();
    if (!db.isOpen())
        return QString();
    QSqlQuery q(db);
    q.prepare("SELECT translate FROM chats WHERE jid = ?");
    q.addBindValue(m_chat);
    return q.exec() && q.next() ? q.value(0).toString() : QString();
}

// A row the list draws that still wants its translation is queued, once, and the queue goes out 200 ms later, so a
// screenful is one request. Only what is drawn is asked for: the list asks data() for the rows on screen and the few it
// keeps ready around them, and a whole page at once kept the service busy.
void MessageModel::wantTranslation(const Msg &m)
{
    if (!m.data.value("wantTr").toBool() || m_askedTr.contains(m.id) || m_toTranslate.contains(m.id))
        return;
    m_toTranslate << m.id;
    if (!m_trSoon.isActive())
        m_trSoon.start(200);
}

// What wantTranslation queued goes to the engine in requests of at most 200 ids. The engine
// writes each translation as it lands and says so with a msgs event, which reads the row again,
// now with its tr_lang: nothing is queued twice. A failure forgets the ids (onFailed); one timer retries.
void MessageModel::askTranslate()
{
    QStringList ids = m_toTranslate;
    m_toTranslate.clear();
    while (!ids.isEmpty() && m_engine) {
        const QStringList part = ids.mid(0, 200);
        ids = ids.mid(part.size());
        QVariantMap args;
        args["chat"] = m_chat;
        QVariantList list;
        foreach (const QString &id, part)
            list << id;
        args["ids"] = list;
        args["to"] = m_translate;
        const QString request = m_engine->requestQuiet("msg.translate", args);
        if (request.isEmpty())
            return; // not connected: the link coming back reads the rows again
        m_translating.insert(request, part);
        if (m_translating.size() == 1)
            emit translatingChanged();
        foreach (const QString &id, part)
            m_askedTr.insert(id);
    }
}

// A day's long name is asked once, not once per message of that day.
QString MessageModel::longDay(const QDate &date, const QLocale &locale)
{
    const int day = date.toJulianDay();
    QHash<int, QString>::const_iterator it = m_longDays.constFind(day);
    if (it != m_longDays.constEnd())
        return it.value();
    const QString label = capitalized(locale.toString(date, QLocale::LongFormat));
    m_longDays.insert(day, label);
    return label;
}

// The read-only connection, opened on first use. Not open before the first link: the engine
// hasn't made chats.db yet.
static QSqlDatabase database()
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isValid()) {
        db = QSqlDatabase::addDatabase("QSQLITE", kDb);
        db.setDatabaseName(QDir::homePath() + "/chats.db");
        db.setConnectOptions("QSQLITE_OPEN_READONLY");
    }
    if (!db.isOpen())
        db.open();
    return db;
}

// The chat's unread count in chats.db, 0 when it has no row (a contact with no chat yet).
int MessageModel::unread() const
{
    QSqlDatabase db = database();
    if (!db.isOpen())
        return 0;
    QSqlQuery q(db);
    q.prepare("SELECT unread FROM chats WHERE jid = ?");
    q.addBindValue(m_chat);
    return q.exec() && q.next() ? q.value(0).toInt() : 0;
}

// The times select would read, with no row built: what deciding a page's size needs.
QList<qint64> MessageModel::pageTimes(const QString &where, const QVariantList &args, int limit)
{
    QList<qint64> times;
    QSqlDatabase db = database();
    if (!db.isOpen())
        return times;
    QSqlQuery q(db);
    q.prepare(QString("SELECT ts FROM messages WHERE chat = ? %1 ORDER BY ts DESC, rowid DESC LIMIT %2")
                  .arg(where).arg(limit));
    q.addBindValue(m_chat);
    foreach (const QVariant &a, args)
        q.addBindValue(a);
    if (q.exec()) {
        while (q.next())
            times << q.value(0).toLongLong();
    }
    return times;
}

// Runs the messages query of this chat: "where" adds conditions on top of chat = ?, args are
// bound after it. Newest first, then reversed to the order the list shows.
QList<MessageModel::Msg> MessageModel::select(const QString &where, const QVariantList &args,
                                              int limit)
{
    QList<Msg> rows;
    QSqlDatabase db = database();
    if (!db.isOpen())
        return rows;
    tick();
    QSqlQuery q(db);
    q.prepare(QString("SELECT %1 FROM messages WHERE chat = ? %2 ORDER BY ts DESC, rowid DESC "
                      "LIMIT %3").arg(kColumns, where).arg(limit));
    q.addBindValue(m_chat);
    foreach (const QVariant &a, args)
        q.addBindValue(a);
    if (!q.exec()) {
        qWarning() << "messages:" << q.lastError().text();
        return rows;
    }
    while (q.next())
        rows.prepend(read(columns(q)));
    return rows;
}

// The locale, the clock and the time that read() takes the rows' times and limits from, as of now.
void MessageModel::tick()
{
    m_locale = m_region.locale();
    m_clock = timeFormat(m_locale, DateFormat::Short);
    m_now = QDateTime::currentDateTime().toTime_t();
}

// One row of any chat, as read() takes it; empty when there is none.
QVariantList MessageModel::rowValues(const QString &chat, const QString &id)
{
    QSqlDatabase db = database();
    if (!db.isOpen())
        return QVariantList();
    QSqlQuery q(db);
    q.prepare(QString("SELECT %1 FROM messages WHERE chat = ? AND id = ?").arg(kColumns));
    q.addBindValue(chat);
    q.addBindValue(id);
    return q.exec() && q.next() ? columns(q) : QVariantList();
}

// A WAV's length in seconds: its size over the byte rate in its header (the 44 bytes' own count is neglected).
static int wavSeconds(const QString &path)
{
    QFile file(path);
    if (!file.open(QIODevice::ReadOnly))
        return 0;
    const QByteArray head = file.read(44);
    const quint32 rate = head.size() < 44 ? 0 : qFromLittleEndian<quint32>(
        reinterpret_cast<const uchar *>(head.constData()) + 28);
    return rate ? int(file.size() / rate) : 0;
}

// Where a sticker's file is on the phone, "" when it isn't: a downloaded message's, else the copy stickers.file made, named
// by the hash's hex (the engine's stickerFile).
static QString stickerFile(const QString &hash)
{
    QStringList paths;
    QSqlDatabase db = database();
    if (db.isOpen()) {
        QSqlQuery q(db);
        q.prepare("SELECT path FROM messages WHERE sticker = ? AND media_state = 2 AND path IS NOT NULL ORDER BY ts DESC");
        q.addBindValue(hash);
        if (q.exec()) {
            while (q.next())
                paths << q.value(0).toString();
        }
    }
    const QString copy = QDir::homePath() + "/stickers/" + QString::fromLatin1(QByteArray::fromBase64(hash.toLatin1()).toHex());
    paths << copy + ".webp" << copy + ".was";
    foreach (const QString &path, paths) {
        if (QFile::exists(path))
            return path;
    }
    return QString();
}

// The rows of this chat's outbox, as the engine will have them in chats.db once it takes the sends (docs/protocol.md): ours,
// sending, state 0, made by read() from the columns of kColumns, so every key a card reads is set. db is the loaded rows of
// chats.db: an entry whose id is among them is the engine's already, and its row shows instead. Of each kind:
// msg.send and story.reply are a text (the story reply's quote of the story, and mentions' links, wait for the engine's
// row); media.send is a photo (the thumbnail, size and caption it carries), a video, an audio file or a document by its
// extension, with the file's size; voice.send is a voice row with the WAV and its length; sticker.send a sticker row with its
// file, a text "Sticker" when the file isn't on the phone; poll.create a poll row with its question and options; msg.forward
// the source's row with the new id.
// ponytail: a voice row has a flat waveform, and plays only once the engine has it (voice.play wants its row).
QList<MessageModel::Msg> MessageModel::pending(const QList<Msg> &db)
{
    QList<Msg> rows;
    if (!m_outbox)
        return rows;
    const QVariantList entries = m_outbox->entries(m_chat);
    if (entries.isEmpty())
        return rows;
    tick();
    QSet<QString> have;
    foreach (const Msg &m, db)
        have.insert(m.id);
    foreach (const QVariant &entry, entries) {
        const QVariantMap e = entry.toMap();
        const QString cmd = e["cmd"].toString();
        const QVariantMap a = e["args"].toMap();
        if (have.contains(e["id"].toString()))
            continue;
        int kind;
        QString text;
        Outbox::row(e, &kind, &text);
        QVariantList v;
        if (cmd == "msg.forward")
            v = rowValues(a["chat"].toString(), a["id"].toString());
        const bool forward = !v.isEmpty();
        if (forward) {
            // The source's, as the engine copies it (forwardCopy): not its sender, quote, reactions, star or translation.
            static const int others[] = { 5, 6, 9, 10, 11, 12, 13, 14, 18, 24, 25, 27, 28, 29, 30, 31, 33 };
            for (uint i = 0; i < sizeof others / sizeof *others; ++i)
                v[others[i]] = QVariant();
            v[26] = QString("{\"forwarded\":\"1\"}");
        } else {
            for (int i = 0; i < kColumnCount; ++i)
                v << QVariant();
            v[7] = kind;
            v[8] = cmd == "msg.forward" ? Pins::summary(0, QString()) : text; // a forward's source isn't in chats.db
            const QString path = a["path"].toString();
            if (cmd == "media.send") {
                v[17] = 2; // media_state: the file is here
                v[22] = QFileInfo(path).size();
                v[23] = path;
                if (kind == 1) {
                    v[19] = QByteArray::fromBase64(a["thumb"].toString().toLatin1());
                    v[20] = a["width"].toInt();
                    v[21] = a["height"].toInt();
                }
            } else if (cmd == "voice.send") {
                v[15] = wavSeconds(path);
                v[17] = 2;
                v[23] = path;
            } else if (cmd == "sticker.send") {
                const QString file = stickerFile(a["hash"].toString());
                if (file.isEmpty()) {
                    v[7] = 0;
                    v[8] = Pins::summary(5, QString());
                } else {
                    v[17] = 2;
                    v[23] = file;
                    v[32] = a["hash"].toString();
                }
            } else if (cmd == "poll.create") {
                QVariantMap extra;
                extra["options"] = a["options"];
                extra["max"] = a["multiple"].toBool() ? 0 : 1;
                QByteArray json;
                bb::data::JsonDataAccess().saveToBuffer(extra, &json);
                v[26] = QString::fromUtf8(json);
            }
            // A quote of a loaded row: its preview, and whose it was for a group's quote name.
            const QString quote = a["quote"].toString();
            foreach (const Msg &m, db) {
                if (m.id != quote)
                    continue;
                v[9] = Pins::summary(m.kind, m.data["text"].toString());
                v[12] = m.data["out"].toBool() ? 1 : 0;
                v[13] = m.who;
                v[14] = m.data["sender"].toString();
                v[28] = quote;
            }
        }
        v[1] = e["id"].toString();
        v[2] = e["ts"].toLongLong();
        v[3] = 1;
        v[4] = 0;
        Msg m = read(v);
        m.pending = true;
        m.data["outbox"] = true;
        rows << m;
    }
    return rows;
}
