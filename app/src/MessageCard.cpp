#include "MessageCard.h"

#include <bb/cascades/ActionSet>
#include <bb/cascades/ActiveTextHandler>
#include <bb/cascades/ActiveTextTriggeredEvent>
#include <bb/cascades/Button>
#include <bb/cascades/Container>
#include <bb/cascades/ContextMenuHandler>
#include <bb/cascades/FontStyle>
#include <bb/cascades/FontWeight>
#include <bb/cascades/HorizontalAlignment>
#include <bb/cascades/ImageView>
#include <bb/cascades/Label>
#include <bb/cascades/LabelAutoSizeProperties>
#include <bb/cascades/ListView>
#include <bb/cascades/SystemDefaults>
#include <bb/cascades/TapHandler>
#include <bb/cascades/TextContentFlag>
#include <bb/cascades/TextContentProperties>
#include <bb/cascades/TextFormat>
#include <bb/cascades/TextStyleDefinition>
#include <bb/cascades/UIConfig>
#include <bb/cascades/UIPalette>
#include <bb/cascades/VerticalAlignment>

#include "CardStyle.h"
#include "MediaBubble.h"
#include "PollBubble.h"

using namespace bb::cascades;

namespace {
// The QML file's name: the translations of MessageItem.qml's strings (hyperberry_it.ts) apply.
const char *const kContext = "MessageItem";
}

MessageCard::MessageCard(ListView *list, bool rich, bool picture)
    : m_list(list)
    , m_ui(list->ui())
    , m_rich(rich)
    , m_pressed(false)
    , m_timeOnPicture(false)
    , m_styled(false)
    , m_scale(0)
    , m_body(0)
    , m_small(0)
    , m_sender(0)
    , m_quote(0)
    , m_bar(0)
    , m_quoteName(0)
    , m_quoteText(0)
    , m_label(0)
    , m_join(0)
    , m_voice(0)
    , m_media(0)
    , m_album(0)
    , m_link(0)
    , m_poll(0)
    , m_bang(0)
    , m_chip(0)
    , m_reactions(0)
    , m_edited(Card::tr(kContext, "edited") + " ")
    , m_reply(0)
    , m_react(0)
    , m_votes(0)
    , m_copy(0)
    , m_revoke(0)
    , m_deleteMe(0)
{
    // The card's own node and its root: a list item is as wide as its content unless it fills.
    setHorizontalAlignment(HorizontalAlignment::Fill);
    m_root = new Container();
    m_root->setHorizontalAlignment(HorizontalAlignment::Fill);

    // The card: the text column, and the time and ticks beside its first line, not on a row of their
    // own, so a one-line message is one line tall (Q10, 2026-10-31: a row of its own made the cards
    // too tall). The labels in the card set their margins: unset, Cascades puts about 20 px between them.
    m_card = new Container();
    m_card->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_card->setLayout(Card::across());

    m_col = new Container();
    Card::quota(m_col, 1);

    m_text = new Label();
    m_text->setTopMargin(0);
    m_text->setMultiline(true);
    // A mention's link is ours: the system can't open it, so the page does.
    ActiveTextHandler *links = new ActiveTextHandler(m_text);
    m_text->setActiveTextHandler(links);
    connect(links, SIGNAL(triggered(bb::cascades::ActiveTextTriggeredEvent *)), this,
            SLOT(onLink(bb::cascades::ActiveTextTriggeredEvent *)));
    m_col->add(m_text);

    m_timeBox = new Container();
    m_timeBox->setVerticalAlignment(VerticalAlignment::Top);
    m_timeBox->setLeftMargin(m_ui->du(1));
    m_timeBox->setLayout(Card::across());

    // One label for "edited" and the time: a separate one never showed on the Q10 (2026-10-31). A
    // percentage of its base, SmallText: the same at Normal, and growing with the text size as the body does.
    m_time = new Label();
    m_time->setVerticalAlignment(VerticalAlignment::Center);
    m_time->setTextFormat(TextFormat::Plain);
    m_time->textStyle()->setBase(SystemDefaults::TextStyles::smallText());
    m_time->textStyle()->setFontSize(FontSize::PercentageValue);
    m_ticks = new ImageView();
    m_ticks->setVerticalAlignment(VerticalAlignment::Center);
    m_timeBox->add(m_time);
    m_timeBox->add(m_ticks);

    m_card->add(m_col);
    m_card->add(m_timeBox);

    m_underline = new Container();
    m_underline->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_underline->setPreferredHeight(m_ui->px(2));

    // The picture card: the head and the card around the picture, which MessageCardBubbles puts
    // between them. No margins between the three: the paddings space them.
    if (picture) {
        m_frame = new Container();
        m_frame->setHorizontalAlignment(HorizontalAlignment::Fill);
        m_head = new Container();
        m_head->setHorizontalAlignment(HorizontalAlignment::Fill);
        m_head->setBottomMargin(0);
        m_card->setTopMargin(0);
        m_frame->add(m_head);
        m_frame->add(m_card);
        m_root->add(m_frame);
    } else {
        m_frame = m_card;
        m_head = 0;
        m_root->add(m_card);
    }
    m_root->add(m_underline);
    setRoot(m_root);

    // The long-press menu (step 3.3): the actions are made at the first populating, which comes just
    // before it opens, and their enabled states set at each. The set is the same on every row, with
    // items disabled rather than left out: ActionItem has no visible, and rows are recycled.
    m_menu = new ActionSet();
    addActionSet(m_menu);
    ContextMenuHandler *handler = new ContextMenuHandler(this);
    setContextMenuHandler(handler);
    connect(handler, SIGNAL(populating(bb::cascades::ContextMenuShowingEvent *)), this,
            SLOT(onPopulating()));

    // The search match is tinted: the page sets foundId.
    if (!connect(list, SIGNAL(foundIdChanged()), this, SLOT(paintBackground())))
        qWarning("cards: the list has no foundIdChanged");
}

void MessageCard::show(const QVariantMap &msg)
{
    m_msg = msg;
    const bool out = flag("out");
    const bool deleted = flag("deleted");

    // The parts this message needs and the card hasn't made yet.
    if (!str("sender").isEmpty() && !m_sender)
        makeSender();
    if (!str("quote").isEmpty() && !m_quote)
        makeQuote();
    if (m_rich && !str("label").isEmpty() && !m_label)
        makeLabel();
    if (m_rich && !str("invite").isEmpty() && !deleted && !m_join)
        makeJoin();
    if (m_rich)
        makeBubbles();
    if (out && m_msg.value("state").toInt() == 4 && !m_bang)
        makeBang();
    if (m_msg.value("reactionCount").toInt() > 0 && !m_chip)
        makeChip();
    restyle();

    // Theirs inset on the right and ours on the left: about 2 % and 9 % of the width, BBM's proportions.
    m_root->setLeftPadding(out ? m_ui->du(7) : m_ui->du(1.5));
    m_root->setRightPadding(out ? m_ui->du(1.5) : m_ui->du(7));
    fillColumn();
    if (m_rich)
        fillBubbles();
    fillFoot();
    fillMenu();
    paintBackground();
}

// Pressed: the page's pressColor. A search match: the primary colour at 25 % over the page, so it
// reads in both themes. Else the page's own colour. An empty foundId matches nothing.
void MessageCard::paintBackground()
{
    const QString found = Card::foundId(m_list);
    if (m_pressed)
        m_frame->setBackground(Card::color(Card::pressColor(m_list)));
    else if (!found.isEmpty() && found == str("id"))
        m_frame->setBackground(Card::color("#401bd862"));
    else
        m_frame->setBackground(m_ui->palette()->plain());
}

void MessageCard::select(bool)
{
}

void MessageCard::activate(bool activated)
{
    m_pressed = activated;
    paintBackground();
}

void MessageCard::reset(bool, bool activated)
{
    m_pressed = activated;
    paintBackground();
}

// Everything the text size and the theme's colours decide, set again only when one of them changed
// or a part was made. The gap goes above each card: in the bottom-to-top list that also spaces the
// first card from its day's header (Q10, 2026-10-31). It and the card's padding grow with the text
// size (Q10, 2026-11-01: 140 % looked cramped). 2 px over 1.5 and 1 du: the text sat a little
// tight to the card's edges (Q10).
void MessageCard::restyle()
{
    const float scale = Card::textScale(m_list);
    const int body = Card::bodySize(m_list);
    const int small = Card::smallSize(m_list);
    const Color plain = m_ui->palette()->plain();
    if (m_styled && scale == m_scale && body == m_body && small == m_small && plain == m_plain)
        return;
    m_styled = true;
    m_scale = scale;
    m_body = body;
    m_small = small;
    m_plain = plain;

    const Color quiet = m_ui->palette()->secondaryTextOnPlain();
    const float side = Card::side(m_ui, scale);
    m_root->setTopPadding(m_ui->du(1.5 * scale));
    m_card->setLeftPadding(side);
    m_card->setRightPadding(side);
    m_card->setTopPadding(m_ui->du(1 * scale)); // 2 px less: the name and time a little higher (Q10, 0.0.1.212)
    m_card->setBottomPadding(m_ui->du(1 * scale) + m_ui->px(2));
    if (m_head) {
        m_head->setLeftPadding(side);
        m_head->setRightPadding(side);
        m_head->setTopPadding(m_ui->du(1 * scale));
        m_head->setBottomPadding(m_ui->du(1 * scale) + m_ui->px(2));
    }
    Card::style(m_text, m_list, body);
    // A little under SmallText (the user, 2026-11-08: the time a bit smaller).
    m_time->textStyle()->setFontSizeValue(qRound(85 * scale));
    if (m_sender)
        Card::style(m_sender, m_list, small);
    if (m_quote) {
        m_bar->setBackground(m_ui->palette()->primary());
        Card::style(m_quoteName, m_list, small);
        Card::style(m_quoteText, m_list, small);
        m_quoteText->textStyle()->setColor(quiet);
    }
    if (m_label) {
        Card::style(m_label, m_list, body);
        m_label->textStyle()->setColor(quiet);
    }
    if (m_chip) {
        m_chip->setBackground(plain);
        m_chip->setLeftPadding(side);
        Card::style(m_reactions, m_list, body); // the text's size, not the small one (the user, 2026-11-08)
    }
}

void MessageCard::makeSender()
{
    m_sender = new Label();
    m_sender->setBottomMargin(m_ui->du(0.5) + m_ui->px(2)); // +2 px (Q10, 0.0.1.212)
    m_sender->textStyle()->setFontWeight(FontWeight::Bold);
    (m_head ? m_head : m_col)->insert(0, m_sender);
    m_styled = false;
}

// The message this one replies to, on a tint a little off the card's colour.
void MessageCard::makeQuote()
{
    m_quote = new Container();
    m_quote->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_quote->setTopMargin(0);
    m_quote->setBottomMargin(m_ui->du(0.5));
    m_quote->setLayout(Card::across());

    m_bar = new Container();
    m_bar->setVerticalAlignment(VerticalAlignment::Fill);
    m_bar->setMinWidth(m_ui->du(0.5));
    m_bar->setMaxWidth(m_ui->du(0.5));

    // The quota leaves the bar its width: a two-line quote squeezed it to nothing (0.0.1.136, Q10).
    Container *words = new Container();
    words->setLeftMargin(m_ui->du(1));
    words->setRightPadding(m_ui->du(1));
    words->setTopPadding(m_ui->du(0.5));
    words->setBottomPadding(m_ui->du(0.5));
    Card::quota(words, 1);

    // Whose message it was, in groups.
    m_quoteName = new Label();
    m_quoteName->setBottomMargin(0);
    m_quoteName->textStyle()->setFontWeight(FontWeight::Bold);
    m_quoteText = new Label();
    m_quoteText->setTopMargin(0);
    m_quoteText->setMultiline(true);
    m_quoteText->autoSize()->setMaxLineCount(2);

    words->add(m_quoteName);
    words->add(m_quoteText);
    m_quote->add(m_bar);
    m_quote->add(words);
    (m_head ? m_head : m_col)->insert(m_sender ? 1 : 0, m_quote);
    m_styled = false;
}

// After the voice, media and album bubbles, before the text.
void MessageCard::makeLabel()
{
    m_label = new Label();
    m_label->setTopMargin(0);
    m_label->setBottomMargin(0);
    m_label->textStyle()->setFontStyle(FontStyle::Italic);
    m_col->insert(m_col->indexOf(m_text), m_label);
    m_styled = false;
}

// An invite we received (step 6.6f), after its text.
void MessageCard::makeJoin()
{
    m_join = new Button();
    m_join->setTopMargin(m_ui->du(1));
    m_join->setHorizontalAlignment(HorizontalAlignment::Fill);
    m_join->setText(Card::tr(kContext, "Join group"));
    connect(m_join, SIGNAL(clicked()), this, SLOT(onJoin()));
    place(m_join, m_poll);
}

// Our message that failed: a red "!" after the ticks.
void MessageCard::makeBang()
{
    m_bang = new Label();
    m_bang->setVerticalAlignment(VerticalAlignment::Center);
    m_bang->setText("!");
    m_bang->textStyle()->setBase(SystemDefaults::TextStyles::subtitleText());
    m_bang->textStyle()->setFontWeight(FontWeight::Bold);
    m_bang->textStyle()->setColor(Card::color("#FF3333"));
    m_timeBox->add(m_bang);
}

// The reactions (step 5.11), under the card at its left edge, theirs and ours alike, with the
// card's own left padding so the emoji line up with its text. A tap says who reacted.
void MessageCard::makeChip()
{
    m_chip = new Container();
    m_chip->setHorizontalAlignment(HorizontalAlignment::Left);
    m_chip->setTopMargin(m_ui->du(0.5));
    m_chip->setRightPadding(m_ui->du(1));
    m_chip->setTopPadding(m_ui->du(0.5));
    m_chip->setBottomPadding(m_ui->du(0.5));
    TapHandler *tap = new TapHandler(m_chip);
    m_chip->addGestureHandler(tap);
    connect(tap, SIGNAL(tapped(bb::cascades::TapEvent *)), this, SLOT(onWho()));

    m_reactions = new Label();
    m_reactions->setTopMargin(0);
    m_reactions->setBottomMargin(0);
    m_chip->add(m_reactions);
    m_root->add(m_chip);
    m_styled = false;
}

// The sender, the quote, the text and what hangs on it. A part a message doesn't need is hidden,
// as it was set for an earlier one.
void MessageCard::fillColumn()
{
    const bool deleted = flag("deleted");
    const QString sender = str("sender");
    if (m_sender) {
        m_sender->setVisible(!sender.isEmpty());
        if (!sender.isEmpty()) {
            const QString tint = str("senderColor");
            m_sender->setText(sender);
            m_sender->textStyle()->setColor(Card::color(tint.isEmpty() ? QString("#FFFFFF") : tint));
        }
    }

    const QString quote = str("quote");
    if (m_quote) {
        m_quote->setVisible(!quote.isEmpty());
        if (!quote.isEmpty()) {
            const QString name = str("quoteName");
            const QString tint = str("quoteColor");
            m_quote->setBackground(Card::color(Card::quoteTint(m_list)));
            m_quoteName->setVisible(!name.isEmpty());
            m_quoteName->setText(name);
            m_quoteName->textStyle()->setColor(tint.isEmpty() ? m_ui->palette()->primary() : Card::color(tint));
            m_quoteText->setText(Card::noKeycaps(quote));
        }
    }

    if (m_label) {
        const QString label = str("label");
        m_label->setVisible(!label.isEmpty());
        if (!label.isEmpty())
            m_label->setText(label);
    }

    // With mentions the engine's html has them linked (MessageKinds::html, which drops the keycaps
    // too); else the plain text. Links, numbers and addresses are tappable, opened by the system
    // (the user's request, 0.0.1.132: a YouTube link did nothing), but not on a deleted message's note.
    const QString html = str("html");
    const QString text = str("text");
    if (m_rich)
        m_text->setVisible(!text.isEmpty());
    m_text->setTextFormat(html.isEmpty() ? TextFormat::Plain : TextFormat::Html);
    m_text->setText(html.isEmpty() ? Card::noKeycaps(text) : html);
    m_text->content()->setFlags(deleted ? TextContentFlag::Default : TextContentFlag::ActiveText);
    m_text->textStyle()->setFontStyle(deleted ? FontStyle::Italic : FontStyle::Default);
    m_text->textStyle()->setColor(deleted ? m_ui->palette()->secondaryTextOnPlain()
                                          : m_ui->palette()->textOnPlain());

    if (m_join)
        m_join->setVisible(!str("invite").isEmpty() && !deleted);

    // The picture card: the head when there's a sender or quote, the card when there's a caption
    // (or no picture: a deleted one).
    if (m_head) {
        m_head->setVisible(!sender.isEmpty() || !quote.isEmpty());
        m_card->setVisible(!text.isEmpty() || !str("label").isEmpty() || str("media").isEmpty());
    }
}

// The time ("edited" before it) and our message's state, as in ChatRow: sending, sent, delivered,
// read, or a red "!" when it failed; then the underline and the reactions' chip.
void MessageCard::fillFoot()
{
    const bool out = flag("out");
    const int state = m_msg.value("state").toInt();
    m_time->setText((flag("edited") ? m_edited : QString()) + str("time"));
    // Top-aligned boxes put the time's baseline above the first line's: 7 px under a name or quote
    // (small), 12 px under body text, measured at 85 % and 105 % on the Q10, then scaled with the
    // text size setting.
    // ponytail: linear in the scale, not measured at 90-140 %.
    // In the picture card the head holds them, so the caption is the card's first line. +2 px for
    // the time at 85 %, whose baseline sits higher (Q10, 0.0.1.212: +3 was 1 px low; 0.0.1.216: 2 more).
    const bool small = !m_head && (!str("sender").isEmpty() || !str("quote").isEmpty());
    const bool onPicture = m_head && !m_card->isVisible();
    placeTime(onPicture);
    if (!onPicture)
        m_timeBox->setTopPadding(m_ui->px((small ? 7 : 12) * m_scale));
    // White on the picture's dark chip.
    const Color quiet = onPicture ? Color::White : m_ui->palette()->secondaryTextOnPlain();
    m_time->textStyle()->setColor(quiet);

    const bool ticked = out && state < 4;
    m_ticks->setVisible(ticked);
    if (ticked) {
        m_ticks->setImageSource(Card::url(state == 0 ? "asset:///images/clock.png"
                                    : state == 1 ? "asset:///images/tick.png" : "asset:///images/ticks.png"));
        m_ticks->setFilterColor(state == 3 ? m_ui->palette()->primary() : quiet);
    }
    if (m_bang)
        m_bang->setVisible(out && state == 4);

    m_underline->setBackground(out ? m_ui->palette()->primary() : m_ui->palette()->secondaryTextOnPlain());

    const int count = m_msg.value("reactionCount").toInt();
    if (m_chip) {
        m_chip->setVisible(count > 0);
        if (count > 0)
            m_reactions->setText(str("reactions") + (count > 1 ? QString(" %1").arg(count) : QString()));
    }
}


// The time and ticks: beside the card's first line, or on a photo with no caption at its bottom
// right, on a dark chip so they read on any picture, as Android shows them. Moved only when the
// place changes, since rows are recycled.
void MessageCard::placeTime(bool onPicture)
{
    if (onPicture != m_timeOnPicture) {
        m_timeOnPicture = onPicture;
        if (onPicture) {
            m_card->remove(m_timeBox);
            m_media->stamp()->add(m_timeBox);
        } else {
            m_media->stamp()->remove(m_timeBox);
            m_card->add(m_timeBox);
        }
    }
    const float chip = onPicture ? m_ui->du(1) : 0;
    m_timeBox->setLeftMargin(onPicture ? 0 : m_ui->du(1));
    m_timeBox->setLeftPadding(chip);
    m_timeBox->setRightPadding(chip);
    m_timeBox->setBottomPadding(onPicture ? m_ui->du(0.5) : 0);
    if (onPicture) {
        m_timeBox->setTopPadding(m_ui->du(0.5));
        m_timeBox->setBackground(Card::color("#80000000"));
    } else {
        m_timeBox->resetBackground();
    }
}
