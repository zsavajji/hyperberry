#ifndef MESSAGEMODEL_H
#define MESSAGEMODEL_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <bb/utility/i18n/RelativeDateFormatter>
#include <QList>
#include <QPointer>
#include <QSet>
#include <QSqlQuery>
#include <QString>
#include <QStringList>
#include <QVariantMap>

class EngineLink;

// One chat's messages (PLAN.md §7, step 3.1), read-only from chats.db. The model is flat
// ([i]) and newest first: the newest day's messages newest to oldest, then that day's header,
// then the next older day, and so on. The list is bottom to top, so index 0 is at the bottom,
// it opens at the newest message with no scroll call, each header shows above its day, and an
// older page is appended at the model's end (the visual top) without moving the rows on
// screen. itemType: "header" (data: the title), "msg", or "service" for kind 6. A "msg" is a
// map of id, out, state, edited, deleted, revocable, time, label, text, quote, sender and senderColor,
// and media ("photo", "video", "doc", "audio" or "") with the keys MediaData.h lists.
// The newest 50 load at once, older pages as the list nears its top; the loaded set always
// holds whole days, so a page never lands inside a loaded day.
class MessageModel : public bb::cascades::DataModel
{
    Q_OBJECT
public:
    MessageModel(EngineLink *engine, const QString &chat, bool group, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);

    // Loads the next older page when the first visible entry, an index path, is within 25
    // entries of the visual top (in a bottom-to-top list that is the model's end).
    Q_INVOKABLE void nearTop(const QVariantList &firstVisible);

    // The chat's unread count as chats.db has it now.
    int unread() const;

    // Messages the page deleted for me and may still take back: they leave the list at once,
    // before the engine's msgs event, and unhide brings them back.
    void hide(const QStringList &ids);
    void unhide(const QStringList &ids);

signals:
    // A merge added a message that isn't ours and wasn't there before (not a first load or an
    // older page): the controller decides whether it was seen.
    void incoming();

private slots:
    void onMsgs(const QString &chat, const QStringList &ids);
    void onConnected();

private:
    struct Msg
    {
        qint64 ts;
        qint64 rowid;
        QString id;
        bool service;
        QVariantMap data;
    };
    struct Entry
    {
        QString key;   // the message id, or "day:" and the julian day
        QString title; // a header's
        int msg;       // index into m_all; -1 for a header
    };

    static bool lessThan(const Msg &a, const Msg &b);
    Msg read(QSqlQuery &q);
    QList<Msg> select(const QString &where, const QVariantList &args, int limit);
    void loadPage();
    void merge(const QStringList &asked, const QList<Msg> &fresh, bool older);
    void rebuild();
    QStringList keys() const;
    void refresh(const QStringList &old, const QSet<QString> &updated);

    QPointer<EngineLink> m_engine;
    QString m_chat;
    bool m_group;
    bool m_atStart; // the oldest message is loaded
    QList<Msg> m_all;
    QSet<QString> m_hidden;
    QList<Entry> m_entries;
    bb::utility::i18n::RelativeDateFormatter m_days;
    bb::system::LocaleHandler m_region;
};

#endif
