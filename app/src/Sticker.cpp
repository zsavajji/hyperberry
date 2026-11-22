#include "Sticker.h"

#include "EngineLink.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <rlottie_capi.h>
#include <string.h>
#include <webp/demux.h>
#include <zlib.h>

using Sticker::kStickerPx;

const int kJsonMax = 4 * 1024 * 1024; // the largest Lottie JSON (and .was) taken: a sticker's is ~100 KB

// The first frame of a still or animated WebP, as an image of its own (null on failure).
static QImage firstFrame(const QString &path)
{
    QFile f(path);
    if (!f.open(QIODevice::ReadOnly))
        return QImage();
    const QByteArray bytes = f.readAll(); // the decoder reads it in place until it is deleted
    WebPData data = { reinterpret_cast<const uint8_t *>(bytes.constData()), size_t(bytes.size()) };
    // Stickers are 512 px; the decoder allocates the whole canvas, 4 bytes a pixel, for whatever a
    // sender claims. WebPGetInfo reads the canvas of an animation too.
    int w = 0, h = 0;
    if (!WebPGetInfo(data.bytes, data.size, &w, &h) || w > 2048 || h > 2048)
        return QImage();
    WebPAnimDecoderOptions options;
    if (!WebPAnimDecoderOptionsInit(&options))
        return QImage();
    // B, G, R, then premultiplied alpha: QImage::Format_ARGB32_Premultiplied on a little-endian ARM.
    options.color_mode = MODE_bgrA;
    WebPAnimDecoder *decoder = WebPAnimDecoderNew(&data, &options);
    if (!decoder)
        return QImage();
    QImage image;
    WebPAnimInfo info;
    uint8_t *pixels = 0; // the decoder's, overwritten by the next frame and freed with it: copied
    int timestamp = 0;
    if (WebPAnimDecoderGetInfo(decoder, &info) && WebPAnimDecoderGetNext(decoder, &pixels, &timestamp)) {
        const int width = int(info.canvas_width), height = int(info.canvas_height);
        image = QImage(pixels, width, height, width * 4, QImage::Format_ARGB32_Premultiplied).copy();
    }
    WebPAnimDecoderDelete(decoder);
    return image;
}

bool Sticker::isLottie(const QString &path)
{
    QFile f(path);
    char magic[4];
    return f.open(QIODevice::ReadOnly) && f.read(magic, 4) == 4 && memcmp(magic, "PK\x03\x04", 4) == 0;
}

bool Sticker::animated(const QString &path)
{
    QFile f(path);
    char head[21];
    if (!f.open(QIODevice::ReadOnly) || f.read(head, 21) != 21)
        return false;
    // RIFF, the size, WEBP, then VP8X, its size, and the flags byte: 0x02 is the animation.
    return memcmp(head, "PK\x03\x04", 4) == 0
        || (memcmp(head, "RIFF", 4) == 0 && memcmp(head + 8, "WEBPVP8X", 8) == 0 && (head[20] & 0x02));
}

// The Lottie JSON in the ZIP that zip holds (empty on any fault). The ZIP is found by its end-of-central-directory record
// and the offsets counted from where it starts, so data in front of it does no harm. The entries have data descriptors
// (their local headers carry no sizes), so the sizes come from the central directory.
static QByteArray zipJson(const QByteArray &zip)
{
    const unsigned char *z = reinterpret_cast<const unsigned char *>(zip.constData());
    const int n = zip.size();
    auto u16 = [z](int i) -> unsigned { return z[i] | z[i + 1] << 8; };
    auto u32 = [&](int i) -> unsigned { return u16(i) | u16(i + 2) << 16; };
    int end = n - 22;
    while (end >= 0 && memcmp(z + end, "PK\x05\x06", 4) != 0)
        --end;
    if (end < 0)
        return QByteArray();
    const unsigned count = u16(end + 10), size = u32(end + 12), offset = u32(end + 16);
    const long long start = (long long)end - size - offset;
    long long at = start + offset;
    if (start < 0)
        return QByteArray();
    for (unsigned i = 0; i < count && at + 46 <= n && memcmp(z + at, "PK\x01\x02", 4) == 0; ++i) {
        const int nameLen = u16(at + 28);
        const long long next = at + 46 + nameLen + u16(at + 30) + u16(at + 32);
        if (next > n)
            break;
        const QByteArray name(zip.constData() + at + 46, nameLen);
        if (name.endsWith("animation.json") && !name.contains("secondary")) {
            const unsigned method = u16(at + 10), packed = u32(at + 20), unpacked = u32(at + 24);
            const long long local = start + u32(at + 42);
            if (local + 30 > n || memcmp(z + local, "PK\x03\x04", 4) != 0 || unpacked > unsigned(kJsonMax))
                return QByteArray();
            const long long data = local + 30 + u16(local + 26) + u16(local + 28);
            if (data + packed > n || (method != 0 && method != 8) || (method == 0 && packed != unpacked))
                return QByteArray();
            QByteArray json(int(unpacked), 0);
            if (method == 0) {
                memcpy(json.data(), z + data, unpacked);
                return json;
            }
            z_stream zs;
            memset(&zs, 0, sizeof zs);
            if (inflateInit2(&zs, -MAX_WBITS) != Z_OK) // raw deflate, no zlib header
                return QByteArray();
            zs.next_in = const_cast<Bytef *>(z + data);
            zs.avail_in = packed;
            zs.next_out = reinterpret_cast<Bytef *>(json.data());
            zs.avail_out = unpacked;
            const int ret = inflate(&zs, Z_FINISH);
            inflateEnd(&zs);
            return ret == Z_STREAM_END && zs.avail_out == 0 ? json : QByteArray();
        }
        at = next;
    }
    return QByteArray();
}

QByteArray Sticker::lottieJson(const QString &path)
{
    QFile f(path);
    if (!f.open(QIODevice::ReadOnly) || f.size() > kJsonMax)
        return QByteArray();
    return zipJson(f.readAll());
}

// Frame 0 of a Lottie sticker, drawn at side square (null on failure).
static QImage lottieFirstFrame(const QString &path, int side)
{
    const QByteArray json = Sticker::lottieJson(path);
    if (json.isEmpty())
        return QImage();
    Lottie_Animation *animation = lottie_animation_from_data(json.constData(), path.toUtf8().constData(), "");
    if (!animation)
        return QImage();
    // rlottie draws premultiplied ARGB as 0xAARRGGBB: B, G, R, A in memory on a little-endian ARM, which is what
    // Format_ARGB32_Premultiplied is.
    QImage image(side, side, QImage::Format_ARGB32_Premultiplied);
    image.fill(0);
    lottie_animation_render(animation, 0, reinterpret_cast<uint32_t *>(image.bits()), side, side, image.bytesPerLine());
    lottie_animation_destroy(animation);
    return image;
}

// The still picture of the file at path, at most side square: a Lottie's frame 0, else a WebP's first frame (null on failure).
static QImage still(const QString &path, int side)
{
    QImage image = Sticker::isLottie(path) ? lottieFirstFrame(path, side) : firstFrame(path);
    if (!image.isNull() && (image.width() > side || image.height() > side))
        image = image.scaled(side, side, Qt::KeepAspectRatio, Qt::SmoothTransformation);
    return image;
}

// The PNG of the still picture of path kept in file, made the first time; false when it can't be made.
static bool cached(const QString &file, const QString &path, int side)
{
    if (QFile::exists(file))
        return true;
    const QImage image = still(path, side);
    if (image.isNull() || !QDir().mkpath(QFileInfo(file).path()))
        return false;
    if (!image.save(file, "PNG")) {
        QFile::remove(file); // no half-written preview left to be taken for a good one
        return false;
    }
    return true;
}

// ponytail: made on the UI thread, as the photo previews are; a worker thread if scrolling stutters (a Lottie takes 20 to
// 60 ms to parse, once). A file that doesn't decode is tried again at every read of its row.
QByteArray Sticker::picture(const QString &id, const QString &path)
{
    const QString file = QDir::homePath() + "/cache/previews/" + id + ".png";
    if (!cached(file, path, kStickerPx))
        return QByteArray();
    QFile f(file);
    return f.open(QIODevice::ReadOnly) ? f.readAll() : QByteArray();
}

// The picker's cells and preview: the same, named by the sticker's hash (hex, as the engine names its copy of the file) and the
// side, so the 128 px of the grid and the 360 px of the preview don't share a file. Only QImage, QFile, libwebp and rlottie are
// touched: a worker thread calls it.
QString Sticker::gridPicture(const QString &hash, const QString &path, int side)
{
    const QString file = QDir::homePath() + "/cache/previews/st-" + QString::number(side) + "-"
                         + QString::fromLatin1(QByteArray::fromBase64(hash.toLatin1()).toHex()) + ".png";
    return cached(file, path, side) ? file : QString();
}

void Sticker::fetch(EngineLink *engine, const QString &chat, const QString &id, QSet<QString> &asked)
{
    if (!engine || asked.contains(id))
        return;
    QVariantMap args;
    args["chat"] = chat;
    args["id"] = id;
    if (!engine->requestQuiet("media.get", args).isEmpty())
        asked.insert(id);
}
