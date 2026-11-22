#ifndef STICKERPICKER_H
#define STICKERPICKER_H

#include <bb/cascades/DataModel>
#include <bb/cascades/Image>
#include <bb/cascades/ImageView>
#include <QHash>
#include <QList>
#include <QObject>
#include <QPointer>
#include <QSet>
#include <QString>
#include <QStringList>
#include <QVariantList>
#include <QVariantMap>

class EngineLink;
class StickerModel;

// The sticker page's data (StickerGrid; docs/protocol.md, "stickers.get"; docs/plan/stickers.md, S3): Favourites and Recent, the
// two groups of a grid, from one stickers.get each time a page opens. A picture is asked, downloaded and decoded only when the
// grid shows its cell, at most four at a time, the decoding on a worker thread (a Lottie takes 20 to 60 ms to parse). One for
// each page: what it holds goes with the page, the pictures stay as files in data/cache/previews.
class StickerPicker : public QObject
{
    Q_OBJECT
public:
    StickerPicker(EngineLink *engine, QObject *parent);

    // The grid: the groups as the top level (itemType "header", data the title), their stickers as children (itemType
    // "item", data {hash, image, hasImage, fave}). image is an empty Image until the picture is ready.
    bb::cascades::DataModel *model() const;
    // True from the page's ask until the engine answers; and whether no sticker is held.
    bool loading() const { return m_loading; }
    bool empty() const { return m_groups.isEmpty(); }

    // Asks for the lists: the page opened, or a favourite changed.
    void load();
    // Adds the sticker to Favourites or takes it out; the lists are asked again when the engine has done it.
    void fave(const QString &hash, bool on);
    // Shows the sticker in imageView at side px, which already has its grid picture: its still, from its file.
    void preview(QObject *imageView, const QString &hash, int side);
    void endPreview();

signals:
    // loading or empty changed.
    void stateChanged();

private slots:
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);
    void onFailed(const QString &id);
    void onDecoded();

private:
    friend class StickerModel;

    struct Group {
        QString title;
        QStringList hashes;
    };

    void setLists(const QVariantMap &lists);
    void addGroup(QList<Group> *groups, const QVariantList &stickers, const QString &title);
    void ask(const QString &hash);
    void pump();
    void decode(const QString &hash, const QString &path, int side);

    // For StickerModel.
    QVariant rowData(const QVariantList &indexPath);

    QPointer<EngineLink> m_engine;
    StickerModel *m_model;
    QList<Group> m_groups; // what the grid shows, never an empty one
    QSet<QString> m_faves;
    QHash<QString, QString> m_path; // hash -> the sticker's file, "" until stickers.file brings it
    bool m_loading;
    QString m_idGet;         // the stickers.get we wait for
    QSet<QString> m_faveIds; // sticker.fave requests out
    // The pictures: asked once each, queued, then downloaded when the file isn't here, then decoded.
    QHash<QString, bb::cascades::Image> m_images; // by hash
    QSet<QString> m_asked;
    QStringList m_queue;             // the last asked goes first: it is the one on screen
    QHash<QString, QString> m_files; // stickers.file requests out: request id -> hash
    int m_decoding;
    // The preview.
    QPointer<bb::cascades::ImageView> m_previewView;
    QString m_previewId; // the stickers.file we wait for
    QString m_previewHash;
    int m_previewSide;
    int m_previewTok; // a decode or an answer for an earlier preview is dropped
};

#endif
