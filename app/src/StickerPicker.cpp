#include "StickerPicker.h"

#include "EngineLink.h"
#include "Moves.h"
#include "Sticker.h"

#include <QCoreApplication>
#include <QFutureWatcher>
#include <QtConcurrentRun>
#include <QUrl>

// At most this many pictures are downloaded or decoded at once: the engine is under a 30 MB cap and the phone has two cores.
static const int kJobs = 4;
// The grid's cells are 128 px stills; the preview shows the sticker's own file at the side the grid asks for (360 on the Q10).
static const int kGridSide = 128;

static QVariantList at(int group, int item)
{
    QVariantList path;
    path << group << item;
    return path;
}

// A cell's key for Moves: its group's title and its sticker (a sticker may be in both groups).
static QStringList cellKeys(const QString &title, const QStringList &hashes)
{
    QStringList keys;
    foreach (const QString &hash, hashes)
        keys << title + ":" + hash;
    return keys;
}

// The grid's model: the picker's groups, which it reads in place. The signals are protected (Qt 4), so it sends them.
class StickerModel : public bb::cascades::DataModel
{
public:
    explicit StickerModel(StickerPicker *picker) : DataModel(picker), m_picker(picker) {}

    int childCount(const QVariantList &indexPath)
    {
        if (indexPath.isEmpty())
            return m_picker->m_groups.size();
        if (indexPath.size() == 1)
            return m_picker->m_groups.value(indexPath[0].toInt()).hashes.size();
        return 0;
    }
    bool hasChildren(const QVariantList &indexPath) { return indexPath.size() < 2 && childCount(indexPath) > 0; }
    QString itemType(const QVariantList &indexPath) { return indexPath.size() == 1 ? "header" : "item"; }
    QVariant data(const QVariantList &indexPath) { return m_picker->rowData(indexPath); }

    void updated(const QVariantList &path) { emit itemUpdated(path); }
    // Moves maps where each held header and cell went, so the grid keeps them: a bare AddRemove made every cell on screen
    // again, and the page blinked.
    void changed(Moves *moves)
    {
        emit itemsChanged(bb::cascades::DataModelChangeType::AddRemove,
                          QSharedPointer<bb::cascades::DataModel::IndexMapper>(moves));
    }

private:
    StickerPicker *m_picker;
};

StickerPicker::StickerPicker(EngineLink *engine, QObject *parent)
    : QObject(parent)
    , m_engine(engine)
    , m_model(new StickerModel(this))
    , m_loading(false)
    , m_decoding(0)
    , m_previewSide(0)
    , m_previewTok(0)
{
    connect(engine, SIGNAL(replied(QString, QString, QVariant)), this, SLOT(onReplied(QString, QString, QVariant)));
    connect(engine, SIGNAL(failed(QString, QString, QString)), this, SLOT(onFailed(QString)));
}

bb::cascades::DataModel *StickerPicker::model() const
{
    return m_model;
}

// ponytail: an engine that is down leaves the page empty until it is opened again; no ask on reconnecting.
void StickerPicker::load()
{
    m_idGet = m_engine ? m_engine->request("stickers.get", QVariantMap()) : QString();
    m_loading = !m_idGet.isEmpty();
    emit stateChanged();
}

// A group of the stickers.get list under its title, left out when empty. The engine's path is a file that is here, else "":
// a path learned from stickers.file stays.
void StickerPicker::addGroup(QList<Group> *groups, const QVariantList &stickers, const QString &title)
{
    Group g;
    g.title = title;
    foreach (const QVariant &s, stickers) {
        const QVariantMap sticker = s.toMap();
        const QString hash = sticker["hash"].toString();
        if (hash.isEmpty())
            continue;
        g.hashes << hash;
        if (!sticker["path"].toString().isEmpty() || !m_path.contains(hash))
            m_path.insert(hash, sticker["path"].toString());
    }
    if (!g.hashes.isEmpty())
        *groups << g;
}

// The two lists, Favourites then Recent, as one change for the grid.
void StickerPicker::setLists(const QVariantMap &lists)
{
    QList<Group> groups;
    addGroup(&groups, lists["faves"].toList(), QCoreApplication::translate("StickersPage", "Favourites"));
    m_faves.clear();
    if (!groups.isEmpty())
        m_faves = QSet<QString>::fromList(groups.first().hashes);
    addGroup(&groups, lists["recent"].toList(), QCoreApplication::translate("StickersPage", "Recent"));

    Moves *moves = new Moves;
    foreach (const Group &g, m_groups) {
        moves->oldGroups << g.title;
        moves->oldItems << cellKeys(g.title, g.hashes);
    }
    m_groups = groups;
    for (int g = 0; g < m_groups.size(); ++g) {
        moves->groups.insert(m_groups.at(g).title, g);
        const QStringList keys = cellKeys(m_groups.at(g).title, m_groups.at(g).hashes);
        for (int i = 0; i < keys.size(); ++i)
            moves->items.insert(keys.at(i), qMakePair(g, i));
    }
    m_model->changed(moves);
}

void StickerPicker::onReplied(const QString &id, const QString &, const QVariant &result)
{
    const QVariantMap r = result.toMap();
    if (m_files.contains(id)) {
        const QString hash = m_files.take(id);
        const QString path = r["path"].toString();
        if (!path.isEmpty()) {
            m_path.insert(hash, path);
            decode(hash, path, kGridSide);
        }
        pump();
    } else if (id == m_idGet) {
        m_idGet.clear();
        setLists(r);
        m_loading = false;
        emit stateChanged();
    } else if (m_faveIds.remove(id)) {
        load(); // the engine has changed it: the lists are asked again
    } else if (id == m_previewId) {
        m_previewId.clear();
        const QString path = r["path"].toString();
        if (m_previewView && !path.isEmpty()) {
            m_path.insert(m_previewHash, path);
            decode(m_previewHash, path, m_previewSide);
        }
    }
}

// Every failure of a request out, with its id: a picture that failed isn't asked again by this page (m_asked keeps it), and
// the next in the queue takes its place.
void StickerPicker::onFailed(const QString &id)
{
    if (m_files.remove(id)) {
        pump();
    } else if (id == m_idGet) {
        m_idGet.clear();
        m_loading = false;
        emit stateChanged();
    } else if (id == m_previewId) {
        m_previewId.clear();
    } else {
        m_faveIds.remove(id);
    }
}

void StickerPicker::fave(const QString &hash, bool on)
{
    if (!m_engine)
        return;
    QVariantMap args;
    args["hash"] = hash;
    args["on"] = on;
    const QString id = m_engine->request("sticker.fave", args);
    if (!id.isEmpty())
        m_faveIds.insert(id);
}

// The row of a cell. Reading a sticker's row is what asks for its picture, so only the cells the grid shows cost anything.
QVariant StickerPicker::rowData(const QVariantList &indexPath)
{
    const int g = indexPath.value(0).toInt();
    if (g < 0 || g >= m_groups.size())
        return QVariant();
    if (indexPath.size() == 1)
        return m_groups.at(g).title;
    const QString hash = m_groups.at(g).hashes.value(indexPath.value(1).toInt());
    if (hash.isEmpty())
        return QVariant();
    ask(hash);
    QVariantMap row;
    row["hash"] = hash;
    row["image"] = QVariant::fromValue(m_images.value(hash));
    row["hasImage"] = m_images.contains(hash);
    row["fave"] = m_faves.contains(hash);
    return row;
}

// A sticker's picture is asked once, the first time a cell wants it, and waits in a queue for a place.
void StickerPicker::ask(const QString &hash)
{
    if (m_asked.contains(hash))
        return;
    m_asked.insert(hash);
    m_queue.append(hash);
    pump();
}

// Starts what the places allow. The last asked goes first: it is the cell the user is looking at. A file that is here is
// decoded; any other is downloaded first (stickers.file), and decoded when it comes.
void StickerPicker::pump()
{
    while (m_files.size() + m_decoding < kJobs && !m_queue.isEmpty()) {
        const QString hash = m_queue.takeLast();
        const QString path = m_path.value(hash);
        if (!path.isEmpty()) {
            decode(hash, path, kGridSide);
            continue;
        }
        QVariantMap args;
        args["hash"] = hash;
        const QString id = m_engine ? m_engine->requestQuiet("stickers.file", args) : QString();
        if (id.isEmpty()) {
            m_asked.remove(hash); // the engine is down: the next read of the cell asks again
            break;
        }
        m_files.insert(id, hash);
    }
}

// Decodes on a worker thread; onDecoded takes the result on this one. The grid's side is the cells' picture, any other the
// preview's.
void StickerPicker::decode(const QString &hash, const QString &path, int side)
{
    QFutureWatcher<QString> *watcher = new QFutureWatcher<QString>(this);
    watcher->setProperty("hash", hash);
    watcher->setProperty("side", side);
    watcher->setProperty("tok", m_previewTok);
    connect(watcher, SIGNAL(finished()), this, SLOT(onDecoded()));
    watcher->setFuture(QtConcurrent::run(&Sticker::gridPicture, hash, path, side));
    if (side == kGridSide)
        ++m_decoding;
}

void StickerPicker::onDecoded()
{
    QFutureWatcher<QString> *watcher = static_cast<QFutureWatcher<QString> *>(sender());
    watcher->deleteLater();
    const QString file = watcher->result(); // the picture's PNG, "" when it failed
    if (watcher->property("side").toInt() != kGridSide) {
        if (watcher->property("tok").toInt() == m_previewTok && m_previewView && !file.isEmpty())
            m_previewView->setImage(bb::cascades::Image(QUrl::fromLocalFile(file)));
        return;
    }
    --m_decoding;
    if (!file.isEmpty()) {
        const QString hash = watcher->property("hash").toString();
        m_images.insert(hash, bb::cascades::Image(QUrl::fromLocalFile(file)));
        // Every cell with this sticker is read again (it may be in Favourites and in Recent).
        for (int g = 0; g < m_groups.size(); ++g) {
            for (int i = 0; i < m_groups.at(g).hashes.size(); ++i) {
                if (m_groups.at(g).hashes.at(i) == hash)
                    m_model->updated(at(g, i));
            }
        }
    }
    pump();
}

// The sticker's own file, for the view: the grid's picture is there already. An answer after endPreview, or for another
// sticker, finds m_previewId changed and is dropped (onReplied); so is a decode (m_previewTok).
void StickerPicker::preview(QObject *imageView, const QString &hash, int side)
{
    m_previewView = qobject_cast<bb::cascades::ImageView *>(imageView);
    m_previewHash = hash;
    m_previewSide = side;
    m_previewId.clear();
    ++m_previewTok;
    if (!m_previewView)
        return;
    const QString path = m_path.value(hash);
    if (!path.isEmpty()) {
        decode(hash, path, side);
        return;
    }
    QVariantMap args;
    args["hash"] = hash;
    if (m_engine)
        m_previewId = m_engine->requestQuiet("stickers.file", args);
}

void StickerPicker::endPreview()
{
    m_previewView = 0;
    m_previewId.clear();
    ++m_previewTok;
}
