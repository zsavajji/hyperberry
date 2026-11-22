#ifndef STICKERGRID_H
#define STICKERGRID_H

#include <bb/cascades/CustomControl>
#include <bb/cascades/ListItemListener>
#include <bb/cascades/ListItemProvider>
#include <QPointer>
#include <QString>
#include <QVariantList>
#include <QVariantMap>

namespace bb {
namespace cascades {
class Button;
class Container;
class ImageView;
class Label;
class ListView;
}
}

class StickerGrid;
class StickerPicker;

// The sticker page's content (StickersPage.qml, docs/plan/stickers.md, S3), in C++ as the chat's cards are, ported from
// Tengram's: a grid of four columns, Favourites then Recent (StickerPicker holds the data), the empty note, and the preview over
// everything. A tap on a sticker sends it (picked) and a long press shows it large over a dimmed page, still, with Send and Add
// to or Remove from favourites. It fills what it is in.
class StickerGrid : public bb::cascades::CustomControl
{
    Q_OBJECT
    // The EngineLink, set as ContactPicker's link is. Setting it asks for the lists.
    Q_PROPERTY(QObject *link READ link WRITE setLink)
    // A pressed cell's colour, main.qml's nav.pressColor ("#RRGGBB"): what a QML list item read through its view.
    Q_PROPERTY(QString pressColor READ pressColor WRITE setPressColor)
public:
    explicit StickerGrid(bb::cascades::Container *parent = 0);

    QObject *link() const { return m_link; }
    void setLink(QObject *link);
    QString pressColor() const { return m_pressColor; }
    void setPressColor(const QString &color) { m_pressColor = color; }

    // A cell's long press: the sticker's row, {hash, image, hasImage, fave}.
    void openPreview(const QVariantMap &sticker);

signals:
    // A tap on a sticker, or Send in the preview: its hash, which sticker.send takes.
    void picked(const QString &hash);

private slots:
    void syncState();
    void onTriggered(const QVariantList &path);
    void closePreview();
    void onSend();
    void onFave();

private:
    QPointer<QObject> m_link;
    StickerPicker *m_picker;
    QString m_pressColor;
    bb::cascades::ListView *m_list;
    bb::cascades::Label *m_empty;
    bb::cascades::Container *m_overlay;
    bb::cascades::ImageView *m_previewView;
    bb::cascades::Button *m_faveButton;
    QVariantMap m_preview; // the sticker shown
    bool m_previewing;
};

// The grid's cells, as MessageCards makes the chat's: a Header for a group's title, a StickerCell for a sticker.
class StickerCells : public bb::cascades::ListItemProvider
{
    Q_OBJECT
public:
    explicit StickerCells(StickerGrid *grid);

    bb::cascades::VisualNode *createItem(bb::cascades::ListView *list, const QString &type);
    void updateItem(bb::cascades::ListView *list, bb::cascades::VisualNode *listItem, const QString &type,
                    const QVariantList &indexPath, const QVariant &data);

private:
    StickerGrid *m_grid; // the list is the grid's, so the grid outlives the cells
};

// One sticker's cell: its grid picture 90 % of the cell. A press tints it; a long press opens the grid's preview.
class StickerCell : public bb::cascades::CustomControl, public bb::cascades::ListItemListener
{
    Q_OBJECT
public:
    explicit StickerCell(StickerGrid *grid);

    // sticker is a row of StickerPicker's model.
    void show(const QVariantMap &sticker);

    // ListItemListener: a press tints the cell; selection isn't used.
    void select(bool selected);
    void activate(bool activated);
    void reset(bool selected, bool activated);

private slots:
    void onLongPress();

private:
    StickerGrid *m_grid;
    QVariantMap m_sticker;
    bb::cascades::Container *m_root;
    bb::cascades::ImageView *m_image;
};

#endif
