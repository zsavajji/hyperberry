#ifndef CONTACTPICKER_H
#define CONTACTPICKER_H

#include <bb/cascades/ArrayDataModel>
#include <bb/cascades/DataModel>
#include <QObject>
#include <QPointer>
#include <QString>
#include <QVariant>
#include <QVariantList>

class EngineLink;

// The new chat page's address book (PLAN.md §7, step 3.6), made by QML in the page's
// attachedObjects so it dies with the page. It asks contacts.list, draws each contact as a
// chat-list row (the tile the chat will have) and filters them as the user types.
class ContactPicker : public QObject
{
    Q_OBJECT
    // The EngineLink; named link, not engine, for the reason in ChatController.h.
    Q_PROPERTY(QObject *link READ link WRITE setLink)
    // The rows shown: jid, kind 0, name, initials, color.
    Q_PROPERTY(bb::cascades::DataModel *model READ model CONSTANT)
    Q_PROPERTY(bool loaded READ loaded NOTIFY loadedChanged)
    // How many contacts there are in all, and how many the filter shows (the model's size()
    // isn't re-read by a binding).
    Q_PROPERTY(int total READ total NOTIFY loadedChanged)
    Q_PROPERTY(int shown READ shown NOTIFY shownChanged)
public:
    explicit ContactPicker(QObject *parent = 0);

    QObject *link() const;
    void setLink(QObject *link);
    bb::cascades::DataModel *model() const;
    bool loaded() const;
    int total() const;
    int shown() const;

    Q_INVOKABLE void load();
    // Shows the contacts with a word of the name starting with text, ignoring case; "" shows all.
    Q_INVOKABLE void filter(const QString &text);

signals:
    void loadedChanged();
    void shownChanged();

private slots:
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);
    void onConnected();

private:
    void ask();

    QPointer<EngineLink> m_engine;
    bb::cascades::ArrayDataModel *m_model;
    QVariantList m_all;
    QString m_text;
    QString m_askId; // the contacts.list request we wait for
    bool m_loaded;
    bool m_wanted; // load() was called
};

#endif
