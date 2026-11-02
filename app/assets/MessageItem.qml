import bb.cascades 1.4

// One message as a BBM-style card (PLAN.md §7): flat, on the black page, theirs inset on the
// right and ours on the left, with a hairline underline (primary colour on ours). msg is a
// "msg" row of MessageModel. Texts take the list's shared emoji style, as ChatRow's do.
// The time and ticks sit beside the first line, not on a row of their own: a one-line message
// is one line tall (Q10, 2026-10-31: a row of its own made the cards too tall), lowered onto
// that line's baseline. The labels in the card set their margins: unset, Cascades puts about
// 20 px between them.
Container {
    id: item
    property variant msg
    horizontalAlignment: HorizontalAlignment.Fill
    // About 2 % and 9 % of the width, BBM's proportions.
    leftPadding: msg.out ? ui.du(7) : ui.du(1.5)
    rightPadding: msg.out ? ui.du(1.5) : ui.du(7)
    // The gap goes above each card: in the bottom-to-top list that also spaces the first card
    // from its day's header (Q10, 2026-10-31: 1 du touched the header and was a little tight).
    // It and the card's padding grow with the text size (Q10, 2026-11-01: 140 % looked
    // cramped).
    topPadding: ui.du(1.5 * item.ListItem.view.textScale)
    // The long-press menu (step 3.3). The set is the same on every row, with items disabled
    // rather than left out: ActionItem has no visible, and list items are recycled across rows,
    // so adding and removing actions per row isn't safe. The page does the work, through the
    // list (ListItem.view). "Delete for me" is a DeleteActionItem, which the system puts last.
    contextActions: [
        ActionSet {
            title: item.msg.deleted ? "" : (item.msg.text != "" ? item.msg.text : (item.msg.summary || item.msg.label))
            ActionItem {
                title: qsTr("Reply")
                imageSource: "file:///usr/share/icons/bb_action_reply.png"
                enabled: !item.msg.deleted
                onTriggered: item.ListItem.view.reply(item.msg)
            }
            // The icon is the emoji picker's action (step 5.11); the bar opens over the composer.
            ActionItem {
                title: qsTr("React")
                imageSource: "asset:///images/emoji.png"
                enabled: !item.msg.deleted
                onTriggered: item.ListItem.view.react(item.msg)
            }
            // Who chose what in a poll (the user's request, 0.0.1.132), as the counts' tap shows it
            // per option.
            ActionItem {
                title: qsTr("View votes")
                imageSource: "file:///usr/share/icons/ic_accept.png"
                enabled: item.msg.poll && !item.msg.deleted
                onTriggered: item.ListItem.view.showVoters(item.msg.votesText)
            }
            // /usr/share/icons has no copy icon: this one is the stock Text Messages app's.
            ActionItem {
                title: qsTr("Copy")
                imageSource: "asset:///images/copy.png"
                enabled: !item.msg.deleted && item.msg.text != ""
                onTriggered: item.ListItem.view.copy(item.msg.text)
            }
            ActionItem {
                title: qsTr("Delete for everyone")
                imageSource: "file:///usr/share/icons/bb_action_delete.png"
                enabled: item.msg.revocable
                onTriggered: item.ListItem.view.deleteForEveryone(item.msg.id)
            }
            DeleteActionItem {
                title: qsTr("Delete for me")
                onTriggered: item.ListItem.view.deleteForMe(item.msg.id)
            }
        }
    ]

    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        // A search match: the primary colour at 25 % over the page, so it reads in both themes.
        background: item.ListItem.view.foundId == item.msg.id ? Color.create("#401bd862") : ui.palette.plain
        // 2 px over 1.5 and 1 du: the text sat a little tight to the card's edges (Q10).
        leftPadding: ui.du(1.5 * item.ListItem.view.textScale) + ui.px(2)
        rightPadding: ui.du(1.5 * item.ListItem.view.textScale) + ui.px(2)
        topPadding: ui.du(1 * item.ListItem.view.textScale) + ui.px(2)
        bottomPadding: ui.du(1 * item.ListItem.view.textScale) + ui.px(2)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        Container {
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            Label {
                visible: item.msg.sender != ""
                bottomMargin: ui.du(0.5)
                text: item.msg.sender
                textFormat: TextFormat.Plain
                textStyle.base: item.ListItem.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: item.ListItem.view.smallSize
                textStyle.fontWeight: FontWeight.Bold
                textStyle.color: Color.create(item.msg.senderColor || "#FFFFFF")
            }
            // The message this one replies to.
            Container {
                visible: item.msg.quote != ""
                topMargin: 0
                bottomMargin: ui.du(0.5)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    verticalAlignment: VerticalAlignment.Fill
                    preferredWidth: ui.du(0.5)
                    background: ui.palette.primary
                }
                Container {
                    leftMargin: ui.du(1)
                    // Whose message it was, in groups.
                    Label {
                        visible: item.msg.quoteName != ""
                        bottomMargin: 0
                        text: item.msg.quoteName
                        textFormat: TextFormat.Plain
                        textStyle.base: item.ListItem.view.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: item.ListItem.view.smallSize
                        textStyle.fontWeight: FontWeight.Bold
                        textStyle.color: item.msg.quoteColor ? Color.create(item.msg.quoteColor) : ui.palette.primary
                    }
                    Label {
                        topMargin: 0
                        multiline: true
                        autoSize.maxLineCount: 2
                        // Without U+20E3, as the body below.
                        text: item.msg.quote.replace(/\u20E3/g, "")
                        textFormat: TextFormat.Plain
                        textStyle.base: item.ListItem.view.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: item.ListItem.view.smallSize
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
            }
            // Built only for voice notes: a bubble in every card would weigh down every row,
            // and the Q10's scrolling is sensitive to that (4.1).
            ControlDelegate {
                delegateActive: item.msg.voice
                sourceComponent: ComponentDefinition {
                    VoiceBubble {
                        msg: item.msg
                        view: item.ListItem.view
                    }
                }
            }
            // Built only for photos, videos, audio files and documents, as the voice one is.
            ControlDelegate {
                delegateActive: item.msg.media != ""
                sourceComponent: ComponentDefinition {
                    MediaBubble {
                        msg: item.msg
                        view: item.ListItem.view
                    }
                }
            }
            // Built only for an album (step 6.5g), in MediaBubble's place: its media is "".
            ControlDelegate {
                delegateActive: item.msg.album.length > 0
                sourceComponent: ComponentDefinition {
                    AlbumBubble {
                        msg: item.msg
                        view: item.ListItem.view
                    }
                }
            }
            Label {
                visible: item.msg.label != ""
                topMargin: 0
                bottomMargin: 0
                text: item.msg.label
                textFormat: TextFormat.Plain
                textStyle.base: item.ListItem.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: item.ListItem.view.bodySize
                textStyle.fontStyle: FontStyle.Italic
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
            Label {
                visible: item.msg.text != ""
                topMargin: 0
                multiline: true
                // Without U+20E3, a keycap's frame. The renderer draws a digit and its marks from one
                // font, and none has both: ours has no digits, the system's no frame. So a keycap one
                // lost its "1" (Q10, 2026-10-31). Only the shown text: the message and Copy keep it.
                text: item.msg.text.replace(/\u20E3/g, "")
                textFormat: TextFormat.Plain
                // Links, numbers and addresses are tappable, opened by the system (the user's
                // request, 0.0.1.132: a YouTube link did nothing). Not on a deleted message's note.
                content.flags: item.msg.deleted ? TextContentFlag.Default : TextContentFlag.ActiveText
                textStyle.base: item.ListItem.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: item.ListItem.view.bodySize
                textStyle.fontStyle: item.msg.deleted ? FontStyle.Italic : FontStyle.Default
                textStyle.color: item.msg.deleted ? ui.palette.secondaryTextOnPlain : ui.palette.textOnPlain
            }
            // Built only for texts with a link preview, as the voice one is, after the text (step 6.5f).
            ControlDelegate {
                delegateActive: item.msg.linkTitle != "" && !item.msg.deleted
                sourceComponent: ComponentDefinition {
                    LinkPreview {
                        msg: item.msg
                        view: item.ListItem.view
                    }
                }
            }
            // Built only for polls, as the voice one is, after the question: the options (step 6.5e).
            ControlDelegate {
                delegateActive: item.msg.poll && !item.msg.deleted
                sourceComponent: ComponentDefinition {
                    PollBubble {
                        msg: item.msg
                        view: item.ListItem.view
                    }
                }
            }
        }
        // The time ("edited" before it) and our message's state, as in ChatRow: sending, sent,
        // delivered, read, or a red "!" when it failed. Top-aligned boxes put the time's baseline
        // above the first line's: 7 px under a name or quote (small), 12 px under body text,
        // measured at 85 % and 105 % on the Q10, then scaled with the text size setting.
        // ponytail: linear in the scale, not measured at 90-140 %.
        Container {
            verticalAlignment: VerticalAlignment.Top
            leftMargin: ui.du(1)
            topPadding: item.msg.sender != "" || item.msg.quote != "" ? ui.px(7 * item.ListItem.view.textScale) : ui.px(12 * item.ListItem.view.textScale)
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            // One label: a separate "edited" label never showed on the Q10 (2026-10-31).
            Label {
                verticalAlignment: VerticalAlignment.Center
                text: (item.msg.edited ? qsTr("edited") + " " : "") + item.msg.time
                textFormat: TextFormat.Plain
                // A percentage of its base, SmallText (fontsize.h): the same at Normal, and
                // growing with the text size as the body does.
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: Math.round(100 * item.ListItem.view.textScale)
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
            ImageView {
                visible: item.msg.out && item.msg.state < 4
                verticalAlignment: VerticalAlignment.Center
                imageSource: item.msg.state == 0 ? "asset:///images/clock.png" : item.msg.state == 1 ? "asset:///images/tick.png" : "asset:///images/ticks.png"
                filterColor: item.msg.state == 3 ? ui.palette.primary : ui.palette.secondaryTextOnPlain
            }
            Label {
                visible: item.msg.out && item.msg.state == 4
                verticalAlignment: VerticalAlignment.Center
                text: "!"
                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                textStyle.fontWeight: FontWeight.Bold
                textStyle.color: Color.create("#FF3333")
            }
        }
    }
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        preferredHeight: ui.px(2)
        background: item.msg.out ? ui.palette.primary : ui.palette.secondaryTextOnPlain
    }
    // The reactions (step 5.11), under the card at its left edge, theirs and ours alike, with the
    // card's own left padding so the emoji line up with its text. A tap says who reacted.
    Container {
        visible: (item.msg.reactionCount || 0) > 0
        horizontalAlignment: HorizontalAlignment.Left
        topMargin: ui.du(0.5)
        background: ui.palette.plain
        leftPadding: ui.du(1.5 * item.ListItem.view.textScale) + ui.px(2)
        rightPadding: ui.du(1)
        topPadding: ui.du(0.5)
        bottomPadding: ui.du(0.5)
        gestureHandlers: TapHandler {
            onTapped: item.ListItem.view.whoReacted(item.msg.id)
        }
        Label {
            topMargin: 0
            bottomMargin: 0
            text: (item.msg.reactions || "") + (item.msg.reactionCount > 1 ? " " + item.msg.reactionCount : "")
            textFormat: TextFormat.Plain
            textStyle.base: item.ListItem.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: item.ListItem.view.smallSize
        }
    }
}
