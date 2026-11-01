#include "MessageModel.h"

#include "EngineLink.h"
#include "MediaData.h"
#include "Moves.h"
#include "colors.h"
#include "dates.h"
#include "Waveform.h"

#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QHash>
#include <QLocale>
#include <QPair>
#include <QSet>
#include <QSqlDatabase>
#include <QSqlError>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// The connection ChatListModel opens, read-only: only the engine writes chats.db.
static const char *const kDb = "chats";

static const int kPage = 50;

// The last three are the quoted message's out, sender and sender_name, by its primary key.
#define QUOTED(col) "(SELECT " col " FROM messages q WHERE q.chat = messages.chat AND q.id = messages.quote_id)"
static const char *const kColumns = "rowid, id, ts, out, state, sender, sender_name, kind, "
                                    "text, quote_preview, edited, deleted, "
                                    QUOTED("out") ", " QUOTED("sender") ", " QUOTED("sender_name") ", duration, waveform, media_state, played, "
                                    "thumb, width, height, size, path";
#undef QUOTED

bool MessageModel::lessThan(const Msg &a, const Msg &b)
{
    return a.ts < b.ts || (a.ts == b.ts && a.rowid < b.rowid);
}

MessageModel::MessageModel(EngineLink *engine, const QString &chat, bool group, QObject *parent)
    : DataModel(parent)
    , m_engine(engine)
    , m_chat(chat)
    , m_group(group)
    , m_atStart(false)
    , m_days(RelativeDateFormat::NonTimeCritical)
    , m_region(bb::system::LocaleType::Region)
{
    m_days.applyUserRegionLocale();
    connect(engine, SIGNAL(msgsChanged(QString, QStringList)), this,
            SLOT(onMsgs(QString, QStringList)));
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(onConnected()));
    loadPage();
}

int MessageModel::childCount(const QVariantList &indexPath)
{
    return indexPath.isEmpty() ? m_entries.size() : 0;
}

bool MessageModel::hasChildren(const QVariantList &indexPath)
{
    return indexPath.isEmpty() && !m_entries.isEmpty();
}

QString MessageModel::itemType(const QVariantList &indexPath)
{
    const Entry e = m_entries.value(indexPath.value(0).toInt());
    if (e.msg < 0)
        return "header";
    return m_all[e.msg].service ? "service" : "msg";
}

QVariant MessageModel::data(const QVariantList &indexPath)
{
    const Entry e = m_entries.value(indexPath.value(0).toInt());
    return e.msg < 0 ? QVariant(e.title) : QVariant(m_all[e.msg].data);
}

MessageModel::Msg MessageModel::read(QSqlQuery &q)
{
    Msg m;
    m.rowid = q.value(0).toLongLong();
    m.id = q.value(1).toString();
    m.ts = q.value(2).toLongLong();
    const bool out = q.value(3).toInt() != 0;
    const int kind = q.value(7).toInt();
    const QString text = q.value(8).toString();
    const bool deleted = q.value(11).toInt() != 0;
    m.service = kind == 6;
    if (m.service) {
        m.data["text"] = text;
        return m;
    }

    const QLocale locale = m_region.locale();
    QString label, shown = text;
    // Photos, videos, audio files and documents are drawn by MediaBubble: no label, and a
    // document's text (its name) is the bubble's. A caption stays in the text.
    switch (kind) {
    case 5: label = tr("Sticker"); break;
    case 7: label = text; shown = QString(); break; // "Location", "Poll: …"
    }
    if (MediaData::fill(m.data, deleted ? 0 : kind, text, q.value(19).toByteArray(),
                        q.value(20).toInt(), q.value(21).toInt(), q.value(22).toLongLong(),
                        q.value(15).toInt(), q.value(17).toInt(), q.value(23).toString(), locale)
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
    m.data["deleted"] = deleted;
    // Delete for everyone: ours, sent, and inside WhatsApp's limit of about 2.5 days.
    // ponytail: read when the row is read, so a page left open for days can offer it a little late.
    const int state = q.value(4).toInt();
    m.data["revocable"] = out && !deleted && state >= 1 && state <= 3
                          && QDateTime::currentDateTime().toTime_t() - m.ts < 60 * 3600;
    m.data["time"] = locale.toString(QDateTime::fromTime_t(m.ts).time(),
                                     timeFormat(locale, DateFormat::Short));
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
    m.data["quote"] = q.value(9).toString();
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
    return m;
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

// Runs the messages query of this chat: "where" adds conditions on top of chat = ?, args are
// bound after it. Newest first, then reversed to the order the list shows.
QList<MessageModel::Msg> MessageModel::select(const QString &where, const QVariantList &args,
                                              int limit)
{
    QList<Msg> rows;
    QSqlDatabase db = database();
    if (!db.isOpen())
        return rows;
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
        rows.prepend(read(q));
    return rows;
}

// The newest page, or the page older than the oldest loaded message. A full page also takes
// the rest of its oldest row's day, so the loaded set holds whole days.
void MessageModel::loadPage()
{
    QList<Msg> rows;
    if (m_all.isEmpty()) {
        rows = select(QString(), QVariantList(), kPage);
    } else {
        const Msg &o = m_all.first();
        rows = select("AND (ts < ? OR (ts = ? AND rowid < ?))",
                      QVariantList() << o.ts << o.ts << o.rowid, kPage);
    }
    // A short page is the start of the chat.
    m_atStart = rows.size() < kPage;
    if (!m_atStart) {
        const Msg o = rows.first();
        const uint midnight =
            QDateTime(QDateTime::fromTime_t(o.ts).date(), QTime(0, 0)).toTime_t();
        rows = select("AND ts >= ? AND (ts < ? OR (ts = ? AND rowid < ?))",
                      QVariantList() << midnight << o.ts << o.ts << o.rowid, -1) + rows;
    }
    merge(QStringList(), rows, true);
}

// In a bottom-to-top list the model's end is the top.
void MessageModel::nearTop(const QVariantList &firstVisible)
{
    if (m_atStart || firstVisible.isEmpty())
        return;
    if (firstVisible.value(0).toInt() >= m_entries.size() - 25)
        loadPage();
}

void MessageModel::onMsgs(const QString &chat, const QStringList &ids)
{
    if (chat != m_chat)
        return;
    // The sim's seed names 600 ids at once; SQLite binds 999 at most.
    QList<Msg> fresh;
    for (int from = 0; from < ids.size(); from += 500) {
        const QStringList part = ids.mid(from, 500);
        QVariantList args;
        QStringList marks;
        foreach (const QString &id, part) {
            args << id;
            marks << "?";
        }
        fresh += select(QString("AND id IN (%1)").arg(marks.join(",")), args, part.size());
    }
    merge(ids, fresh, false);
}

// Events sent while the link was down are lost: read everything from the oldest loaded message
// on again (not the newest N: messages that came meanwhile would push loaded ones out).
void MessageModel::onConnected()
{
    if (!m_engine || !m_engine->connected())
        return;
    if (m_all.isEmpty()) {
        loadPage();
        return;
    }
    const Msg o = m_all.first();
    QStringList asked;
    foreach (const Msg &m, m_all)
        asked << m.id; // one that isn't read again is gone
    const QList<Msg> fresh = select("AND (ts > ? OR (ts = ? AND rowid >= ?))",
                                    QVariantList() << o.ts << o.ts << o.rowid, -1); // no limit
    foreach (const Msg &m, fresh)
        asked << m.id;
    merge(asked, fresh, false);
}

// Puts rows into m_all and tells the list. asked: the ids that were read again; fresh: the
// rows that came back. A loaded asked id that isn't in fresh is gone.
void MessageModel::merge(const QStringList &asked, const QList<Msg> &fresh, bool older)
{
    QHash<QString, Msg> back;
    foreach (const Msg &m, fresh)
        back.insert(m.id, m);
    const QSet<QString> was = QSet<QString>::fromList(asked);

    QSet<QString> loaded, updated;
    QList<Msg> all;
    foreach (const Msg &m, m_all) {
        loaded.insert(m.id);
        if (!was.contains(m.id))
            all.append(m);
        else if (back.contains(m.id)) {
            all.append(back.value(m.id));
            updated.insert(m.id);
        }
    }
    const bool empty = m_all.isEmpty();
    bool incoming = false;
    foreach (const Msg &m, fresh) {
        if (loaded.contains(m.id))
            continue;
        // A new message older than the loaded ones comes with a later page.
        if (older || empty || m_atStart || m.ts > m_all.first().ts
            || (m.ts == m_all.first().ts && m.rowid > m_all.first().rowid)) {
            all.append(m);
            // Only loadPage passes older (the first load and older pages): anything else that is
            // new arrived live, even into a chat that opened empty.
            if (!older && !m.data["out"].toBool())
                incoming = true;
        }
    }
    // Sending moves a row: its ts becomes the server's.
    qStableSort(all.begin(), all.end(), lessThan);

    const QStringList old = keys();
    m_all = all;
    refresh(old, updated);
    if (incoming)
        emit this->incoming();
}

QStringList MessageModel::keys() const
{
    QStringList k;
    foreach (const Entry &e, m_entries)
        k << e.key;
    return k;
}

// a is a subsequence of b: b is a with rows inserted anywhere.
static bool isSubsequence(const QStringList &a, const QStringList &b)
{
    int i = 0;
    for (int j = 0; j < b.size() && i < a.size(); ++j) {
        if (a[i] == b[j])
            ++i;
    }
    return i == a.size();
}

// Rebuilds the entries from m_all and tells the list how they changed from old (the keys before).
// itemAdded and itemRemoved never move the rows on screen; an AddRemove did (Q10, 3.1). So pure
// insertions (a new message at the bottom, an older page at the top, an unhidden one anywhere)
// and pure removals (a hidden or deleted one) are told one by one, and only a reorder is an
// AddRemove.
void MessageModel::refresh(const QStringList &old, const QSet<QString> &updated)
{
    rebuild();
    const QStringList now = keys();

    if (old.isEmpty()) {
        if (!now.isEmpty())
            emit itemsChanged(DataModelChangeType::Init);
        return;
    }
    if (isSubsequence(old, now)) {
        int i = 0;
        for (int j = 0; j < now.size(); ++j) {
            if (i < old.size() && old[i] == now[j])
                ++i;
            else
                emit itemAdded(QVariantList() << j);
        }
    } else if (isSubsequence(now, old)) {
        QList<int> gone;
        int i = 0;
        for (int j = 0; j < old.size(); ++j) {
            if (i < now.size() && now[i] == old[j])
                ++i;
            else
                gone.prepend(j);
        }
        foreach (int j, gone)
            emit itemRemoved(QVariantList() << j); // descending: the others keep their places
    } else {
        // ponytail: if a sent message's new server time reorders rows and the list jumps, emit
        // itemMoved.
        Moves *moves = new Moves;
        moves->oldGroups = old;
        for (int i = 0; i < now.size(); ++i)
            moves->groups.insert(now[i], i);
        emit itemsChanged(DataModelChangeType::AddRemove, QSharedPointer<IndexMapper>(moves));
    }
    foreach (const QString &id, updated) {
        const int at = now.indexOf(id);
        if (at >= 0)
            emit itemUpdated(QVariantList() << at);
    }
}

void MessageModel::hide(const QStringList &ids)
{
    const QStringList old = keys();
    foreach (const QString &id, ids)
        m_hidden.insert(id);
    refresh(old, QSet<QString>());
}

void MessageModel::unhide(const QStringList &ids)
{
    const QStringList old = keys();
    foreach (const QString &id, ids)
        m_hidden.remove(id);
    refresh(old, QSet<QString>());
}

// ponytail: rebuilds the entries on each change, fine for the hundreds of rows a chat holds.
void MessageModel::rebuild()
{
    m_days.applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    const QDate today = QDate::currentDate();
    m_entries.clear();
    // m_all is oldest first: walk the visible rows backwards, and a day's header follows its
    // messages. A day with only hidden messages has no header.
    QList<int> shown;
    for (int i = 0; i < m_all.size(); ++i) {
        if (!m_hidden.contains(m_all[i].id))
            shown.append(i);
    }
    for (int k = shown.size() - 1; k >= 0; --k) {
        const int i = shown[k];
        const QDateTime when = QDateTime::fromTime_t(m_all[i].ts);
        Entry e;
        e.key = m_all[i].id;
        e.msg = i;
        m_entries.append(e);
        if (k == 0 || QDateTime::fromTime_t(m_all[shown[k - 1]].ts).date() != when.date()) {
            Entry h;
            h.key = "day:" + QString::number(when.date().toJulianDay());
            h.title = when.date().daysTo(today) < 7 ? dayLabel(when, today, locale, m_days)
                      : capitalized(locale.toString(when.date(), QLocale::LongFormat));
            h.msg = -1;
            m_entries.append(h);
        }
    }
}
