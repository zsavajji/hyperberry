#ifndef MESSAGEKINDS_H
#define MESSAGEKINDS_H

#include <QLocale>
#include <QString>

// The wording of the kinds that are not media (step 6.5), kept out of MessageModel.cpp to keep that
// file short. docs/protocol.md has the kinds and what `extra` holds for each.
namespace MessageKinds {
// Words a message of messages.kind `kind`. text, extra (the JSON, "" when NULL), out and sender (the
// sender's name, "" when none) are the row's; time is the message's time as the model words it, the
// locale the phone's. Sets label (the card's grey line) and shown (the text under it) for kinds 5,
// 7, 9 to 12 and 14, and leaves them alone for the others (shown starts as text). Returns true for a
// service line (kinds 6 and 13): then shown is the whole line and label is not used.
bool fill(int kind, const QString &text, const QString &extra, bool out, const QString &sender,
          const QString &time, const QLocale &locale, QString &label, QString &shown);
}

#endif
