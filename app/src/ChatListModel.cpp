#include "ChatListModel.h"

#include "EngineLink.h"
#include "Moves.h"
#include "colors.h"
#include "Pins.h"
#include "dates.h"

#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QElapsedTimer>
#include <QHash>
#include <QLocale>
#include <QMetaType>
#include <QPair>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>
#include <QStringList>
#include <QThread>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// The QSQLITE connection to chats.db, read-only: only the engine writes it (PLAN.md §12).
static const char *const kDb = "chats";

// A chat with no name yet, by docs/protocol.md's chats.kind.
static QString placeholder(const QString &jid, int kind)
{
    switch (kind) {
    case 1:
        return ChatListModel::tr("Unnamed group");
    case 2:
        return ChatListModel::tr("Broadcast list");
    case 3:
        return ChatListModel::tr("Channel");
    }
    if (jid.endsWith("@s.whatsapp.net"))
        return "+" + jid.section('@', 0, 0);
    return ChatListModel::tr("Unknown");
}

// The row's fields from a SELECT of kRowColumns, all but the time, which depends on the list.
static const char *const kRowColumns =
    "jid, kind, name, last_ts, last_preview, last_out, last_state, unread, muted_until, pinned, photo, translate";

static QVariantMap baseRow(const QSqlQuery &q, qint64 now)
{
    const QString jid = q.value(0).toString();
    const int kind = q.value(1).toInt();
    const QString name = q.value(2).isNull() ? placeholder(jid, kind) : q.value(2).toString();
    QVariantMap row;
    row["jid"] = jid;
    row["kind"] = kind;
    row["name"] = name;
    row["initials"] = initials(name);
    row["color"] = pickColor(jid, kColors, kColorCount);
    row["preview"] = q.value(4).toString();
    row["out"] = q.value(5).toInt() != 0;
    row["state"] = q.value(6).toInt();
    row["unread"] = q.value(7).toInt();
    row["muted"] = q.value(8).toLongLong() > now;
    row["pinned"] = q.value(9).toInt() != 0;
    row["photo"] = q.value(10).toString(); // the engine's preview file's path, "" when none
    row["translate"] = q.value(11).toString(); // the language incoming messages are shown in, "" for off
    return row;
}

RowClock::RowClock(const QLocale &l)
    : locale(l)
    , clock(timeFormat(l, DateFormat::Short))
    , today(QDate::currentDate())
    , now(QDateTime::currentMSecsSinceEpoch() / 1000)
{
    // Only the last week's days are named (and tomorrow, for a phone whose clock is behind): eight labels, of which
    // BB10's formatter gives three ("Today", "Yesterday", tomorrow's) and the locale the weekdays. Asked here, on the
    // UI thread, so place() can run on the reader's thread; and once a day, not once a clock: the formatter is dear
    // (milliseconds a label), and a history sync makes a clock for every reload, four a second.
    static QDate labelled;
    static QString labelledIn;
    static QHash<int, QString> labels;
    if (labelled != today || labelledIn != locale.name()) {
        labelled = today;
        labelledIn = locale.name();
        labels.clear();
        dayFormatter().applyCurrentTimeAsAnchor();
        for (int back = -1; back < 7; ++back) {
            const QDate date = today.addDays(-back);
            labels.insert(date.toJulianDay(), dayLabel(QDateTime(date, QTime(12, 0)), today, locale, dayFormatter()));
        }
    }
    days = labels;
}

QString RowClock::place(QVariantMap *row, const QVariant &lastTs, bool pinned) const
{
    const bool hasTime = !lastTs.isNull();
    const QDateTime when = QDateTime::fromTime_t(lastTs.toUInt());
    // The last week goes by day: "Today" and "Yesterday" from BB10's formatter, then the
    // weekday (the formatter names only two or three days back, and dates the rest). Older
    // chats go by month, so a long tail of old chats isn't a header each.
    QString day, header;
    int age = 0;
    if (hasTime) {
        const QDate date = when.date();
        age = date.daysTo(today);
        // Only the last week's days are named. The formatter was asked for every older day too, a label
        // nothing showed, and it is dear: most of a reload's 65 to 165 ms for 40 chats (Q10, 0.1.1.306).
        if (age < 7) {
            day = days.value(date.toJulianDay());
            if (day.isEmpty()) // a date beyond tomorrow
                day = locale.toString(date, QLocale::ShortFormat);
        }
        header = age < 7 ? day : capitalized(locale.standaloneMonthName(date.month()));
        if (age >= 7 && date.year() != today.year())
            header += " " + QString::number(date.year());
    }
    // The time under a day's header; the day, or the date, where no header says it.
    if (hasTime)
        (*row)["time"] = age == 0 || (age < 7 && !pinned) ? locale.toString(when.time(), clock)
                       : age < 7 ? day : locale.toString(when.date(), QLocale::ShortFormat);
    return pinned ? ChatListModel::tr("Pinned") : hasTime ? header : ChatListModel::tr("No messages");
}

// By the start of any word of the name, ignoring case, as ContactPicker::filter does. An empty filter matches all.
static bool nameMatches(const QString &filter, const QString &name)
{
    if (filter.isEmpty())
        return true;
    foreach (const QString &w, name.split(' ', QString::SkipEmptyParts)) {
        if (w.startsWith(filter, Qt::CaseInsensitive))
            return true;
    }
    return false;
}

ChatListModel::ChatListModel(EngineLink *engine, Mode mode, QObject *parent)
    : DataModel(parent)
    , m_engine(engine)
    , m_mode(mode)
    , m_dbName(mode == Archived ? "chats-archived" : mode == Channels ? "chats-channels" : kDb)
    , m_unreadTotal(0)
    , m_revision(0)
    , m_region(bb::system::LocaleType::Region)
    , m_thread(new QThread) // no parent, see ~ChatListModel
    , m_reading(false)
    , m_again(false)
    , m_loaded(false)
{
    QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", m_dbName);
    db.setDatabaseName(QDir::homePath() + "/chats.db");
    db.setConnectOptions("QSQLITE_OPEN_READONLY");

    // During a history sync the engine sends a chats event every 50 ms; a reload at most every
    // 250 ms keeps the UI thread free.
    m_soon.setSingleShot(true);
    m_soon.setInterval(250);
    connect(&m_soon, SIGNAL(timeout()), this, SLOT(reload()));
    connect(engine, SIGNAL(chatsChanged(QStringList)), this, SLOT(reloadSoon()));
    // Events sent while the link was down are lost; the database has what they said.
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(reloadSoon()));
    // Nobody sees the frame while the app is fullscreen: it's computed when the app leaves it.
    if (m_mode == Chats)
        connect(engine, SIGNAL(fullscreenChanged()), this, SLOT(updateFrame()));
    qRegisterMetaType<ChatListRequest>("ChatListRequest");
    qRegisterMetaType<ChatListRead>("ChatListRead");
    ChatListReader *reader = new ChatListReader(m_dbName + "-reader");
    reader->moveToThread(m_thread);
    connect(this, SIGNAL(readWanted(ChatListRequest)), reader, SLOT(read(ChatListRequest)));
    connect(reader, SIGNAL(done(ChatListRead)), this, SLOT(onRead(ChatListRead)));
    m_thread->start();
    // A cold start asks for what chats.db already has before the engine answers (PLAN.md §12); the rows are there
    // when the event loop first runs.
    reload();
}

ChatListModel::~ChatListModel()
{
    m_thread->quit();
    m_thread->wait(2000);
    // The thread (and the reader on it) is deliberately not deleted: a QThread destroyed while it still runs aborts
    // the process, as it would if the reader is in a long read, and this object lasts as long as the app does.
}

int ChatListModel::childCount(const QVariantList &indexPath)
{
    if (indexPath.isEmpty())
        return m_groups.size();
    if (indexPath.size() == 1)
        return m_groups.value(indexPath[0].toInt()).rows.size();
    return 0;
}

bool ChatListModel::hasChildren(const QVariantList &indexPath)
{
    return indexPath.size() < 2 && childCount(indexPath) > 0;
}

QString ChatListModel::itemType(const QVariantList &indexPath)
{
    return indexPath.size() == 1 ? "header" : "item";
}

QVariant ChatListModel::data(const QVariantList &indexPath)
{
    const Group g = m_groups.value(indexPath.value(0).toInt());
    if (indexPath.size() == 1)
        return g.title;
    return g.rows.value(indexPath.value(1).toInt());
}

// ponytail: an archived chat has no "time", which the chat page doesn't show.
QVariantMap ChatListModel::chat(const QString &jid)
{
    foreach (const Group &g, m_groups) {
        foreach (const QVariantMap &row, g.rows) {
            if (row["jid"].toString() == jid)
                return row;
        }
    }
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    if (!db.isOpen() && !db.open())
        return QVariantMap();
    QSqlQuery q(db);
    q.prepare(QString("SELECT %1 FROM chats WHERE jid = ?").arg(kRowColumns));
    q.addBindValue(jid);
    if (!q.exec() || !q.next())
        return QVariantMap();
    return baseRow(q, QDateTime::currentMSecsSinceEpoch() / 1000);
}

// Qt does the matching, not SQL's LIKE: LIKE folds ASCII only, so "è" would not find "È".
// ponytail: reads every text of the chat on each search; an FTS table if a chat of many thousands
// makes Enter slow.
QStringList ChatListModel::findMessages(const QString &jid, const QString &text)
{
    QStringList ids;
    const QString t = text.trimmed();
    if (t.isEmpty())
        return ids;
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    if (!db.isOpen() && !db.open())
        return ids;
    QSqlQuery q(db);
    q.prepare("SELECT id, text FROM messages WHERE chat = ? AND kind NOT IN (6, 13) AND deleted = 0 "
              "AND text != '' ORDER BY ts DESC, rowid DESC");
    q.addBindValue(jid);
    if (!q.exec()) {
        qWarning() << "find:" << q.lastError().text();
        return ids;
    }
    while (q.next()) {
        if (q.value(1).toString().contains(t, Qt::CaseInsensitive))
            ids << q.value(0).toString();
    }
    return ids;
}

// ponytail: scans messages for starred = 1 (no index, as Qt 4.8's SQLite reads the file); an index on the engine's
// side if the page gets slow.
QVariantList ChatListModel::starred()
{
    QVariantList rows;
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    if (!db.isOpen() && !db.open())
        return rows;
    QSqlQuery q(db);
    if (!q.exec("SELECT m.chat, m.id, m.ts, m.out, m.sender_name, m.kind, m.text, c.name, c.kind "
                "FROM messages m JOIN chats c ON c.jid = m.chat WHERE m.starred = 1 AND m.deleted = 0 "
                "ORDER BY m.ts DESC, m.rowid DESC")) {
        qWarning() << "starred:" << q.lastError().text();
        return rows;
    }
    dayFormatter().applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    const QString clock = timeFormat(locale, DateFormat::Short);
    const QDate today = QDate::currentDate();
    while (q.next()) {
        const QString chat = q.value(0).toString();
        const int kind = q.value(8).toInt();
        const QDateTime when = QDateTime::fromTime_t(q.value(2).toUInt());
        const int age = when.date().daysTo(today);
        QVariantMap row;
        row["chat"] = chat;
        row["id"] = q.value(1).toString();
        row["chatName"] = q.value(7).isNull() ? placeholder(chat, kind) : q.value(7).toString();
        row["sender"] = kind != 1 ? QString() : q.value(3).toInt() != 0 ? tr("You") : q.value(4).toString();
        row["text"] = Pins::summary(q.value(5).toInt(), q.value(6).toString());
        // As the chat list: the time today, the day within the week, else the date.
        row["time"] = age == 0 ? locale.toString(when.time(), clock)
                    : age < 7 ? dayLabel(when, today, locale, dayFormatter())
                    : locale.toString(when.date(), QLocale::ShortFormat);
        rows << row;
    }
    return rows;
}

// For the toast of the chip under a card: "You 👍" for ours, else the reactor's name, or the part
// of the sender's key before the '@' when there is no name.
QString ChatListModel::reactionsText(const QString &jid, const QString &id)
{
    QStringList lines;
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    if (!db.isOpen() && !db.open())
        return QString();
    QSqlQuery q(db);
    q.prepare("SELECT sender, sender_name, emoji FROM reactions WHERE chat = ? AND msg = ? "
              "ORDER BY ts, sender");
    q.addBindValue(jid);
    q.addBindValue(id);
    if (!q.exec()) {
        qWarning() << "reactions:" << q.lastError().text();
        return QString();
    }
    while (q.next()) {
        const QString sender = q.value(0).toString();
        QString who = q.value(1).toString();
        if (sender.isEmpty())
            who = tr("You");
        else if (who.isEmpty())
            who = sender.section('@', 0, 0);
        lines << who + " " + q.value(2).toString();
    }
    return lines.join("\n");
}

bool ChatListModel::empty() const
{
    return m_loaded && m_groups.isEmpty();
}

void ChatListModel::reloadSoon()
{
    if (!m_soon.isActive())
        m_soon.start();
}

bool ChatListModel::matches(const QString &name) const
{
    return nameMatches(m_filter, name);
}

void ChatListModel::setFilter(const QString &text)
{
    const QString t = text.trimmed();
    if (t == m_filter)
        return;
    m_filter = t;
    reload();
}

// ponytail: re-reads every chat on each change, now on the reader's thread (Tengram's list took about 300 ms for 500
// chats on the Classic); onRead() shows it. Read only the changed jids if accounts with thousands of chats make it show.
void ChatListModel::reload()
{
    // One read at a time: what changes while it is out is read again when it is back.
    if (m_reading) {
        m_again = true;
        return;
    }
    m_reading = true;
    m_again = false;
    m_readTimer.start();
    ChatListRequest request;
    // Channels have their own list: WhatsApp keeps them apart too.
    request.where = m_mode == Channels ? QString("kind = 3")
                                       : QString("kind != 3 AND archived = %1").arg(m_mode == Archived ? 1 : 0);
    request.filter = m_filter;
    request.clock = RowClock(m_region.locale());
    emit readWanted(request);
}

ChatListReader::ChatListReader(const QString &connectionName)
    : m_dbName(connectionName)
{
}

// On the reader's thread.
void ChatListReader::read(const ChatListRequest &request)
{
    ChatListRead out;
    QElapsedTimer timer;
    timer.start();
    // The connection is its own and is made here, on first use, because it belongs to the thread that makes it.
    if (!QSqlDatabase::contains(m_dbName)) {
        QSqlDatabase added = QSqlDatabase::addDatabase("QSQLITE", m_dbName);
        added.setDatabaseName(QDir::homePath() + "/chats.db");
        added.setConnectOptions("QSQLITE_OPEN_READONLY");
    }
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    // Before the first link the engine hasn't made chats.db: try again at the next event.
    if (!db.isOpen() && !db.open()) {
        emit done(out);
        return;
    }
    QSqlQuery q(db);
    if (!q.exec(QString("SELECT %1 FROM chats WHERE %2 ORDER BY pinned DESC, last_ts DESC").arg(kRowColumns).arg(request.where))) {
        qWarning() << "chats:" << q.lastError().text();
        emit done(out);
        return;
    }
    const RowClock &clock = request.clock;

    // Where a reload's time goes (Tengram on the Classic, 2026-11-24: a cold start read 500 chats twice on the UI
    // thread, 293 and 278 ms): the query, SQLite's fetching of the rows (next()), and building them.
    const qint64 execNs = timer.nsecsElapsed();
    qint64 fetchNs = 0;
    QElapsedTimer step;
    for (;;) {
        step.start();
        const bool more = q.next();
        fetchNs += step.nsecsElapsed();
        if (!more)
            break;
        const QString jid = q.value(0).toString();
        QVariantMap row = baseRow(q, clock.now);
        if (!nameMatches(request.filter, row["name"].toString()))
            continue;
        const QString title = clock.place(&row, q.value(3), row["pinned"].toBool());
        if (out.groups.isEmpty() || out.groups.last().title != title) {
            out.groups.append(ChatListGroup());
            out.groups.last().title = title;
            out.groupAt.insert(title, out.groups.size() - 1);
        }
        out.itemAt.insert(jid, qMakePair(out.groups.size() - 1, out.groups.last().rows.size()));
        out.groups.last().rows.append(row);
        ++out.chats;
    }
    const qint64 builtNs = timer.nsecsElapsed();
    out.queryMs = int(execNs / 1000000);
    out.fetchMs = int(fetchNs / 1000000);
    out.rowsMs = int((builtNs - execNs - fetchNs) / 1000000);
    out.ok = true;
    emit done(out);
}

// On the UI thread: swaps the rows in and tells the list.
void ChatListModel::onRead(const ChatListRead &read)
{
    QElapsedTimer timer;
    timer.start();
    m_reading = false;
    const bool shownEmpty = empty();
    m_loaded = true;
    if (read.ok) {
        const bool wasEmpty = m_groups.isEmpty();
        Moves *moves = 0;
        if (!wasEmpty) {
            moves = new Moves;
            moves->groups = read.groupAt;
            moves->items = read.itemAt;
            foreach (const Group &g, m_groups) {
                moves->oldGroups.append(g.title);
                QStringList jids;
                foreach (const QVariantMap &row, g.rows)
                    jids.append(row["jid"].toString());
                moves->oldItems.append(jids);
            }
        }
        m_groups = read.groups;
        if (wasEmpty) {
            emit itemsChanged(DataModelChangeType::Init);
        } else {
            emit itemsChanged(DataModelChangeType::AddRemove, QSharedPointer<IndexMapper>(moves));
            // The rows that stayed may have new text: the list reads its cached rows again.
            emit itemsChanged(DataModelChangeType::Update);
        }
        ++m_revision;
        emit revisionChanged();
        if (m_mode == Chats) {
            // Once per run: with "ui: main" and the Hub tap's line it times a cold start.
            static bool first = true;
            if (first) {
                first = false;
                qDebug() << "chat list: first draw," << read.chats << "chats";
            }
            updateFrame();
        }
        // The read is off the UI thread: only "list" (the emits and the frame, here) is on it, and the rest of the
        // total is the answer's wait for the UI thread, which at a cold start is busy loading main.qml.
        const int total = int(m_readTimer.elapsed());
        if (total > 50)
            qDebug("chat list: reload, %d chats in %d ms (off the UI thread: query %d, fetch %d, rows %d; on it: list %d)",
                   read.chats, total, read.queryMs, read.fetchMs, read.rowsMs, int(timer.elapsed()));
    }
    // Not read (chats.db is not there yet): the next event reloads anyway.
    if (shownEmpty != empty())
        emit emptyChanged();
    if (m_again)
        reload();
}

// A chat marked unread (unread < 0, step 5.3) counts as 1. 2^40 in muted_until is forever.
// The frame names the chats with unread messages, newest first, and shows no message text: the
// newest line showed after it was read, and with the notification preview off (Q10, 0.0.1.88).
void ChatListModel::updateFrame()
{
    if (m_engine->fullscreen())
        return;
    QSqlDatabase db = QSqlDatabase::database(m_dbName, false);
    if (!db.isOpen() && !db.open())
        return;
    const qint64 now = QDateTime::currentMSecsSinceEpoch() / 1000;
    int total = 0;
    QStringList names;
    QSqlQuery q(db);
    // ponytail: the first 10 names are more than the frame's three lines hold.
    q.prepare("SELECT name, jid, unread FROM chats WHERE muted_until <= ? AND unread != 0 AND archived = 0 AND kind != 3 "
              "ORDER BY last_ts DESC");
    q.addBindValue(now);
    if (q.exec()) {
        while (q.next()) {
            const int unread = q.value(2).toInt();
            total += unread < 0 ? 1 : unread;
            if (names.size() < 10) {
                const QString name = q.value(0).toString();
                names << (name.isEmpty() ? q.value(1).toString().section('@', 0, 0) : name);
            }
        }
    }
    const QString chats = names.join(", ");
    if (total != m_unreadTotal || chats != m_unreadChats) {
        m_unreadTotal = total;
        m_unreadChats = chats;
        emit frameChanged();
    }
}
