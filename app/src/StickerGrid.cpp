#include "StickerGrid.h"

#include <bb/cascades/Button>
#include <bb/cascades/Container>
#include <bb/cascades/DataModel>
#include <bb/cascades/DockLayout>
#include <bb/cascades/GridListLayout>
#include <bb/cascades/Header>
#include <bb/cascades/HorizontalAlignment>
#include <bb/cascades/Image>
#include <bb/cascades/ImageView>
#include <bb/cascades/ImageViewLoadEffect>
#include <bb/cascades/Label>
#include <bb/cascades/ListHeaderMode>
#include <bb/cascades/ListView>
#include <bb/cascades/LongPressHandler>
#include <bb/cascades/ScalingMethod>
#include <bb/cascades/TapHandler>
#include <bb/cascades/TextAlign>
#include <bb/cascades/TextStyleDefinition>
#include <bb/cascades/UIConfig>
#include <bb/cascades/UIPalette>
#include <bb/cascades/VerticalAlignment>

#include "CardStyle.h"
#include "EngineLink.h"
#include "StickerPicker.h"

using namespace bb::cascades;

// The strings are StickersPage.qml's (hyperberry_it.ts and the rest): translate() takes the context its qsTr had.

StickerGrid::StickerGrid(Container *parent)
    : CustomControl(parent)
    , m_picker(0)
    , m_previewing(false)
{
    UIConfig *ui = this->ui();
    setHorizontalAlignment(HorizontalAlignment::Fill);
    setVerticalAlignment(VerticalAlignment::Fill);

    Container *root = new Container();
    root->setLayout(new DockLayout());
    root->setHorizontalAlignment(HorizontalAlignment::Fill);
    root->setVerticalAlignment(VerticalAlignment::Fill);

    // headerMode must be set: a grid's default (None) shows only the groups, as cells, and never their items (EmojiPicker.qml).
    m_list = new ListView();
    GridListLayout *cells = new GridListLayout();
    cells->setColumnCount(4);
    cells->setCellAspectRatio(1.0f);
    cells->setHeaderMode(ListHeaderMode::Standard);
    m_list->setLayout(cells);
    m_list->setListItemProvider(new StickerCells(this)); // the list owns it
    connect(m_list, SIGNAL(triggered(QVariantList)), this, SLOT(onTriggered(QVariantList)));

    m_empty = new Label();
    m_empty->setHorizontalAlignment(HorizontalAlignment::Center);
    m_empty->setVerticalAlignment(VerticalAlignment::Center);
    m_empty->setLeftMargin(Card::du(ui, 4));
    m_empty->setRightMargin(Card::du(ui, 4));
    m_empty->setMultiline(true);
    m_empty->textStyle()->setTextAlign(TextAlign::Center);
    m_empty->textStyle()->setColor(ui->palette()->secondaryTextOnPlain());
    m_empty->setText(QCoreApplication::translate(
        "StickersPage", "No stickers yet. The stickers you send and your favourites show here."));
    m_empty->setVisible(false);

    // The preview: over the page, dimmed. A tap on the dim closes it; the dim is a sibling of the column of buttons, so their
    // taps never reach it.
    m_overlay = new Container();
    m_overlay->setLayout(new DockLayout());
    m_overlay->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_overlay->setVerticalAlignment(VerticalAlignment::Fill);
    m_overlay->setVisible(false);
    Container *dim = new Container();
    dim->setHorizontalAlignment(HorizontalAlignment::Fill);
    dim->setVerticalAlignment(VerticalAlignment::Fill);
    dim->setBackground(Color::fromARGB(0xB3000000));
    TapHandler *tap = new TapHandler(dim);
    dim->addGestureHandler(tap);
    connect(tap, SIGNAL(tapped(bb::cascades::TapEvent *)), this, SLOT(closePreview()));

    Container *box = new Container();
    box->setHorizontalAlignment(HorizontalAlignment::Fill);
    box->setVerticalAlignment(VerticalAlignment::Center);
    box->setLeftPadding(Card::du(ui, 2));
    box->setRightPadding(Card::du(ui, 2));
    m_previewView = new ImageView();
    m_previewView->setHorizontalAlignment(HorizontalAlignment::Center);
    m_previewView->setPreferredSize(Card::du(ui, 40), Card::du(ui, 40)); // 360 px on the Q10
    m_previewView->setScalingMethod(ScalingMethod::AspectFit);
    m_previewView->setLoadEffect(ImageViewLoadEffect::None);
    Container *buttons = new Container();
    buttons->setHorizontalAlignment(HorizontalAlignment::Fill);
    buttons->setTopMargin(Card::du(ui, 3));
    buttons->setLayout(Card::across());
    Button *send = new Button();
    Card::quota(send, 1);
    send->setText(QCoreApplication::translate("StickersPage", "Send"));
    connect(send, SIGNAL(clicked()), this, SLOT(onSend()));
    m_faveButton = new Button();
    Card::quota(m_faveButton, 1);
    connect(m_faveButton, SIGNAL(clicked()), this, SLOT(onFave()));
    buttons->add(send);
    buttons->add(m_faveButton);
    box->add(m_previewView);
    box->add(buttons);
    m_overlay->add(dim);
    m_overlay->add(box);

    root->add(m_list);
    root->add(m_empty);
    root->add(m_overlay);
    setRoot(root);
}

// A QPointer, as ContactPicker's link: at app exit the page may outlive the EngineLink. The page's own picker asks for the
// lists.
void StickerGrid::setLink(QObject *link)
{
    EngineLink *engine = qobject_cast<EngineLink *>(link);
    if (!engine || m_picker)
        return;
    m_link = link;
    m_picker = new StickerPicker(engine, this);
    connect(m_picker, SIGNAL(stateChanged()), this, SLOT(syncState()));
    m_list->setDataModel(m_picker->model()); // the picker's: the list doesn't take it
    m_picker->load();
}

// The note when the engine answered with no sticker.
void StickerGrid::syncState()
{
    m_empty->setVisible(!m_picker->loading() && m_picker->empty());
}

// A long press that opened the preview ends in a release, which may count as a tap.
void StickerGrid::onTriggered(const QVariantList &path)
{
    if (m_previewing || path.size() != 2 || !m_picker)
        return;
    emit picked(m_picker->model()->data(path).toMap().value("hash").toString());
}

// The grid's picture is in the view at once; the sticker's own file, sharper, replaces it when it arrives.
void StickerGrid::openPreview(const QVariantMap &sticker)
{
    if (m_previewing || !m_picker)
        return;
    m_preview = sticker;
    m_previewView->setImage(sticker.value("image").value<Image>());
    m_faveButton->setText(sticker.value("fave").toBool()
                              ? QCoreApplication::translate("StickersPage", "Remove from favourites")
                              : QCoreApplication::translate("StickersPage", "Add to favourites"));
    m_overlay->setVisible(true);
    m_previewing = true;
    m_picker->preview(m_previewView, sticker.value("hash").toString(), qRound(Card::du(ui(), 40)));
}

void StickerGrid::closePreview()
{
    if (m_picker)
        m_picker->endPreview();
    m_previewing = false;
    m_overlay->setVisible(false);
}

void StickerGrid::onSend()
{
    const QString hash = m_preview.value("hash").toString();
    closePreview();
    emit picked(hash);
}

void StickerGrid::onFave()
{
    if (m_picker)
        m_picker->fave(m_preview.value("hash").toString(), !m_preview.value("fave").toBool());
    closePreview();
}

StickerCells::StickerCells(StickerGrid *grid)
    : ListItemProvider(0)
    , m_grid(grid)
{
}

VisualNode *StickerCells::createItem(ListView *, const QString &type)
{
    if (type == "header")
        return new Header();
    return new StickerCell(m_grid);
}

void StickerCells::updateItem(ListView *, VisualNode *listItem, const QString &type, const QVariantList &,
                              const QVariant &data)
{
    if (type == "header")
        static_cast<Header *>(listItem)->setTitle(data.toString());
    else
        static_cast<StickerCell *>(listItem)->show(data.toMap());
}

StickerCell::StickerCell(StickerGrid *grid)
    : m_grid(grid)
{
    UIConfig *ui = grid->ui();
    setHorizontalAlignment(HorizontalAlignment::Fill);
    setVerticalAlignment(VerticalAlignment::Fill);

    m_root = new Container();
    m_root->setLayout(new DockLayout());
    m_root->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_root->setVerticalAlignment(VerticalAlignment::Fill);
    // 90 % of the cell (162 px of 180 on the Q10).
    m_root->setLeftPadding(ui->px(9));
    m_root->setRightPadding(ui->px(9));
    m_root->setTopPadding(ui->px(9));
    m_root->setBottomPadding(ui->px(9));
    LongPressHandler *press = new LongPressHandler(m_root);
    m_root->addGestureHandler(press);
    connect(press, SIGNAL(longPressed(bb::cascades::LongPressEvent *)), this, SLOT(onLongPress()));

    m_image = new ImageView();
    m_image->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_image->setVerticalAlignment(VerticalAlignment::Fill);
    m_image->setScalingMethod(ScalingMethod::AspectFit);
    m_image->setLoadEffect(ImageViewLoadEffect::None);

    m_root->add(m_image);
    setRoot(m_root);
}

void StickerCell::show(const QVariantMap &sticker)
{
    m_sticker = sticker;
    const bool has = sticker.value("hasImage").toBool();
    if (has)
        m_image->setImage(sticker.value("image").value<Image>());
    m_image->setVisible(has);
}

void StickerCell::select(bool)
{
}

void StickerCell::activate(bool activated)
{
    if (activated)
        m_root->setBackground(Card::color(m_grid->pressColor()));
    else
        m_root->resetBackground();
}

void StickerCell::reset(bool, bool activated)
{
    activate(activated);
}

void StickerCell::onLongPress()
{
    m_grid->openPreview(m_sticker);
}
