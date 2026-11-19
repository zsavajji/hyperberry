#ifndef CARDSTYLE_H
#define CARDSTYLE_H

#include <bb/cascades/Color>
#include <bb/cascades/Control>
#include <bb/cascades/FontSize>
#include <bb/cascades/Label>
#include <bb/cascades/LayoutOrientation>
#include <bb/cascades/ListView>
#include <bb/cascades/StackLayout>
#include <bb/cascades/StackLayoutProperties>
#include <bb/cascades/TextFormat>
#include <bb/cascades/TextStyle>
#include <bb/cascades/TextStyleDefinition>
#include <bb/cascades/UIConfig>
#include <bb/cascades/UIPalette>
#include <bb/device/DisplayInfo>
#include <QCoreApplication>
#include <QMetaObject>
#include <QString>
#include <QUrl>
#include <QVariant>

// What the C++ cards (MessageCards.cpp, step 8.6) read from the chat page's list, which QML's
// ListItem.view gave the QML cards, and the few setters every card repeats.
namespace Card {

// The list's properties (ChatPage.qml): the text sizes in percent of the emoji style, the scale
// they come from, and the colours as strings.
inline int bodySize(bb::cascades::ListView *list) { return list->property("bodySize").toInt(); }
inline int smallSize(bb::cascades::ListView *list) { return list->property("smallSize").toInt(); }
inline float textScale(bb::cascades::ListView *list) { return list->property("textScale").toFloat(); }
inline QString quoteTint(bb::cascades::ListView *list) { return list->property("quoteTint").toString(); }
inline QString pressColor(bb::cascades::ListView *list) { return list->property("pressColor").toString(); }
// Our cards' colour, "" for the page's own (ChatPage.qml's ownColor).
inline QString ownColor(bb::cascades::ListView *list) { return list->property("ownColor").toString(); }
// The strip under their cards, "" for the theme's quiet grey (ChatPage.qml's theirBar).
inline QString theirBar(bb::cascades::ListView *list) { return list->property("theirBar").toString(); }
// Bright: the list's flag (ChatPage.qml), for the colours that differ from Dark's.
inline bool bright(bb::cascades::ListView *list) { return list->property("bright").toBool(); }
// The text, and the quiet text (times, previews, quotes): on Bright darker than the theme's, which read faint
// there (the user, 2026-11-17); on Dark the theme's.
inline bb::cascades::Color text(bb::cascades::ListView *list, bb::cascades::UIConfig *ui)
{
    return bright(list) ? bb::cascades::Color::fromARGB(0xff111111) : ui->palette()->textOnPlain();
}
inline bb::cascades::Color quiet(bb::cascades::ListView *list, bb::cascades::UIConfig *ui)
{
    return bright(list) ? bb::cascades::Color::fromARGB(0xff5e5e5e) : ui->palette()->secondaryTextOnPlain();
}
// Their cards' colour, "" for the page's own (ChatPage.qml's theirColor).
inline QString theirColor(bb::cascades::ListView *list) { return list->property("theirColor").toString(); }
// The page's own colour behind the cards, "" for the theme's (ChatPage.qml's pageColor, main.qml's pageGrey: Dark with
// Settings > Appearance > Dark background on Grey, the user's ask, 2026-11-19).
inline QString pageColor(bb::cascades::ListView *list) { return list->property("pageColor").toString(); }
inline QString foundId(bb::cascades::ListView *list) { return list->property("foundId").toString(); }
inline bb::cascades::TextStyle emojiStyle(bb::cascades::ListView *list)
{
    return list->property("emojiStyle").value<bb::cascades::TextStyle>();
}

// "#RRGGBB", or "#AARRGGBB" as QML's Color.create takes it; six digits are opaque.
inline bb::cascades::Color color(const QString &hex)
{
    bool ok = false;
    unsigned int argb = hex.mid(1).toUInt(&ok, 16);
    if (!ok)
        return bb::cascades::Color();
    if (hex.size() == 7)
        argb |= 0xFF000000u;
    return bb::cascades::Color::fromARGB(argb);
}

// What a picture's empty box and an album's tile are painted with: the page's colour as the chat page has it.
inline bb::cascades::Color page(bb::cascades::ListView *list, bb::cascades::UIConfig *ui)
{
    const QString hex = pageColor(list);
    return hex.isEmpty() ? ui->palette()->background() : color(hex);
}

// A text in the list's emoji style at a percentage of its size, plain: the card's labels all start so.
inline void style(bb::cascades::Label *label, bb::cascades::ListView *list, int percent)
{
    bb::cascades::TextStyleDefinition *s = label->textStyle();
    s->setBase(emojiStyle(list));
    s->setFontSize(bb::cascades::FontSize::PercentageValue);
    s->setFontSizeValue(percent);
    label->setTextFormat(bb::cascades::TextFormat::Plain);
}

// Without U+20E3, a keycap's frame: the renderer draws a digit and its marks from one font, and none
// has both, so a keycap one lost its "1" (Q10, 2026-10-31). Only the shown text: Copy keeps it.
inline QString noKeycaps(QString text)
{
    text.remove(QChar(0x20E3));
    return text;
}

// Calls one of the page's functions on the list. QML functions take and return a QVariant, and
// the caller passes as many arguments as the function has: none, one or two.
inline QVariant call(bb::cascades::ListView *list, const char *fn, const QVariant &a = QVariant(),
                     const QVariant &b = QVariant())
{
    QVariant result;
    if (b.isValid())
        QMetaObject::invokeMethod(list, fn, Q_RETURN_ARG(QVariant, result), Q_ARG(QVariant, a),
                                  Q_ARG(QVariant, b));
    else if (a.isValid())
        QMetaObject::invokeMethod(list, fn, Q_RETURN_ARG(QVariant, result), Q_ARG(QVariant, a));
    else
        QMetaObject::invokeMethod(list, fn, Q_RETURN_ARG(QVariant, result));
    return result;
}

// The translation of what the QML file of that name had in qsTr, so its .ts entries still apply.
inline QString tr(const char *context, const char *source)
{
    return QCoreApplication::translate(context, source);
}

// An image or icon's URL, "asset:///..." or "file:///...", as QML's imageSource takes it.
inline QUrl url(const char *source)
{
    return QUrl(QString::fromLatin1(source));
}

// The share of a stack's free space a control takes (StackLayoutProperties' spaceQuota).
inline void quota(bb::cascades::Control *control, float share)
{
    bb::cascades::StackLayoutProperties *p = new bb::cascades::StackLayoutProperties();
    p->setSpaceQuota(share);
    control->setLayoutProperties(p);
}

// A stack that lays its children out from the left.
inline bb::cascades::StackLayout *across()
{
    bb::cascades::StackLayout *layout = new bb::cascades::StackLayout();
    layout->setOrientation(bb::cascades::LayoutOrientation::LeftToRight);
    return layout;
}

// The screen's width at start.
// ponytail: a phone that rotates would want the list's.
inline int screen()
{
    static const int width = bb::device::DisplayInfo().pixelSize().width();
    return width;
}

// The Passport draws everything a fifth bigger: at 12 px a du its 1440 px are 120 du, where the Q10
// has 80, so a du layout came out small there (a Passport user, 2026-11-18; the user tried a third,
// too much, and chose a fifth). QML gets the same numbers as "zoom" and "pxZoom" (main.cpp).
inline float zoom() { return screen() == 1440 ? 1.2f : 1.0f; }
inline float du(bb::cascades::UIConfig *ui, float n) { return ui->du(zoom() * n); }
// What a size measured on the Q10 in pixels (9 to a du) takes to grow as the du ones do: on the
// Passport 12 / 9 times the zoom, elsewhere 1 (the voice note's fixed 381 px waveform and 72 px disc
// stayed Q10-sized while their text grew, 0.2.0.359).
inline float pxZoom() { return screen() == 1440 ? zoom() * 12 / 9 : 1.0f; }

// A card's width at most, and a picture's: 80 % of the screen. A card is as wide as what it holds,
// up to this (the user, 2026-11-14: on the Passport a one-letter message spanned the screen).
inline float width(bb::cascades::UIConfig *) { return 0.8f * screen(); }

// The card's padding at each side, growing with the text size: 2 px over 1.5 du at 100 %.
inline float side(bb::cascades::UIConfig *ui, float scale) { return du(ui, 1.5 * scale) + ui->px(2); }

}

#endif
