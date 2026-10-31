#ifndef CHATLISTMODEL_H
#define CHATLISTMODEL_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <bb/utility/i18n/RelativeDateFormatter>
#include <QList>
#include <QString>
#include <QTimer>
#include <QVariantMap>

class EngineLink;

// The chat list (PLAN.md §7, step 2.4): chats.db's chats that aren't archived (or, for the Archived page, only those), pinned first,
// then newest first. Headers ("Pinned", "Today", "Yesterday", "Monday", "Jan 20") are the top
// level ([g]), and the chats are their children ([g, i]): a map of jid, kind, name, initials,
// color, preview, time, out, state, unread, muted and pinned.
class ChatListModel : public bb::cascades::DataModel
{
    Q_OBJECT
    // True when there's no chat to show.
    Q_PROPERTY(bool empty READ empty NOTIFY emptyChanged)
    // The Active Frame's two facts (step 5.2): unread messages in unmuted chats, and the newest
    // incoming "Name: preview". Only recomputed while the app isn't fullscreen.
    Q_PROPERTY(int unreadTotal READ unreadTotal NOTIFY frameChanged)
    Q_PROPERTY(QString lastLine READ lastLine NOTIFY frameChanged)
public:
    // archived: only the archived chats, and no frame work (the Archived page).
    explicit ChatListModel(EngineLink *engine, bool archived = false, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);
    bool empty() const;
    int unreadTotal() const { return m_unreadTotal; }
    QString lastLine() const { return m_lastLine; }
    // The chat's map as the rows have it, archived chats included; empty when there is none.
    Q_INVOKABLE QVariantMap chat(const QString &jid);

signals:
    void emptyChanged();
    void frameChanged();

private slots:
    void reloadSoon();
    void reload();
    void updateFrame();

private:
    struct Group
    {
        QString title;
        QList<QVariantMap> rows;
    };
    EngineLink *m_engine;
    bool m_archived;
    QString m_dbName;
    QList<Group> m_groups;
    int m_unreadTotal;
    QString m_lastLine;
    QTimer m_soon;
    bb::utility::i18n::RelativeDateFormatter m_days;
    bb::system::LocaleHandler m_region;
};

#endif
