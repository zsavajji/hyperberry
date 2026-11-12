#include "StoriesModel.h"

#include "EngineLink.h"
#include "colors.h"
#include "dates.h"

#include <bb/data/JsonDataAccess>
#include <bb/utility/i18n/Formatting>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <QImageReader>
#include <QLocale>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// Its own read-only connection to chats.db: only the engine writes it (PLAN.md §12).
static const char *const kDb = "stories";

// A story lives 24 h (docs/protocol.md): rows older than that are hidden even if the engine hasn't swept.
static const uint kLife = 24 * 60 * 60;

// A story's picture as a file: the downloaded photo scaled to w x h (made once, kept in data/cache/previews/ as
// <id>-<what>.jpg, which the engine wipes on unlink), else the story's inline thumbnail written out as it is.
// "" when the story has neither (a text story, a video whose thumbnail didn't come).
// ponytail: made on the UI thread, tens of ms on the Q10 and once per photo; a worker thread if the tab stutters.
static QString pictureFile(const QString &id, int kind, const QString &path, QSqlDatabase &db, const QString &poster,
                           const char *what, int w, int h, Qt::AspectRatioMode mode, int quality)
{
    const QString dir = QDir::homePath() + "/cache/previews/";
    if (kind == 1 && !path.isEmpty() && QFile::exists(path)) {
        const QString file = dir + id + "-" + what + ".jpg";
        if (QFile::exists(file))
            return file;
        QImageReader reader(path);
        QSize to = reader.size(); // from the header
        if (to.isValid()) {
            to.scale(w, h, mode);
            reader.setScaledSize(to);
            const QImage image = reader.read();
            if (!image.isNull() && QDir().mkpath(dir)) {
                if (image.save(file, "JPEG", quality))
                    return file;
                QFile::remove(file); // no half-written preview left to be taken for a good one
            }
        }
    }
    const QString file = dir + id + "-thumb.jpg";
    if (QFile::exists(file))
        return file;
    QSqlQuery q(db);
    q.prepare("SELECT thumb FROM stories WHERE poster = ? AND id = ?");
    q.addBindValue(poster);
    q.addBindValue(id);
    if (!q.exec() || !q.next())
        return QString();
    const QByteArray thumb = q.value(0).toByteArray();
    QFile f(file);
    if (thumb.isEmpty() || !QDir().mkpath(dir) || !f.open(QIODevice::WriteOnly))
        return QString();
    f.write(thumb);
    f.close();
    return file;
}

// The tile is 310 x 211 px on the Q10; its picture is made a little larger and the ImageView crops it.
static QString tileFile(const QString &id, int kind, const QString &path, QSqlDatabase &db, const QString &poster)
{
    return pictureFile(id, kind, path, db, poster, "tile", 320, 220, Qt::KeepAspectRatioByExpanding, 80);
}

// The viewer's picture fits the Q10's 720 px square, which a larger screen's ImageView scales up.
static QString viewFile(const QString &id, int kind, const QString &path, QSqlDatabase &db, const QString &poster)
{
    return pictureFile(id, kind, path, db, poster, "view", 720, 720, Qt::KeepAspectRatio, 85);
}

// A text story's colour: WhatsApp's ARGB number as "#RRGGBB", "" when it has none.
static QString argb(const QVariantMap &extra, const char *key)
{
    const uint v = extra.value(key).toUInt();
    return v ? QString("#%1").arg(v & 0xFFFFFF, 6, 16, QChar('0')) : QString();
}

static bool before(const QVariantMap &a, const QVariantMap &b)
{
    const bool au = a["unseen"].toInt() > 0, bu = b["unseen"].toInt() > 0;
    if (au != bu)
        return au;
    return a["ts"].toUInt() > b["ts"].toUInt();
}

StoriesModel::StoriesModel(EngineLink *engine, QObject *parent)
    : DataModel(parent)
    , m_engine(engine)
    , m_active(false)
    , m_unseen(0)
    , m_days(RelativeDateFormat::NonTimeCritical)
    , m_region(bb::system::LocaleType::Region)
{
    m_days.applyUserRegionLocale();
    QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", kDb);
    db.setDatabaseName(QDir::homePath() + "/chats.db");
    db.setConnectOptions("QSQLITE_OPEN_READONLY");
    m_soon.setSingleShot(true);
    m_soon.setInterval(250);
    connect(&m_soon, SIGNAL(timeout()), this, SLOT(reload()));
    connect(engine, SIGNAL(storiesChanged()), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(replied(QString, QString, QVariant)), this, SLOT(onReplied(QString, QString, QVariant)));
    connect(engine, SIGNAL(failureChanged()), this, SLOT(onFailure()));
    reloadSoon();
}

int StoriesModel::childCount(const QVariantList &indexPath)
{
    return indexPath.isEmpty() ? m_rows.size() : 0;
}

bool StoriesModel::hasChildren(const QVariantList &indexPath)
{
    return indexPath.isEmpty() && !m_rows.isEmpty();
}

QString StoriesModel::itemType(const QVariantList &) { return "item"; }

QVariant StoriesModel::data(const QVariantList &indexPath)
{
    return indexPath.size() == 1 ? m_rows.value(indexPath[0].toInt()) : QVariant();
}

void StoriesModel::setActive(bool on)
{
    if (on == m_active)
        return;
    m_active = on;
    if (on)
        reload();
    emit activeChanged();
}

void StoriesModel::reloadSoon()
{
    if (!m_soon.isActive())
        m_soon.start();
}

// ponytail: every change starts the list over (Init), which scrolls it to the top; a story arrives
// now and then and the tab is shown for a moment, so no diff for the index mapper. The 24 h cutoff is
// taken at each reload: a story that expires while nothing changes stays until the next event.
void StoriesModel::reload()
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return;
    const uint cutoff = QDateTime::currentDateTime().toTime_t() - kLife;
    QSqlQuery q(db);
    q.prepare("SELECT s.poster, s.id, s.ts, s.kind, s.text, s.path, s.extra, s.seen, s.name, c.name, c.photo "
              "FROM stories s LEFT JOIN chats c ON c.jid = s.poster WHERE s.ts > ? ORDER BY s.poster, s.ts");
    q.addBindValue(cutoff);
    if (!q.exec()) {
        qWarning() << "stories:" << q.lastError().text();
        return;
    }
    int unseenAll = 0;
    QList<QVariantMap> rows;
    if (m_active) {
        m_days.applyCurrentTimeAsAnchor();
        const QLocale locale = m_region.locale();
        const QString clock = timeFormat(locale, DateFormat::Short);
        const QDate today = QDate::currentDate();
        QString poster;
        QVariantMap row;
        bool looking = true; // for the tile: the first story not seen, else the newest, so on while the stories are seen
        while (q.next()) {
            const QString p = q.value(0).toString();
            const bool seen = q.value(7).toInt() != 0;
            if (!seen)
                ++unseenAll;
            if (p != poster) {
                if (!row.isEmpty())
                    rows.append(row);
                poster = p;
                row = QVariantMap();
                looking = true;
                QString name = q.value(9).toString();
                if (name.isEmpty())
                    name = q.value(8).toString();
                if (name.isEmpty())
                    name = p.endsWith("@s.whatsapp.net") ? "+" + p.section('@', 0, 0) : tr("Unknown");
                row["poster"] = p;
                row["name"] = name;
                row["initials"] = initials(name);
                row["color"] = pickColor(p, kColors, kColorCount);
                row["photo"] = q.value(10).toString();
                row["count"] = 0;
                row["unseen"] = 0;
            }
            row["count"] = row["count"].toInt() + 1;
            if (!seen)
                row["unseen"] = row["unseen"].toInt() + 1;
            row["ts"] = q.value(2).toUInt(); // oldest first, so the last one is the newest
            const QDateTime when = QDateTime::fromTime_t(q.value(2).toUInt());
            row["time"] = when.date() == today ? locale.toString(when.time(), clock)
                                               : dayLabel(when, today, locale, m_days);
            if (looking) { // this story is the tile unless an unseen one came before it
                looking = seen;
                const int kind = q.value(3).toInt();
                QVariantMap extra;
                bb::data::JsonDataAccess json;
                const QVariant parsed = json.loadFromBuffer(q.value(6).toByteArray());
                if (!json.hasError() && parsed.type() == QVariant::Map)
                    extra = parsed.toMap();
                const QString bg = argb(extra, "bg");
                row["kind"] = kind;
                row["text"] = q.value(4).toString().left(90);
                row["bg"] = bg.isEmpty() ? QString(pickColor(p, kColors, kColorCount)) : bg;
                row["fg"] = argb(extra, "fg").isEmpty() ? QString("#FFFFFF") : argb(extra, "fg");
                row["tile"] = kind == 0 ? QString() : tileFile(q.value(1).toString(), kind, q.value(5).toString(), db, p);
            }
        }
        if (!row.isEmpty())
            rows.append(row);
        qSort(rows.begin(), rows.end(), before);
    } else {
        while (q.next())
            if (q.value(7).toInt() == 0)
                ++unseenAll;
    }
    if (unseenAll != m_unseen) {
        m_unseen = unseenAll;
        emit unseenChanged();
    }
    if (m_active) {
        const bool wasEmpty = m_rows.isEmpty();
        m_rows = rows;
        emit itemsChanged(DataModelChangeType::Init);
        if (wasEmpty != m_rows.isEmpty())
            emit emptyChanged();
    }
}

static const char *const kStoryCols = "poster, id, ts, kind, text, path, extra, seen, duration, width, height";

QVariantMap StoriesModel::storyMap(QSqlQuery &q, QSqlDatabase &db)
{
    const QString poster = q.value(0).toString();
    const QString id = q.value(1).toString();
    const int kind = q.value(3).toInt();
    const QString path = q.value(5).toString();
    const bool here = !path.isEmpty() && QFile::exists(path);
    QVariantMap extra;
    bb::data::JsonDataAccess json;
    const QVariant parsed = json.loadFromBuffer(q.value(6).toByteArray());
    if (!json.hasError() && parsed.type() == QVariant::Map)
        extra = parsed.toMap();
    const QString bg = argb(extra, "bg");
    const QString fg = argb(extra, "fg");
    const QDateTime when = QDateTime::fromTime_t(q.value(2).toUInt());
    m_days.applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    QVariantMap row;
    row["id"] = id;
    row["poster"] = poster;
    row["kind"] = kind;
    row["text"] = q.value(4).toString();
    row["bg"] = bg.isEmpty() ? QString(pickColor(poster, kColors, kColorCount)) : bg;
    row["fg"] = fg.isEmpty() ? QString("#FFFFFF") : fg;
    row["seen"] = q.value(7).toInt() != 0;
    row["time"] = dayLabel(when, QDate::currentDate(), locale, m_days) + ", "
                + locale.toString(when.time(), timeFormat(locale, DateFormat::Short));
    row["duration"] = q.value(8).toInt();
    row["width"] = q.value(9).toInt();
    row["height"] = q.value(10).toInt();
    row["pic"] = kind == 0 ? QString() : viewFile(id, kind, path, db, poster);
    row["video"] = kind == 2 && here ? path : QString();
    row["ready"] = kind == 0 || here;
    return row;
}

QVariantList StoriesModel::storiesOf(const QString &poster)
{
    QVariantList list;
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return list;
    QSqlQuery q(db);
    q.prepare(QString("SELECT %1 FROM stories WHERE poster = ? AND ts > ? ORDER BY ts").arg(kStoryCols));
    q.addBindValue(poster);
    q.addBindValue(QDateTime::currentDateTime().toTime_t() - kLife);
    if (!q.exec()) {
        qWarning() << "stories:" << q.lastError().text();
        return list;
    }
    while (q.next())
        list.append(storyMap(q, db));
    return list;
}

QVariantMap StoriesModel::storyOf(const QString &poster, const QString &id)
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return QVariantMap();
    QSqlQuery q(db);
    q.prepare(QString("SELECT %1 FROM stories WHERE poster = ? AND id = ?").arg(kStoryCols));
    q.addBindValue(poster);
    q.addBindValue(id);
    return q.exec() && q.next() ? storyMap(q, db) : QVariantMap();
}

void StoriesModel::fetch(const QString &poster, const QString &id)
{
    QVariantMap args;
    args["poster"] = poster;
    args["id"] = id;
    const QString req = m_engine->request("story.get", args);
    if (!req.isEmpty())
        m_pending.insert(req, qMakePair(poster, id));
}

void StoriesModel::markSeen(const QString &poster, const QString &id)
{
    QVariantMap args;
    args["poster"] = poster;
    args["id"] = id;
    m_engine->request("story.seen", args);
}

void StoriesModel::onReplied(const QString &id, const QString &cmd, const QVariant &)
{
    if (cmd != "story.get" || !m_pending.contains(id))
        return;
    const QPair<QString, QString> story = m_pending.take(id);
    emit storyReady(story.first, story.second);
}

// ponytail: a failure names no request, so one of several fetches in flight failing ends them all.
void StoriesModel::onFailure()
{
    if (m_pending.isEmpty() || m_engine->failure().value("cmd").toString() != "story.get")
        return;
    const QList<QPair<QString, QString> > failed = m_pending.values();
    m_pending.clear();
    for (int i = 0; i < failed.size(); ++i)
        emit storyFailed(failed[i].first, failed[i].second);
}
