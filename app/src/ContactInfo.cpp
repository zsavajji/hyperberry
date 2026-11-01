#include "ContactInfo.h"

#include "EngineLink.h"
#include "colors.h"

ContactInfo::ContactInfo(QObject *parent)
    : QObject(parent)
{
}

QObject *ContactInfo::link() const
{
    return m_engine;
}

// A QPointer, as ChatController's: at app exit the page may outlive the EngineLink.
void ContactInfo::setLink(QObject *link)
{
    m_engine = qobject_cast<EngineLink *>(link);
    if (m_engine)
        connect(m_engine, SIGNAL(replied(QString, QString, QVariant)), this,
                SLOT(onReplied(QString, QString, QVariant)));
}

QVariantMap ContactInfo::info() const
{
    return m_info;
}

void ContactInfo::ask(const QString &jid)
{
    if (!m_engine)
        return;
    QVariantMap args;
    args["chat"] = jid;
    m_id = m_engine->request("contact.info", args);
}

// Gives each group the tile the chat list gives it: initials and the jid's colour.
void ContactInfo::onReplied(const QString &id, const QString &cmd, const QVariant &result)
{
    if (cmd != "contact.info" || id != m_id)
        return;
    QVariantMap info = result.toMap();
    QVariantList groups;
    foreach (const QVariant &v, info["groups"].toList()) {
        QVariantMap g = v.toMap();
        if (g["name"].toString().isEmpty())
            g["name"] = tr("Unnamed group");
        g["initials"] = initials(g["name"].toString());
        g["color"] = pickColor(g["jid"].toString(), kColors, kColorCount);
        groups << g;
    }
    info["groups"] = groups;
    m_info = info;
    emit infoChanged();
}
