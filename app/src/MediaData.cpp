#include "MediaData.h"

#include <bb/cascades/Image>
#include <QCoreApplication>

// "m:ss"
static QString clock(int seconds)
{
    return QString("%1:%2").arg(seconds / 60).arg(seconds % 60, 2, 10, QChar('0'));
}

// Whole KB under 1 MB (at least 1), else MB with one decimal in the phone's region; "" when the
// size is unknown.
static QString sizeText(qint64 bytes, const QLocale &locale)
{
    if (bytes <= 0)
        return QString();
    if (bytes < 1024 * 1024)
        return QString("%1 KB").arg(qMax<qint64>(1, bytes / 1024));
    return QString("%1 MB").arg(locale.toString(bytes / (1024.0 * 1024.0), 'f', 1));
}

bool MediaData::fill(QVariantMap &data, int kind, const QString &text, const QByteArray &thumb,
                     int width, int height, qint64 size, int duration, int mediaState,
                     const QString &path, const QLocale &locale)
{
    QString media;
    switch (kind) {
    case 1: media = "photo"; break;
    case 2: media = "video"; break;
    case 4: media = "doc"; break;
    case 8: media = "audio"; break;
    }
    data["media"] = media;
    if (media.isEmpty())
        return false;
    // Always an Image, an empty one without a thumbnail: a recycled card assigned undefined would
    // keep the picture of the row it showed before.
    data["thumb"] = QVariant::fromValue(thumb.isEmpty() ? bb::cascades::Image()
                                                        : bb::cascades::Image(thumb));
    data["hasThumb"] = !thumb.isEmpty();
    data["ratio"] = width > 0 && height > 0 ? double(height) / width : 0.75;
    data["mediaState"] = mediaState;
    data["path"] = path;
    data["sizeText"] = sizeText(size, locale);
    data["durationText"] = (kind == 2 || kind == 8) && duration > 0 ? clock(duration) : QString();
    data["fileName"] = kind == 4 ? text : QString();
    // What a reply and the long-press menu show for a message whose text is empty.
    static const char *const names[] = { "", QT_TRANSLATE_NOOP("MediaData", "Photo"),
        QT_TRANSLATE_NOOP("MediaData", "Video"), "", QT_TRANSLATE_NOOP("MediaData", "Document"), "", "", "",
        QT_TRANSLATE_NOOP("MediaData", "Audio file") };
    data["summary"] = kind == 4 && !text.isEmpty() ? text
                                                    : QCoreApplication::translate("MediaData", names[kind]);
    return true;
}
