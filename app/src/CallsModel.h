#ifndef CALLSMODEL_H
#define CALLSMODEL_H

#include <bb/cascades/DataModel>
#include <bb/system/LocaleHandler>
#include <bb/utility/i18n/RelativeDateFormatter>
#include <QList>
#include <QSettings>
#include <QTimer>
#include <QVariantMap>

class EngineLink;

// The Calls tab (step 7.4): the call rows (kind 13) of every chat, newest first, 200 at most. A flat
// list of maps: jid, name, initials, color, photo, missed, detail ("Missed voice call", "Outgoing, 3:12")
// and time. Read-only on chats.db, like ChatListModel; it reloads only while `active` (the tab is shown).
// The Active Frame also reads from it what the tab has not shown yet: missedNew, our incoming missed calls since
// the tab was last shown ("callsSeen" in settings.ini), and missedLast, the newest one's "Name · time". That is
// counted whether the tab is shown or not.
class CallsModel : public bb::cascades::DataModel
{
    Q_OBJECT
    Q_PROPERTY(bool active READ active WRITE setActive NOTIFY activeChanged)
    Q_PROPERTY(bool empty READ empty NOTIFY emptyChanged)
    Q_PROPERTY(int missedNew READ missedNew NOTIFY missedChanged)
    Q_PROPERTY(QString missedLast READ missedLast NOTIFY missedChanged)
public:
    explicit CallsModel(EngineLink *engine, QObject *parent = 0);

    int childCount(const QVariantList &indexPath);
    bool hasChildren(const QVariantList &indexPath);
    QString itemType(const QVariantList &indexPath);
    QVariant data(const QVariantList &indexPath);
    bool active() const { return m_active; }
    void setActive(bool on);
    bool empty() const { return m_rows.isEmpty(); }
    int missedNew() const { return m_missedNew; }
    QString missedLast() const { return m_missedLast; }

signals:
    void activeChanged();
    void emptyChanged();
    void missedChanged();

private slots:
    void reloadSoon();
    void reload();
    void countSoon();
    void count();

private:
    void markSeen();

    QList<QVariantMap> m_rows;
    bool m_active;
    QTimer m_soon;
    QTimer m_countSoon;
    QSettings m_file; // settings.ini, as Settings opens it
    int m_missedNew;
    QString m_missedLast;
    bb::system::LocaleHandler m_region;
};

#endif
