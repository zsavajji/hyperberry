#ifndef STORIESMODEL_H
#define STORIESMODEL_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <bb/utility/i18n/RelativeDateFormatter>
#include <QHash>
#include <QList>
#include <QSet>
#include <QStringList>
#include <QSqlDatabase>
#include <QSqlQuery>
#include <QThreadPool>
#include <QTimer>
#include <QVariantMap>

class EngineLink;
class Outbox;

// The Stories tab: one row per poster with a story under 24 h old (docs/protocol.md, `stories`), the
// ones with something unseen first, then by the newest story. A flat list of maps: poster, name,
// initials, color, photo (the chat's picture, "" when none), count, unseen, muted (the phone muted the poster's
// updates, the engine's story_muted: below the others, greyed, and not in the badge), time, and the tile: kind
// (0 text, 1 photo, 2 video), tile (a small JPEG's path, "" when none), bg and fg (a text story's colours)
// and text. The tile is the first story not yet seen, else the newest. A photo's tile is its thumbnail until
// the sharp one is made, on a worker thread when Cascades first asks for the row (the row has tileId and
// tileSrc, the story and its photo, till then), and then the row is updated. Read-only on chats.db, like
// CallsModel; the list reloads only while `active` (the tab is shown), but `unseen`, all posters'
// unseen stories (the tab's badge), is kept up to date always. Our own stories (poster "me") are one row,
// "My story", always first, with `own` true and nothing unseen. The viewer (StoryViewer.qml) reads one
// poster's stories through storiesOf and asks the engine for a file through fetch. Who viewed one of our
// stories (the engine's story_views) is viewersOf, newest first, and how many viewCount.
class StoriesModel : public bb::cascades::DataModel
{
    Q_OBJECT
    Q_PROPERTY(bool active READ active WRITE setActive NOTIFY activeChanged)
    Q_PROPERTY(bool empty READ empty NOTIFY emptyChanged)
    Q_PROPERTY(int unseen READ unseen NOTIFY unseenChanged)
    // Who posted them: the names of the posters with unseen, unmuted stories, newest first, ", " between, ten at most
    // (the Active Frame's Stories row, Cover.qml).
    Q_PROPERTY(QString unseenNames READ unseenNames NOTIFY unseenChanged)
public:
    StoriesModel(EngineLink *engine, Outbox *outbox, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);
    bool active() const { return m_active; }
    void setActive(bool on);
    bool empty() const { return m_rows.isEmpty(); }
    int unseen() const { return m_unseen; }
    QString unseenNames() const { return m_unseenNames; }

    // The poster's stories under 24 h old, oldest first, as storyOf's maps.
    Q_INVOKABLE QVariantList storiesOf(const QString &poster);
    // One story: id, poster, kind (0 text, 1 photo, 2 video), text, bg and fg (a text story's colours),
    // seen, time ("Today, 14:03"), duration (seconds, 0 if unknown), width, height, pic (the picture
    // to show: a photo sized for the screen, else the inline thumbnail, "" for a text story; a photo's is the
    // thumbnail until prepare has made the sized one), video (the file's path, "" until it is here) and ready
    // (everything it shows is here). {} for none.
    Q_INVOKABLE QVariantMap storyOf(const QString &poster, const QString &id);
    // A poster as the tile row has them (poster, own, name, initials, color, photo), for opening their stories from a chat; {} when
    // none of their stories is left.
    Q_INVOKABLE QVariantMap posterOf(const QString &poster);
    // Who viewed our story id, newest first, one map each: jid (the viewer's key), name, initials, color, photo
    // ("" when none) and time (the clock alone when it was today, else the day and the clock).
    Q_INVOKABLE QVariantList viewersOf(const QString &id);
    // How many viewed our story id; 0 when none or unknown.
    Q_INVOKABLE int viewCount(const QString &id);
    // Makes a photo story's sized picture on a worker thread if it isn't made; storyReady says when it is.
    Q_INVOKABLE void prepare(const QString &poster, const QString &id);
    // Asks the engine for the story's file (story.get); storyReady or storyFailed answers.
    Q_INVOKABLE void fetch(const QString &poster, const QString &id);
    // The viewer showed it: the engine marks it seen and sends the read receipt (story.seen).
    Q_INVOKABLE void markSeen(const QString &poster, const QString &id);
    // A text to the poster's chat quoting the story (story.reply), and an emoji on the story (story.react);
    // replySent, replyFailed and reactFailed answer.
    Q_INVOKABLE void reply(const QString &poster, const QString &id, const QString &text);
    Q_INVOKABLE void react(const QString &poster, const QString &id, const QString &emoji);

signals:
    void storyReady(const QString &poster, const QString &id);
    void storyFailed(const QString &poster, const QString &id);
    void replySent(const QString &poster, const QString &id);
    void replyFailed(const QString &poster, const QString &id);
    void reactFailed(const QString &poster, const QString &id);
    // Our own story could not be posted, or taken back (the engine's storyop event with ok false).
    void postFailed();
    void deleteFailed();
    void activeChanged();
    void emptyChanged();
    void unseenChanged();
    // The viewers of one of our stories changed: read viewersOf / viewCount again.
    void viewsChanged();

private slots:
    void reloadSoon();
    void reload();
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);
    void onFailure();
    void onStoryOp(const QString &cmd, const QString &id, bool ok);
    // A job of make() is done, ok when the file is there; called by name from the worker, queued onto the UI thread.
    void pictureMade(const QString &poster, const QString &id, const QString &what, bool ok);

private:
    QVariantMap storyMap(QSqlQuery &q, QSqlDatabase &db);
    // Queues the scaling of src to the picture "tile" or "view" of story id, once; the pool does one at a time.
    void make(const QString &poster, const QString &id, const char *what, const QString &src);
    // The name to show: the chat's, else the one that came with the row, else "+number", else "Unknown".
    QString personName(const QString &chatName, const QString &ownName, const QString &key) const;
    EngineLink *m_engine;
    Outbox *m_outbox;
    void ask(const QString &cmd, const QString &poster, const QString &id, QVariantMap args);
    // The requests in flight: their id -> the cmd, the poster and the story.
    QHash<QString, QStringList> m_pending;
    QList<QVariantMap> m_rows;
    bool m_active;
    int m_unseen;
    QString m_unseenNames;
    QTimer m_soon;
    bb::system::LocaleHandler m_region;
    QThreadPool m_pool;
    // The files being made, so a job is queued once.
    QSet<QString> m_making;
};

#endif
