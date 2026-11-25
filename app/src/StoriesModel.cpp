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
#include <QMap>
#include <QMetaObject>
#include <QPair>
#include <QRunnable>
#include <QSqlDatabase>
#include <QSqlError>
#include <QSqlQuery>

using namespace bb::cascades;
using namespace bb::utility::i18n;

// Its own read-only connection to chats.db: only the engine writes it (PLAN.md §12).
static const char *const kDb = "stories";

// A story lives 24 h (docs/protocol.md): rows older than that are hidden even if the engine hasn't swept.
static const uint kLife = 24 * 60 * 60;

// Where a story's picture is kept: data/cache/previews/<id>-<what>.jpg, which the engine wipes on unlink.
static QString previewFile(const QString &id, const QString &what)
{
    return QDir::homePath() + "/cache/previews/" + id + "-" + what + ".jpg";
}

// A story's picture as a file: the downloaded photo scaled (made once by make(), on a worker thread), else the
// story's inline thumbnail written out as it is. A photo that is here but not scaled yet gives its thumbnail and
// sets *pending, for the caller to have the scaled one made. "" when the story has neither (a text story, a
// video whose thumbnail didn't come).
static QString pictureFile(const QString &id, int kind, const QString &path, QSqlDatabase &db, const QString &poster,
                           const char *what, bool *pending)
{
    if (pending)
        *pending = false;
    if (kind == 1 && !path.isEmpty() && QFile::exists(path)) {
        const QString scaled = previewFile(id, what);
        if (QFile::exists(scaled))
            return scaled;
        if (pending)
            *pending = true;
    }
    const QString file = previewFile(id, "thumb");
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
    if (thumb.isEmpty() || !QDir().mkpath(QFileInfo(file).path()) || !f.open(QIODevice::WriteOnly))
        return QString();
    f.write(thumb);
    f.close();
    return file;
}

// The photo src scaled to w x h as a JPEG at file. Off the UI thread: QImage only, never QPixmap.
static bool scalePicture(const QString &src, const QString &file, int w, int h, Qt::AspectRatioMode mode, int quality)
{
    QImageReader reader(src);
    QSize to = reader.size(); // from the header
    if (!to.isValid() || !QDir().mkpath(QFileInfo(file).path()))
        return false;
    to.scale(w, h, mode);
    reader.setScaledSize(to);
    const QImage image = reader.read();
    if (image.isNull())
        return false;
    // Written aside and renamed: the UI thread takes any file at that path for a finished one.
    const QString part = file + ".part";
    if (image.save(part, "JPEG", quality) && QFile::rename(part, file))
        return true;
    QFile::remove(part);
    return false;
}

// One preview to scale, run on the model's pool; the model hears of it on the UI thread (pictureMade).
class PictureJob : public QRunnable
{
public:
    PictureJob(QObject *model, const QString &poster, const QString &id, const QString &what, const QString &src,
               const QString &file, int w, int h, Qt::AspectRatioMode mode, int quality)
        : m_model(model), m_poster(poster), m_id(id), m_what(what), m_src(src), m_file(file)
        , m_w(w), m_h(h), m_mode(mode), m_quality(quality)
    {
    }
    void run()
    {
        const bool ok = scalePicture(m_src, m_file, m_w, m_h, m_mode, m_quality);
        QMetaObject::invokeMethod(m_model, "pictureMade", Qt::QueuedConnection, Q_ARG(QString, m_poster),
                                  Q_ARG(QString, m_id), Q_ARG(QString, m_what), Q_ARG(bool, ok));
    }

private:
    QObject *m_model;
    QString m_poster, m_id, m_what, m_src, m_file;
    int m_w, m_h;
    Qt::AspectRatioMode m_mode;
    int m_quality;
};

// A text story's colour: WhatsApp's ARGB number as "#RRGGBB", "" when it has none.
static QString argb(const QVariantMap &extra, const char *key)
{
    const uint v = extra.value(key).toUInt();
    return v ? QString("#%1").arg(v & 0xFFFFFF, 6, 16, QChar('0')) : QString();
}

// The poster of our own stories (docs/protocol.md).
static const char *const kOwn = "me";

static bool before(const QVariantMap &a, const QVariantMap &b)
{
    if (a["own"].toBool() != b["own"].toBool())
        return a["own"].toBool();
    if (a["muted"].toBool() != b["muted"].toBool())
        return b["muted"].toBool(); // the ones the phone muted go below
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
    , m_region(bb::system::LocaleType::Region)
{
    m_pool.setMaxThreadCount(1); // one at a time: the other Krait core stays with the UI and the video
    QSqlDatabase db = QSqlDatabase::addDatabase("QSQLITE", kDb);
    db.setDatabaseName(QDir::homePath() + "/chats.db");
    db.setConnectOptions("QSQLITE_OPEN_READONLY");
    m_soon.setSingleShot(true);
    m_soon.setInterval(250);
    connect(&m_soon, SIGNAL(timeout()), this, SLOT(reload()));
    connect(engine, SIGNAL(storiesChanged()), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(connectedChanged()), this, SLOT(reloadSoon()));
    connect(engine, SIGNAL(storyViewsChanged()), this, SIGNAL(viewsChanged()));
    connect(engine, SIGNAL(replied(QString, QString, QVariant)), this, SLOT(onReplied(QString, QString, QVariant)));
    connect(engine, SIGNAL(failureChanged()), this, SLOT(onFailure()));
    connect(engine, SIGNAL(storyOp(QString, QString, bool)), this, SLOT(onStoryOp(QString, QString, bool)));
    reloadSoon();
}

QString StoriesModel::personName(const QString &chatName, const QString &ownName, const QString &key) const
{
    if (!chatName.isEmpty())
        return chatName;
    if (!ownName.isEmpty())
        return ownName;
    return key.endsWith("@s.whatsapp.net") ? "+" + key.section('@', 0, 0) : tr("Unknown");
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

// A row Cascades asks for whose tile is still the thumbnail has its sharp one made now, so only the tiles on
// screen are made. ponytail: a photo that won't decode is tried again each time its tile is asked for.
QVariant StoriesModel::data(const QVariantList &indexPath)
{
    if (indexPath.size() != 1)
        return QVariant();
    const QVariantMap row = m_rows.value(indexPath[0].toInt());
    if (!row.value("tileSrc").toString().isEmpty())
        make(row.value("poster").toString(), row.value("tileId").toString(), "tile", row.value("tileSrc").toString());
    return row;
}

// The tile is 310 x 211 px on the Q10, so its picture is made a little larger and the ImageView crops it; the
// viewer's picture fits the Q10's 720 px square, which a larger screen's ImageView scales up.
void StoriesModel::make(const QString &poster, const QString &id, const char *what, const QString &src)
{
    const QString file = previewFile(id, what);
    if (QFile::exists(file) || m_making.contains(file))
        return;
    const bool tile = qstrcmp(what, "tile") == 0;
    m_making.insert(file);
    m_pool.start(new PictureJob(this, poster, id, what, src, file, tile ? 320 : 720, tile ? 220 : 720,
                                tile ? Qt::KeepAspectRatioByExpanding : Qt::KeepAspectRatio, tile ? 80 : 85));
}

// A preview is made (or not): a tile's row takes it, the viewer is told to read the story again.
void StoriesModel::pictureMade(const QString &poster, const QString &id, const QString &what, bool ok)
{
    m_making.remove(previewFile(id, what));
    if (!ok)
        return;
    if (what == "view") {
        emit storyReady(poster, id);
        return;
    }
    for (int i = 0; i < m_rows.size(); ++i) {
        QVariantMap &row = m_rows[i];
        if (row.value("poster").toString() == poster && row.value("tileId").toString() == id) {
            row["tile"] = previewFile(id, what);
            row["tileSrc"] = QString();
            emit itemUpdated(QVariantList() << i);
            return;
        }
    }
}

// The viewer is about to show the story, or the one after: a photo that is here gets its sharp picture made.
void StoriesModel::prepare(const QString &poster, const QString &id)
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return;
    QSqlQuery q(db);
    q.prepare("SELECT kind, path FROM stories WHERE poster = ? AND id = ?");
    q.addBindValue(poster);
    q.addBindValue(id);
    if (!q.exec() || !q.next() || q.value(0).toInt() != 1)
        return;
    const QString path = q.value(1).toString();
    if (!path.isEmpty() && QFile::exists(path))
        make(poster, id, "view", path);
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
    q.prepare("SELECT s.poster, s.id, s.ts, s.kind, s.text, s.path, s.extra, s.seen, s.name, c.name, c.photo, "
              "EXISTS(SELECT 1 FROM story_muted m WHERE m.poster = s.poster) "
              "FROM stories s LEFT JOIN chats c ON c.jid = s.poster WHERE s.ts > ? ORDER BY s.poster, s.ts");
    q.addBindValue(cutoff);
    if (!q.exec()) {
        qWarning() << "stories:" << q.lastError().text();
        return;
    }
    int unseenAll = 0;
    // Each poster's newest unseen story's time and the poster's name (the query runs by poster, then time).
    QMap<QString, QPair<uint, QString> > unseenBy;
    QList<QVariantMap> rows;
    if (m_active) {
        dayFormatter().applyCurrentTimeAsAnchor();
        const QLocale locale = m_region.locale();
        const QString clock = timeFormat(locale, DateFormat::Short);
        const QDate today = QDate::currentDate();
        QString poster;
        QVariantMap row;
        bool looking = true; // for the tile: the first story not seen, else the newest, so on while the stories are seen
        while (q.next()) {
            const QString p = q.value(0).toString();
            const bool seen = q.value(7).toInt() != 0;
            const bool muted = q.value(11).toInt() != 0;
            if (!seen && !muted) { // a muted poster's stories don't call for the badge
                ++unseenAll;
                unseenBy[p] = qMakePair(q.value(2).toUInt(), personName(q.value(9).toString(), q.value(8).toString(), p));
            }
            if (p != poster) {
                if (!row.isEmpty())
                    rows.append(row);
                poster = p;
                row = QVariantMap();
                looking = true;
                const QString name = personName(q.value(9).toString(), q.value(8).toString(), p);
                const bool own = p == kOwn;
                row["poster"] = p;
                row["own"] = own;
                row["muted"] = muted;
                // Ours: "My story", on a tile with our own name's initials ("#" until the link says one).
                row["name"] = own ? tr("My story") : name;
                row["initials"] = initials(own ? m_engine->link().value("name").toString() : name);
                row["color"] = pickColor(p, kColors, kColorCount);
                row["photo"] = q.value(10).toString();
                if (own) { // our own picture, as the profile page shows it
                    const QString me = QDir::homePath() + "/profile/me.jpg";
                    row["photo"] = QFile::exists(me) ? me : QString();
                }
                row["count"] = 0;
                row["unseen"] = 0;
            }
            row["count"] = row["count"].toInt() + 1;
            if (!seen)
                row["unseen"] = row["unseen"].toInt() + 1;
            row["ts"] = q.value(2).toUInt(); // oldest first, so the last one is the newest
            const QDateTime when = QDateTime::fromTime_t(q.value(2).toUInt());
            row["time"] = when.date() == today ? locale.toString(when.time(), clock)
                                               : dayLabel(when, today, locale, dayFormatter());
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
                // A photo here without its scaled tile shows the thumbnail; data() has the tile made when it is asked for.
                const QString id = q.value(1).toString();
                bool pending = false;
                row["tile"] = kind == 0 ? QString() : pictureFile(id, kind, q.value(5).toString(), db, p, "tile", &pending);
                row["tileId"] = pending ? id : QString();
                row["tileSrc"] = pending ? q.value(5).toString() : QString();
            }
        }
        if (!row.isEmpty())
            rows.append(row);
        qSort(rows.begin(), rows.end(), before);
    } else {
        while (q.next()) {
            if (q.value(7).toInt() == 0 && q.value(11).toInt() == 0) {
                const QString p = q.value(0).toString();
                ++unseenAll;
                unseenBy[p] = qMakePair(q.value(2).toUInt(), personName(q.value(9).toString(), q.value(8).toString(), p));
            }
        }
    }
    QList<QPair<uint, QString> > newest = unseenBy.values();
    qSort(newest.begin(), newest.end(), qGreater<QPair<uint, QString> >());
    QStringList names;
    for (int i = 0; i < newest.size() && i < 10; ++i)
        names << newest.at(i).second;
    const QString unseenNames = names.join(", ");
    if (unseenAll != m_unseen || unseenNames != m_unseenNames) {
        m_unseen = unseenAll;
        m_unseenNames = unseenNames;
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
    dayFormatter().applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    QVariantMap row;
    row["id"] = id;
    row["poster"] = poster;
    row["kind"] = kind;
    row["text"] = q.value(4).toString();
    row["bg"] = bg.isEmpty() ? QString(pickColor(poster, kColors, kColorCount)) : bg;
    row["fg"] = fg.isEmpty() ? QString("#FFFFFF") : fg;
    row["seen"] = q.value(7).toInt() != 0;
    row["time"] = dayLabel(when, QDate::currentDate(), locale, dayFormatter()) + ", "
                + locale.toString(when.time(), timeFormat(locale, DateFormat::Short));
    row["duration"] = q.value(8).toInt();
    row["width"] = q.value(9).toInt();
    row["height"] = q.value(10).toInt();
    row["pic"] = kind == 0 ? QString() : pictureFile(id, kind, path, db, poster, "view", 0);
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

QVariantList StoriesModel::viewersOf(const QString &id)
{
    QVariantList list;
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return list;
    QSqlQuery q(db);
    q.prepare("SELECT v.viewer, v.ts, v.name, c.name, c.photo FROM story_views v "
              "LEFT JOIN chats c ON c.jid = v.viewer WHERE v.id = ? ORDER BY v.ts DESC");
    q.addBindValue(id);
    if (!q.exec()) {
        qWarning() << "stories:" << q.lastError().text();
        return list;
    }
    dayFormatter().applyCurrentTimeAsAnchor();
    const QLocale locale = m_region.locale();
    const QString clock = timeFormat(locale, DateFormat::Short);
    const QDate today = QDate::currentDate();
    while (q.next()) {
        const QString key = q.value(0).toString();
        const QString name = personName(q.value(3).toString(), q.value(2).toString(), key);
        const QDateTime when = QDateTime::fromTime_t(q.value(1).toUInt());
        QVariantMap row;
        row["jid"] = key;
        row["name"] = name;
        row["initials"] = initials(name);
        row["color"] = pickColor(key, kColors, kColorCount);
        row["photo"] = q.value(4).toString();
        row["time"] = when.date() == today ? locale.toString(when.time(), clock)
                                           : dayLabel(when, today, locale, dayFormatter()) + ", "
                                                 + locale.toString(when.time(), clock);
        list.append(row);
    }
    return list;
}

int StoriesModel::viewCount(const QString &id)
{
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return 0;
    QSqlQuery q(db);
    q.prepare("SELECT count(*) FROM story_views WHERE id = ?");
    q.addBindValue(id);
    return q.exec() && q.next() ? q.value(0).toInt() : 0;
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

// Sends cmd with args plus the poster and the story, and remembers it until it is answered.
void StoriesModel::ask(const QString &cmd, const QString &poster, const QString &id, QVariantMap args)
{
    args["poster"] = poster;
    args["id"] = id;
    const QString req = m_engine->request(cmd, args);
    if (!req.isEmpty())
        m_pending.insert(req, QStringList() << cmd << poster << id);
}

QVariantMap StoriesModel::posterOf(const QString &poster)
{
    QVariantMap row;
    QSqlDatabase db = QSqlDatabase::database(kDb, false);
    if (!db.isOpen() && !db.open())
        return row;
    QSqlQuery q(db);
    q.prepare("SELECT s.name, c.name, c.photo FROM stories s LEFT JOIN chats c ON c.jid = s.poster "
              "WHERE s.poster = ? AND s.ts > ? LIMIT 1");
    q.addBindValue(poster);
    q.addBindValue(QDateTime::currentDateTime().toTime_t() - kLife);
    if (!q.exec() || !q.next())
        return row;
    const bool own = poster == kOwn;
    const QString name = personName(q.value(1).toString(), q.value(0).toString(), poster);
    QString photo = q.value(2).toString();
    if (own) {
        const QString me = QDir::homePath() + "/profile/me.jpg";
        photo = QFile::exists(me) ? me : QString();
    }
    row["poster"] = poster;
    row["own"] = own;
    row["name"] = own ? tr("My story") : name;
    row["initials"] = initials(own ? m_engine->link().value("name").toString() : name);
    row["color"] = pickColor(poster, kColors, kColorCount);
    row["photo"] = photo;
    return row;
}

void StoriesModel::fetch(const QString &poster, const QString &id)
{
    ask("story.get", poster, id, QVariantMap());
}

void StoriesModel::markSeen(const QString &poster, const QString &id)
{
    if (poster == kOwn) // born seen
        return;
    ask("story.seen", poster, id, QVariantMap());
}

void StoriesModel::reply(const QString &poster, const QString &id, const QString &text)
{
    QVariantMap args;
    args["text"] = text;
    ask("story.reply", poster, id, args);
}

void StoriesModel::react(const QString &poster, const QString &id, const QString &emoji)
{
    QVariantMap args;
    args["emoji"] = emoji;
    ask("story.react", poster, id, args);
}

void StoriesModel::onReplied(const QString &id, const QString &, const QVariant &)
{
    if (!m_pending.contains(id))
        return;
    const QStringList was = m_pending.take(id);
    if (was[0] == "story.get")
        emit storyReady(was[1], was[2]);
    else if (was[0] == "story.reply")
        emit replySent(was[1], was[2]);
}

// ponytail: a failure names no request, so one of several in flight failing ends them all of that cmd.
void StoriesModel::onFailure()
{
    if (m_pending.isEmpty())
        return;
    const QString cmd = m_engine->failure().value("cmd").toString();
    const QList<QString> ids = m_pending.keys();
    for (int i = 0; i < ids.size(); ++i) {
        const QStringList was = m_pending.value(ids[i]);
        if (was[0] != cmd)
            continue;
        m_pending.remove(ids[i]);
        if (cmd == "story.get")
            emit storyFailed(was[1], was[2]);
        else if (cmd == "story.reply")
            emit replyFailed(was[1], was[2]);
        else if (cmd == "story.react")
            emit reactFailed(was[1], was[2]);
    }
}

void StoriesModel::onStoryOp(const QString &cmd, const QString &, bool ok)
{
    if (ok)
        return;
    if (cmd == "post")
        emit postFailed();
    else if (cmd == "delete")
        emit deleteFailed();
}
