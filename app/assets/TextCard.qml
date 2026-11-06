import bb.cascades 1.4

// The card of a plain text message (step 8.5), what a chat is mostly made of. MessageItem holds every
// kind of message, 24 controls and a long-press menu in each card, 46 ms a card (Q10, 0.0.1.159), and
// its look is this one's, kept in step by hand. Here the quote and the reactions are built only when a
// message has them, and the menu when it's opened: ContextMenuHandler's populating comes just before,
// for this purpose. msg is a "text" row of MessageModel, view the chat page's list.
Container {
    id: item
    property variant msg
    property variant view: item.ListItem.view
    horizontalAlignment: HorizontalAlignment.Fill
    leftPadding: msg.out ? ui.du(7) : ui.du(1.5)
    rightPadding: msg.out ? ui.du(1.5) : ui.du(7)
    topPadding: ui.du(1.5 * item.view.textScale)
    // Empty until opened, and filled for good then: rows are recycled, and the actions follow item.msg.
    // Only ActionSet's own add and remove can be called from QML, not Control's addActionSet.
    contextActions: [
        ActionSet {
            id: menu
            title: item.msg.deleted ? "" : item.msg.text
        }
    ]
    contextMenuHandler: ContextMenuHandler {
        onPopulating: {
            if (menu.count() > 0) {
                return
            }
            // made stays, a child of the card: the actions' bindings live in the context it was made in.
            var made = menuDef.createObject(item)
            while (made.count() > 0) {
                var action = made.at(0)
                made.remove(action)
                menu.add(action)
            }
        }
    }
    attachedObjects: [
        ComponentDefinition {
            id: menuDef
            ActionSet {
                ActionItem {
                    title: qsTr("Reply")
                    imageSource: "file:///usr/share/icons/bb_action_reply.png"
                    enabled: !item.msg.deleted && !item.msg.readOnly
                    onTriggered: item.view.reply(item.msg)
                }
                ActionItem {
                    title: qsTr("React")
                    imageSource: "asset:///images/emoji.png"
                    enabled: !item.msg.deleted && !item.msg.readOnly
                    onTriggered: item.view.react(item.msg)
                }
                ActionItem {
                    title: qsTr("Copy")
                    imageSource: "asset:///images/copy.png"
                    enabled: !item.msg.deleted && item.msg.text != ""
                    onTriggered: item.view.copy(item.msg.text)
                }
                ActionItem {
                    title: qsTr("Delete for everyone")
                    imageSource: "file:///usr/share/icons/bb_action_delete.png"
                    enabled: item.msg.revocable
                    onTriggered: item.view.deleteForEveryone(item.msg.id)
                }
                DeleteActionItem {
                    title: qsTr("Delete for me")
                    enabled: !item.msg.readOnly
                    onTriggered: item.view.deleteForMe(item.msg.id)
                }
            }
        }
    ]

    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        background: item.ListItem.active ? Color.create(item.view.pressColor) : item.view.foundId == item.msg.id ? Color.create("#401bd862") : ui.palette.plain
        leftPadding: ui.du(1.5 * item.view.textScale) + ui.px(2)
        rightPadding: ui.du(1.5 * item.view.textScale) + ui.px(2)
        topPadding: ui.du(1 * item.view.textScale) + ui.px(2)
        bottomPadding: ui.du(1 * item.view.textScale) + ui.px(2)
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
                textStyle.base: item.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: item.view.smallSize
                textStyle.fontWeight: FontWeight.Bold
                textStyle.color: Color.create(item.msg.senderColor || "#FFFFFF")
            }
            ControlDelegate {
                visible: delegateActive
                delegateActive: item.msg.quote != ""
                horizontalAlignment: HorizontalAlignment.Fill
                topMargin: 0
                bottomMargin: ui.du(0.5)
                sourceComponent: ComponentDefinition {
                    CardQuote {
                        msg: item.msg
                        view: item.view
                    }
                }
            }
            // As MessageItem's body: its comments there say why each of these is so.
            Label {
                topMargin: 0
                multiline: true
                text: item.msg.html ? item.msg.html : item.msg.text.replace(/\u20E3/g, "")
                textFormat: item.msg.html ? TextFormat.Html : TextFormat.Plain
                content.flags: item.msg.deleted ? TextContentFlag.Default : TextContentFlag.ActiveText
                activeTextHandler: ActiveTextHandler {
                    onTriggered: {
                        var h = event.href.toString()
                        if (h.indexOf("hbmention:") == 0) {
                            event.abort() // ours: the system can't open it
                            item.view.openPerson(h.substring(10), event.text)
                        } else if (item.view.joiner.openLink(h)) {
                            event.abort() // a group or channel invite: it's asked here, not in the browser
                        }
                    }
                }
                textStyle.base: item.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: item.view.bodySize
                textStyle.fontStyle: item.msg.deleted ? FontStyle.Italic : FontStyle.Default
                textStyle.color: item.msg.deleted ? ui.palette.secondaryTextOnPlain : ui.palette.textOnPlain
            }
        }
        // The time and our message's state, as MessageItem's.
        Container {
            verticalAlignment: VerticalAlignment.Top
            leftMargin: ui.du(1)
            topPadding: item.msg.sender != "" || item.msg.quote != "" ? ui.px(7 * item.view.textScale) : ui.px(12 * item.view.textScale)
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            Label {
                verticalAlignment: VerticalAlignment.Center
                text: (item.msg.edited ? qsTr("edited") + " " : "") + item.msg.time
                textFormat: TextFormat.Plain
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: Math.round(100 * item.view.textScale)
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
    ControlDelegate {
        visible: delegateActive
        delegateActive: (item.msg.reactionCount || 0) > 0
        horizontalAlignment: HorizontalAlignment.Left
        topMargin: ui.du(0.5)
        sourceComponent: ComponentDefinition {
            CardReactions {
                msg: item.msg
                view: item.view
            }
        }
    }
}
