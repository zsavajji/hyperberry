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

// The tile is 310 x 211 px on the Q10; the picture is made a little larger and the ImageView crops it.
static const int TileW = 320;
static const int TileH = 220;

// The tile's picture as a file: the downloaded photo scaled down (made once, kept in data/cache/previews/,
// which the engine wipes on unlink), else the story's inline thumbnail written out as it is. "" when
// the story has neither (a text story, a video whose thumbnail didn't come).
// ponytail: made on the UI thread, tens of ms on the Q10 and once per photo; a worker thread if the tab stutters.
static QString tileFile(const QString &id, int kind, const QString &path, QSqlDatabase &db, const QString &poster)
{
    const QString dir = QDir::homePath() + "/cache/previews/";
    if (kind == 1 && !path.isEmpty() && QFile::exists(path)) {
        const QString file = dir + id + "-tile.jpg";
        if (QFile::exists(file))
            return file;
        QImageReader reader(path);
        QSize to = reader.size(); // from the header
        if (to.isValid()) {
            to.scale(TileW, TileH, Qt::KeepAspectRatioByExpanding);
            reader.setScaledSize(to);
            const QImage image = reader.read();
            if (!image.isNull() && QDir().mkpath(dir)) {
                if (image.save(file, "JPEG", 80))
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
