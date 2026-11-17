#include "CallsModel.h"

#include "EngineLink.h"
#include "MediaData.h"
#include "colors.h"
#include "dates.h"

#include <bb/data/JsonDataAccess>
#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QLocale>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// Its own read-only connection to chats.db: only the engine writes it (PLAN.md §12).
static const char *const kDb = "calls";

// The name as ChatListModel words a chat with none (a person's number).
static QString callerName(const QString &jid, const QString &name)
{
    if (!name.isEmpty())
        return name;
    return jid.endsWith("@s.whatsapp.net") ? "+" + jid.section('@', 0, 0) : CallsModel::tr("Unknown");
}

// Today by the clock, the last week by day, older by date (the chat list's time column).
static QString whenText(const QDateTime &when, const QDate &today, const QLocale &locale, const QString &clock)
{
    const int age = when.date().daysTo(today);
    return age == 0 ? locale.toString(when.time(), clock)
         : age < 7 ? dayLabel(when, today, locale, dayFormatter())
         : locale.toString(when.date(), QLocale::ShortFormat);
}

CallsModel::CallsModel(EngineLink *engine, QObject *parent)
    : DataModel(parent)
    , m_active(false)
    , m_file(QDir::homePath() + "/settings.ini", QSettings::IniFormat)
    , m_missedNew(0)
    , m_region(bb::system::LocaleType::Region)
{
    QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", kDb);
    db.setDatabaseName(QDir::homePath() + "/chats.db");
    db.setConnectOptions("QSQLITE_OPEN_READONLY");
    m_soon.setSingleShot(true);
    m_soon.setInterval(250);
    connect(&m_soon, SIGNAL(timeout()), this, SLOT(reload()));
    connect(engine, SIGNAL(chatsChanged(QStringList)), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(msgsChanged(QString, QStringList)), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(reloadSoon()));
    // The Active Frame's count runs while the tab is hidden, so it has its own timer.
    m_countSoon.setSingleShot(true);
    m_countSoon.setInterval(250);
    connect(&m_countSoon, SIGNAL(timeout()), this, SLOT(count()));
    connect(engine, SIGNAL(chatsChanged(QStringList)), this, SLOT(countSoon()));
    connect(engine, SIGNAL(msgsChanged(QString, QStringList)), this, SLOT(countSoon()));
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(countSoon()));
    // First run after the update: the calls already in the list are not new.
    if (!m_file.contains("callsSeen"))
        markSeen();
    count();
}

int CallsModel::childCount(const QVariantList &indexPath)
{
    return indexPath.isEmpty() ? m_rows.size() : 0;
}

bool CallsModel::hasChildren(const QVariantList &indexPath)
{
    return indexPath.isEmpty() && !m_rows.isEmpty();
}

QString CallsModel::itemType(const QVariantList &) { return "item"; }

QVariant CallsModel::data(const QVariantList &indexPath)
{
    return indexPath.size() == 1 ? m_rows.value(indexPath[0].toInt()) : QVariant();
}

void CallsModel::setActive(bool on)
{
    if (on == m_active)
        return;
    m_active = on;
    if (on) {
        markSeen();
        count();
        reload();
    }
    emit activeChanged();
}

void CallsModel::markSeen()
{
    m_file.setValue("callsSeen", QDateTime::currentMSecsSinceEpoch() / 1000);
    m_file.sync();
}

void CallsModel::countSoon()
{
    if (!m_countSoon.isActive())
        m_countSoon.start();
}

// Our own incoming missed calls since the Calls tab was last shown: how many, and the newest by name and time.
void CallsModel::count()
{
    // A call missed while the tab shows is seen there: it doesn't wait in the frame for the next visit.
    if (m_active)
        markSeen();
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return;
    QSqlQuery q(db);
    q.prepare("SELECT m.chat, m.ts, c.name FROM messages m JOIN chats c ON c.jid = m.chat WHERE m.kind = 13 "
              "AND m.text IN ('missed_call','missed_video_call') AND m.out = 0 AND m.ts > ? "
              "ORDER BY m.ts DESC, m.rowid DESC");
    q.addBindValue(m_file.value("callsSeen").toLongLong());
    if (!q.exec()) {
        qWarning() << "calls count:" << q.lastError().text();
        return;
    }
    int n = 0;
    QString last;
    if (q.next()) { // the newest comes first
        n = 1;
        dayFormatter().applyCurrentTimeAsAnchor();
        const QLocale locale = m_region.locale();
        const QDateTime when = QDateTime::fromTime_t(q.value(1).toUInt());
        last = callerName(q.value(0).toString(), q.value(2).toString()) + QString::fromUtf8(" · ")
             + whenText(when, QDate::currentDate(), locale, timeFormat(locale, DateFormat::Short));
        while (q.next())
            n++;
    }
    if (n != m_missedNew || last != m_missedLast) {
        m_missedNew = n;
        m_missedLast = last;
        emit missedChanged();
    }
}

void CallsModel::reloadSoon()
{
    if (m_active && !m_soon.isActive())
        m_soon.start();
}

// ponytail: every change starts the list over (Init), which scrolls it to the top; the tab is shown
// for a moment and a call is rare, so no diff for the index mapper.
void CallsModel::reload()
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return;
    QSqlQuery q(db);
    if (!q.exec("SELECT m.chat, m.id, m.ts, m.out, m.text, m.extra, c.name, c.photo, c.kind "
                "FROM messages m JOIN chats c ON c.jid = m.chat WHERE m.kind = 13 "
                "AND m.text IN ('call','missed_call','video_call','missed_video_call') "
                "ORDER BY m.ts DESC, m.rowid DESC LIMIT 200")) {
        qWarning() << "calls:" << q.lastError().text();
        return;
    }
    dayFormatter().applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    const QString clock = timeFormat(locale, DateFormat::Short);
    const QDate today = QDate::currentDate();
    QList<QVariantMap> rows;
    while (q.next()) {
        const QString jid = q.value(0).toString();
        const QString code = q.value(4).toString();
        const bool out = q.value(3).toInt() != 0;
        const bool missed = code.startsWith("missed_");
        int secs = 0;
        bb::data::JsonDataAccess json;
        const QVariant extra = json.loadFromBuffer(q.value(5).toByteArray());
        if (!json.hasError() && extra.type() == QVariant::Map)
            secs = extra.toMap().value("secs").toInt();
        const QString name = callerName(jid, q.value(6).toString());
        QString detail = missed ? tr("Missed voice call")
                       : secs > 0 ? (out ? tr("Outgoing, %1") : tr("Incoming, %1")).arg(MediaData::clock(secs))
                       : out ? tr("Outgoing") : tr("Incoming");
        const QDateTime when = QDateTime::fromTime_t(q.value(2).toUInt());
        QVariantMap row;
        row["jid"] = jid;
        row["kind"] = q.value(8).toInt(); // CallsPage tells a group's row from a person's
        row["name"] = name;
        row["initials"] = initials(name);
        row["color"] = pickColor(jid, kColors, kColorCount);
        row["photo"] = q.value(7).toString();
        row["missed"] = missed;
        row["detail"] = detail;
        row["time"] = whenText(when, today, locale, clock);
        rows.append(row);
    }
    const bool wasEmpty = m_rows.isEmpty();
    m_rows = rows;
    emit itemsChanged(DataModelChangeType::Init);
    if (wasEmpty != m_rows.isEmpty())
        emit emptyChanged();
}
