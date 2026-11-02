#ifndef MESSAGEKINDS_H
#define MESSAGEKINDS_H

#include <QLocale>
#include <QString>
#include <QVariantMap>

// The wording of the kinds that are not media (step 6.5), kept out of MessageModel.cpp to keep that
// file short. docs/protocol.md has the kinds and what `extra` holds for each.
namespace MessageKinds {
// Words a message of messages.kind `kind`. text, extra (the JSON, "" when NULL), out and sender (the
// sender's name, "" when none) are the row's; time is the message's time as the model words it, the
// locale the phone's. Sets label (the card's grey line) and shown (the text under it) for kinds 7,
// 9 to 12 and 14, and leaves them alone for the others (shown starts as text). A location (9) also
// puts lat, lon and place (its name) in data, for MapChooser (step 6.5c). A contact (10) has no label
// and an empty shown (MediaBubble draws it): data gets vcard (every card's vCard joined by "\r\n", for
// MediaOpener::importContact) and contactInfo (the first number, "N contacts" for several; step 6.5d).
// Returns true for a service line (kinds 6 and 13): then shown is the whole line and label is not used.
bool fill(int kind, const QString &text, const QString &extra, bool out, const QString &sender,
          const QString &time, const QLocale &locale, QVariantMap &data, QString &label,
          QString &shown);
}

#endif
