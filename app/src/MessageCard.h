#ifndef MESSAGECARD_H
#define MESSAGECARD_H

#include <bb/cascades/Color>
#include <bb/cascades/CustomControl>
#include <bb/cascades/ListItemListener>
#include <QString>
#include <QVariantMap>

namespace bb {
namespace cascades {
class ActionItem;
class ActionSet;
class ActiveTextTriggeredEvent;
class Button;
class Container;
class DeleteActionItem;
class ImageView;
class Label;
class ListView;
class UIConfig;
}
}

class Invites;

// One message as a BBM-style card (PLAN.md §7), mirroring MessageItem.qml (rich) and TextCard.qml
// (plain, what a chat is mostly made of): flat, on the black page, theirs inset on the right and
// ours on the left, with a hairline underline. Built once, with the body text and the time and
// ticks, and filled by show() on each recycle. Every other part is made the first time a message
// needs it, then kept and hidden when the next one doesn't, since rows are recycled; each goes into
// the card's column at its QML place. The column holds, in order: sender, quote, [the voice, media
// and album bubbles], label, text, [link preview], invite button, [poll].
class MessageCard : public bb::cascades::CustomControl, public bb::cascades::ListItemListener
{
    Q_OBJECT
public:
    MessageCard(bb::cascades::ListView *list, bool rich);

    // msg is a "text" or "msg" row of MessageModel.
    void show(const QVariantMap &msg);

    // ListItemListener: a press tints the card; selection isn't used.
    void select(bool selected);
    void activate(bool activated);
    void reset(bool selected, bool activated);

private slots:
    void paintBackground();
    void onLink(bb::cascades::ActiveTextTriggeredEvent *event);
    void onPopulating();
    void onReply();
    void onReact();
    void onVotes();
    void onCopy();
    void onRevoke();
    void onDeleteMe();
    void onWho();
    void onJoin();

private:
    QString str(const char *key) const { return m_msg.value(key).toString(); }
    bool flag(const char *key) const { return m_msg.value(key).toBool(); }
    Invites *joiner() const;

    void makeSender();
    void makeQuote();
    void makeLabel();
    void makeJoin();
    void makeBang();
    void makeChip();
    void makeActions();
    bb::cascades::ActionItem *action(const char *title, const char *icon, const char *slot);

    void restyle();
    void fillColumn();
    void fillFoot();
    void fillMenu();

    bb::cascades::ListView *m_list;
    bb::cascades::UIConfig *m_ui;
    bool m_rich;
    QVariantMap m_msg;
    bool m_pressed;

    // What the styling was last set from: restyle() runs again when one of them changes.
    bool m_styled;
    float m_scale;
    int m_body;
    int m_small;
    bb::cascades::Color m_plain;

    bb::cascades::Container *m_root;
    bb::cascades::Container *m_card;
    bb::cascades::Container *m_col;
    bb::cascades::Label *m_sender;
    bb::cascades::Container *m_quote;
    bb::cascades::Container *m_bar;
    bb::cascades::Label *m_quoteName;
    bb::cascades::Label *m_quoteText;
    bb::cascades::Label *m_label;
    bb::cascades::Label *m_text;
    bb::cascades::Button *m_join;
    bb::cascades::Container *m_timeBox;
    bb::cascades::Label *m_time;
    bb::cascades::ImageView *m_ticks;
    bb::cascades::Label *m_bang;
    bb::cascades::Container *m_underline;
    bb::cascades::Container *m_chip;
    bb::cascades::Label *m_reactions;

    QString m_edited; // "edited ", translated
    bb::cascades::ActionSet *m_menu;
    bb::cascades::ActionItem *m_reply;
    bb::cascades::ActionItem *m_react;
    bb::cascades::ActionItem *m_votes;
    bb::cascades::ActionItem *m_copy;
    bb::cascades::ActionItem *m_revoke;
    bb::cascades::DeleteActionItem *m_deleteMe;
};

#endif
