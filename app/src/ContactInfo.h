#ifndef CONTACTINFO_H
#define CONTACTINFO_H

#include <QObject>
#include <QPointer>
#include <QString>
#include <QVariantMap>

class EngineLink;

// The contact info page's reply (step 5.8), made by QML in the page's attachedObjects: the
// request's owner dies with the page, so no connection to the engine outlives it.
class ContactInfo : public QObject
{
    Q_OBJECT
    // The EngineLink, set as ChatController's link is.
    Q_PROPERTY(QObject *link READ link WRITE setLink)
    // The last contact.info result {phone, about, groups}, each group with initials and color
    // added; empty until it comes.
    Q_PROPERTY(QVariantMap info READ info NOTIFY infoChanged)
public:
    explicit ContactInfo(QObject *parent = 0);

    QObject *link() const;
    void setLink(QObject *link);
    QVariantMap info() const;

    // Asks the engine about a person's chat (a chat list row's jid); nothing without an engine.
    Q_INVOKABLE void ask(const QString &jid);

signals:
    void infoChanged();

private slots:
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);

private:
    QPointer<EngineLink> m_engine;
    QVariantMap m_info;
    QString m_id; // the contact.info request we wait for
};

#endif
