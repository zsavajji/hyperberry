#ifndef STORIESMODEL_H
#define STORIESMODEL_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <bb/utility/i18n/RelativeDateFormatter>
#include <QHash>
#include <QList>
#include <QPair>
#include <QSqlDatabase>
#include <QSqlQuery>
#include <QTimer>
#include <QVariantMap>

class EngineLink;

// The Stories tab: one row per poster with a story under 24 h old (docs/protocol.md, `stories`), the
// ones with something unseen first, then by the newest story. A flat list of maps: poster, name,
// initials, color, photo (the chat's picture, "" when none), count, unseen, time, and the tile: kind
// (0 text, 1 photo, 2 video), tile (a small JPEG's path, "" when none), bg and fg (a text story's colours)
// and text. The tile is the first story not yet seen, else the newest. Read-only on chats.db, like
// CallsModel; the list reloads only while `active` (the tab is shown), but `unseen`, all posters'
// unseen stories (the tab's badge), is kept up to date always. The viewer (StoryViewer.qml) reads one
// poster's stories through storiesOf and asks the engine for a file through fetch.
class StoriesModel : public bb::cascades::DataModel
{
    Q_OBJECT
    Q_PROPERTY(bool active READ active WRITE setActive NOTIFY activeChanged)
    Q_PROPERTY(bool empty READ empty NOTIFY emptyChanged)
    Q_PROPERTY(int unseen READ unseen NOTIFY unseenChanged)
public:
    explicit StoriesModel(EngineLink *engine, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);
    bool active() const { return m_active; }
    void setActive(bool on);
    bool empty() const { return m_rows.isEmpty(); }
    int unseen() const { return m_unseen; }

    // The poster's stories under 24 h old, oldest first, as storyOf's maps.
    Q_INVOKABLE QVariantList storiesOf(const QString &poster);
    // One story: id, poster, kind (0 text, 1 photo, 2 video), text, bg and fg (a text story's colours),
    // seen, time ("Today, 14:03"), duration (seconds, 0 if unknown), width, height, pic (the picture
    // to show: a photo sized for the screen, else the inline thumbnail, "" for a text story), video (the
    // file's path, "" until it is here) and ready (everything it shows is here). {} for none.
    Q_INVOKABLE QVariantMap storyOf(const QString &poster, const QString &id);
    // Asks the engine for the story's file (story.get); storyReady or storyFailed answers.
    Q_INVOKABLE void fetch(const QString &poster, const QString &id);
    // The viewer showed it: the engine marks it seen and sends the read receipt (story.seen).
    Q_INVOKABLE void markSeen(const QString &poster, const QString &id);

signals:
    void storyReady(const QString &poster, const QString &id);
    void storyFailed(const QString &poster, const QString &id);
    void activeChanged();
    void emptyChanged();
    void unseenChanged();

private slots:
    void reloadSoon();
    void reload();
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);
    void onFailure();

private:
    QVariantMap storyMap(QSqlQuery &q, QSqlDatabase &db);
    EngineLink *m_engine;
    QHash<QString, QPair<QString, QString> > m_pending; // story.get's request id -> poster and story
    QList<QVariantMap> m_rows;
    bool m_active;
    int m_unseen;
    QTimer m_soon;
    bb::utility::i18n::RelativeDateFormatter m_days;
    bb::system::LocaleHandler m_region;
};

#endif
