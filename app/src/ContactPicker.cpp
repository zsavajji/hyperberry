#include "ContactPicker.h"

#include "EngineLink.h"
#include "colors.h"

#include <QStringList>
#include <QVariantMap>

ContactPicker::ContactPicker(QObject *parent)
    : QObject(parent)
    , m_model(new bb::cascades::ArrayDataModel(this))
    , m_loaded(false)
    , m_wanted(false)
{
}

QObject *ContactPicker::link() const
{
    return m_engine;
}

void ContactPicker::setLink(QObject *link)
{
    m_engine = qobject_cast<EngineLink *>(link);
    if (!m_engine)
        return;
    connect(m_engine, SIGNAL(replied(QString, QString, QVariant)), this,
            SLOT(onReplied(QString, QString, QVariant)));
    connect(m_engine, SIGNAL(connectedChanged()), this, SLOT(onConnected()));
}

bb::cascades::DataModel *ContactPicker::model() const
{
    return m_model;
}

bool ContactPicker::loaded() const
{
    return m_loaded;
}

int ContactPicker::total() const
{
    return m_all.size();
}

int ContactPicker::shown() const
{
    return m_model->size();
}

void ContactPicker::load()
{
    m_wanted = true;
    ask();
}

void ContactPicker::ask()
{
    if (m_loaded || !m_engine || !m_engine->connected())
        return;
    m_askId = m_engine->request("contacts.list", QVariantMap());
}

// Asks again on a reconnect if the first ask never came back.
void ContactPicker::onConnected()
{
    if (m_wanted)
        ask();
}

// Adds what the page draws to each contact: the tile's initials and the colour the chat's tile
// has in the chat list.
void ContactPicker::onReplied(const QString &id, const QString &cmd, const QVariant &result)
{
    if (cmd != "contacts.list" || id != m_askId)
        return;
    m_all.clear();
    foreach (const QVariant &v, result.toList()) {
        QVariantMap c = v.toMap();
        c["kind"] = 0;
        c["initials"] = initials(c["name"].toString());
        c["color"] = pickColor(c["jid"].toString(), kColors, kColorCount);
        m_all << c;
    }
    m_loaded = true;
    filter(m_text);
    emit loadedChanged();
}

// ponytail: a linear pass per keystroke; fine for a few thousand contacts.
void ContactPicker::filter(const QString &text)
{
    m_text = text;
    const QString t = text.trimmed();
    m_model->clear();
    foreach (const QVariant &v, m_all) {
        bool match = t.isEmpty();
        if (!match) {
            foreach (const QString &w, v.toMap()["name"].toString().split(' ', QString::SkipEmptyParts)) {
                if (w.startsWith(t, Qt::CaseInsensitive)) {
                    match = true;
                    break;
                }
            }
        }
        if (match)
            m_model->append(v);
    }
    emit shownChanged();
}
