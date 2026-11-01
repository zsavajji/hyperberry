#ifndef MEDIADATA_H
#define MEDIADATA_H

#include <QByteArray>
#include <QLocale>
#include <QString>
#include <QVariantMap>

// What a photo, video, audio file or document row hands QML on top of the common message fields
// (step 6.1), kept out of MessageModel.cpp to keep that file short.
namespace MediaData {
// Sets data's media keys from the row's columns; id is the message's, kind is messages.kind, 0 for a
// deleted row. A downloaded photo's thumb is its 320 px preview, made here from the file (step 6.3).
// media: "photo", "video", "doc" or "audio", "" for any other kind (always set, so QML never reads
// undefined); the rest (thumb, hasThumb, ratio, mediaState, path, sizeText, durationText,
// fileName) only for media. Returns whether it is media.
bool fill(QVariantMap &data, const QString &id, int kind, const QString &text,
          const QByteArray &thumb, int width, int height, qint64 size, int duration,
          int mediaState, const QString &path, const QLocale &locale);
}

#endif
