#ifndef STICKER_H
#define STICKER_H

#include <QByteArray>
#include <QSet>
#include <QString>

class EngineLink;

// A sticker's picture (step 6.5b): neither Qt 4.8 nor the phone reads WebP, so libwebp decodes it here. A Lottie sticker's
// (a .was) is its frame 0, drawn by rlottie (libtgs.so); StickerPlayer plays the rest.
namespace Sticker {
// The side of a sticker's picture and of the frames its animation is drawn at, in px.
const int kStickerPx = 180;

// The PNG, at most kStickerPx square, of the first frame (the whole picture, for a still one) of the WebP or
// Lottie file at path. Made the first time and kept in data/cache/previews/<id>.png; "" when the file can't
// be read or decoded.
QByteArray picture(const QString &id, const QString &path);

// The picker's picture: the PNG, at most side square, of the first frame of the file at path, kept in
// data/cache/previews/st-<side>-<hex of hash>.png; its path, "" when the file can't be read or decoded. Touches only
// QImage, QFile, libwebp and rlottie, so a worker thread may call it.
QString gridPicture(const QString &hash, const QString &path, int side);

// Whether the file at path is a Lottie sticker: a ZIP, which starts with "PK\x03\x04", as a WebP never does. The UI looks at
// the file, not its name or mimetype: our own download of one is saved as <id>.webp.
bool isLottie(const QString &path);

// Whether the file at path moves: a Lottie, or a WebP whose VP8X header has the animation flag.
bool animated(const QString &path);

// The Lottie JSON inside the .was at path, NUL-terminated as QByteArray's own data is (rlottie takes a C string); empty
// when the file can't be read or unzipped, or the JSON is over 4 MB. Touches only QFile and zlib, so a worker thread may
// call it.
QByteArray lottieJson(const QString &path);

// Asks the engine, quietly (EngineLink::requestQuiet), for the file of a sticker still on the server, the
// first time only: asked holds the ids already asked for, and takes id when the request went out (not
// while the engine is down). The engine's msgs event then makes the chat re-read the row.
void fetch(EngineLink *engine, const QString &chat, const QString &id, QSet<QString> &asked);
}

#endif
