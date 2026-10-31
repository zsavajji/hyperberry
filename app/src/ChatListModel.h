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

// The chat list (PLAN.md §7, step 2.4): chats.db's chats that aren't archived, pinned first,
// then newest first. Headers ("Pinned", "Today", "Yesterday", "Monday", "Jan 20") are the top
// level ([g]), and the chats are their children ([g, i]): a map of jid, kind, name, initials,
// color, preview, time, out, state, unread, muted and pinned.
class ChatListModel : public bb::cascades::DataModel
{
    Q_OBJECT
    // True when there's no chat to show.
    Q_PROPERTY(bool empty READ empty NOTIFY emptyChanged)
public:
    explicit ChatListModel(EngineLink *engine, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);
    bool empty() const;

signals:
    void emptyChanged();

private slots:
    void reloadSoon();
    void reload();

private:
    struct Group
    {
        QString title;
        QList<QVariantMap> rows;
    };
    QList<Group> m_groups;
    QTimer m_soon;
    bb::utility::i18n::RelativeDateFormatter m_days;
    bb::system::LocaleHandler m_region;
};

#endif
