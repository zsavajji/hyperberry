#include "ChatListModel.h"

#include "EngineLink.h"
#include "Moves.h"
#include "colors.h"
#include "dates.h"

#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QHash>
#include <QLocale>
#include <QPair>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>
#include <QStringList>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// The QSQLITE connection to chats.db, read-only: only the engine writes it (PLAN.md §12).
static const char *const kDb = "chats";

// Tile colours, from the BB10 guidelines' icon palette and accents, picked by a hash of the jid
// so a chat keeps its colour.
static const char *const kColors[] = { "#779933", "#CC3F10", "#087099", "#A30D7E" };

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

// The first character of a word, a whole surrogate pair when it's one.
static QString first(const QString &word)
{
    return word.left(word.at(0).isHighSurrogate() ? 2 : 1).toUpper();
}

// Up to two initials for the tile; "#" for a bare number.
static QString initials(const QString &name)
{
    const QStringList words = name.split(' ', QString::SkipEmptyParts);
    if (words.isEmpty() || name.startsWith('+'))
        return "#";
    QString s = first(words.first());
    if (words.size() > 1 && words.last().at(0).isLetter())
        s += first(words.last());
    return s;
}

ChatListModel::ChatListModel(EngineLink *engine, QObject *parent)
    : DataModel(parent)
    , m_days(RelativeDateFormat::NonTimeCritical)
    , m_region(bb::system::LocaleType::Region)
{
    m_days.applyUserRegionLocale();
    QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", kDb);
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
    // A cold start draws what chats.db already has, before the engine answers (PLAN.md §12).
    reload();
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

bool ChatListModel::empty() const
{
    return m_groups.isEmpty();
}

void ChatListModel::reloadSoon()
{
    if (!m_soon.isActive())
        m_soon.start();
}

// ponytail: re-reads every chat on each change, a few ms for hundreds of chats; read only the
// changed jids if accounts with thousands of chats make it show.
void ChatListModel::reload()
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    // Before the first link the engine hasn't made chats.db: try again at the next event.
    if (!db.isOpen() && !db.open())
        return;
    QSqlQuery q(db);
    if (!q.exec("SELECT jid, kind, name, last_ts, last_preview, last_out, last_state, unread, "
                "muted_until, pinned FROM chats WHERE archived = 0 "
                "ORDER BY pinned DESC, last_ts DESC")) {
        qWarning() << "chats:" << q.lastError().text();
        return;
    }

    m_days.applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    const QString clock = timeFormat(locale, DateFormat::Short);
    const QDate today = QDate::currentDate();
    const qint64 now = QDateTime::currentMSecsSinceEpoch() / 1000;
    QHash<int, QString> days; // the formatter's label by julian day: one call per day, not per chat

    QList<Group> groups;
    Moves *moves = new Moves;
    while (q.next()) {
        const QString jid = q.value(0).toString();
        const int kind = q.value(1).toInt();
        const QString name = q.value(2).isNull() ? placeholder(jid, kind) : q.value(2).toString();
        const bool pinned = q.value(9).toInt() != 0;
        const bool hasTime = !q.value(3).isNull();
        const QDateTime when = QDateTime::fromTime_t(q.value(3).toUInt());
        // The last week goes by day: "Today" and "Yesterday" from BB10's formatter, then the
        // weekday (the formatter names only two or three days back, and dates the rest). Older
        // chats go by month, so a long tail of old chats isn't a header each.
        QString day, header;
        int age = 0;
        if (hasTime) {
            const QDate date = when.date();
            age = date.daysTo(today);
            if (!days.contains(date.toJulianDay()))
                days.insert(date.toJulianDay(), dayLabel(when, today, locale, m_days));
            day = days.value(date.toJulianDay());
            header = age < 7 ? day : capitalized(locale.standaloneMonthName(date.month()));
            if (age >= 7 && date.year() != today.year())
                header += " " + QString::number(date.year());
        }

        const QString title = pinned ? tr("Pinned") : hasTime ? header : tr("No messages");
        if (groups.isEmpty() || groups.last().title != title) {
            groups.append(Group());
            groups.last().title = title;
            moves->groups.insert(title, groups.size() - 1);
        }
        QVariantMap row;
        row["jid"] = jid;
        row["kind"] = kind;
        row["name"] = name;
        row["initials"] = initials(name);
        row["color"] = pickColor(jid, kColors, sizeof kColors / sizeof *kColors);
        row["preview"] = q.value(4).toString();
        // The time under a day's header; the day, or the date, where no header says it.
        if (hasTime)
            row["time"] = age == 0 || (age < 7 && !pinned) ? locale.toString(when.time(), clock)
                        : age < 7 ? day : locale.toString(when.date(), QLocale::ShortFormat);
        row["out"] = q.value(5).toInt() != 0;
        row["state"] = q.value(6).toInt();
        row["unread"] = q.value(7).toInt();
        row["muted"] = q.value(8).toLongLong() > now;
        row["pinned"] = pinned;
        moves->items.insert(jid, qMakePair(groups.size() - 1, groups.last().rows.size()));
        groups.last().rows.append(row);
    }

    foreach (const Group &g, m_groups) {
        moves->oldGroups.append(g.title);
        QStringList jids;
        foreach (const QVariantMap &row, g.rows)
            jids.append(row["jid"].toString());
        moves->oldItems.append(jids);
    }
    const bool wasEmpty = m_groups.isEmpty();
    m_groups = groups;
    if (wasEmpty) {
        delete moves;
        emit itemsChanged(DataModelChangeType::Init);
    } else {
        emit itemsChanged(DataModelChangeType::AddRemove, QSharedPointer<IndexMapper>(moves));
        // The rows that stayed may have new text: the list reads its cached rows again.
        emit itemsChanged(DataModelChangeType::Update);
    }
    if (wasEmpty != m_groups.isEmpty())
        emit emptyChanged();
}
