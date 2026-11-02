#include "MediaData.h"
#include "Sticker.h"
#include "sizes.h"

#include <bb/cascades/Image>
#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <QImageReader>

// "m:ss"
QString MediaData::clock(int seconds)
{
    return QString("%1:%2").arg(seconds / 60).arg(seconds % 60, 2, 10, QChar('0'));
}

// The bubble's picture for a downloaded photo (step 6.3): a JPEG of at most 320 px, made the first
// time from the file and kept in data/cache/previews/ (the engine wipes data/cache/ on unlink). The
// engine decodes no picture, and Qt's reader decodes straight to the smaller size. Fills width and
// height from the file when the row has none. Empty when the photo can't be read.
// ponytail: made on the UI thread, tens of ms on the Q10; a worker thread if scrolling stutters.
static QByteArray preview(const QString &id, const QString &path, int &width, int &height)
{
    const QString file = QDir::homePath() + "/cache/previews/" + id + ".jpg";
    const bool have = QFile::exists(file);
    if (!have || width <= 0 || height <= 0) {
        QImageReader reader(path);
        const QSize size = reader.size(); // from the header
        if (width <= 0 || height <= 0) {
            width = size.width();
            height = size.height();
        }
        if (!have) {
            if (!size.isValid())
                return QByteArray();
            QSize to = size;
            if (to.width() > 320 || to.height() > 320)
                to.scale(320, 320, Qt::KeepAspectRatio);
            reader.setScaledSize(to);
            const QImage image = reader.read();
            if (image.isNull() || !QDir().mkpath(QFileInfo(file).path()))
                return QByteArray();
            if (!image.save(file, "JPEG", 80)) {
                QFile::remove(file); // no half-written preview left to be taken for a good one
                return QByteArray();
            }
        }
    }
    QFile f(file);
    return f.open(QIODevice::ReadOnly) ? f.readAll() : QByteArray();
}

bool MediaData::fill(QVariantMap &data, const QString &id, int kind, const QString &text,
                     const QByteArray &thumb, int width, int height, qint64 size, int duration,
                     int mediaState, const QString &path, const QLocale &locale)
{
    QString media;
    switch (kind) {
    case 1: media = "photo"; break;
    case 2: media = "video"; break;
    case 4: media = "doc"; break;
    case 5: media = "sticker"; break;
    case 8: media = "audio"; break;
    }
    data["media"] = media;
    if (media.isEmpty())
        return false;
    // The inline thumbnail stands in when there is no readable photo or sticker.
    QByteArray shown = thumb;
    if ((kind == 1 || kind == 5) && mediaState == 2 && !path.isEmpty()) {
        const QByteArray p = kind == 1 ? preview(id, path, width, height) : Sticker::picture(id, path);
        if (!p.isEmpty())
            shown = p;
    }
    // Always an Image, an empty one without a thumbnail: a recycled card assigned undefined would
    // keep the picture of the row it showed before.
    data["thumb"] = QVariant::fromValue(shown.isEmpty() ? bb::cascades::Image()
                                                        : bb::cascades::Image(shown));
    data["hasThumb"] = !shown.isEmpty();
    data["ratio"] = width > 0 && height > 0 ? double(height) / width : 0.75;
    data["mediaState"] = mediaState;
    data["path"] = path;
    data["sizeText"] = sizeText(size, locale);
    data["durationText"] = (kind == 2 || kind == 8) && duration > 0 ? clock(duration) : QString();
    data["fileName"] = kind == 4 ? text : QString();
    // What a reply and the long-press menu show for a message whose text is empty.
    static const char *const names[] = { "", QT_TRANSLATE_NOOP("MediaData", "Photo"),
        QT_TRANSLATE_NOOP("MediaData", "Video"), "", QT_TRANSLATE_NOOP("MediaData", "Document"),
        QT_TRANSLATE_NOOP("MediaData", "Sticker"), "", "", QT_TRANSLATE_NOOP("MediaData", "Audio file") };
    data["summary"] = kind == 4 && !text.isEmpty() ? text
                                                    : QCoreApplication::translate("MediaData", names[kind]);
    return true;
}
