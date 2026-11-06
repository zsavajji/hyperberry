import bb.cascades 1.4

// One row of the chat list (PLAN.md §7): at least 9.4 du (85 px on the Q10, the stock Text
// Messages row), then a 2 px separator. chat is a row of ChatListModel.
// Every text that can hold emoji takes the list's shared emoji style: a FontFaceRule's font
// reaches only the styles built on it (Q10, 2026-10-31). Its percentages are the system styles'
// sizes as measured on the Q10. 100 % draws capitals 20 px tall, the tile's TitleText 24 px
// (118 %); the name and preview take main.qml's bodySize and smallSize, as the chat's cards do.
Container {
    id: row
    property variant chat
    property bool unread: chat.unread != 0
    property bool hasPhoto: !!chat.photo
    horizontalAlignment: HorizontalAlignment.Fill
    background: row.ListItem.active ? Color.create(row.ListItem.view.pressColor) : Color.Transparent
    // The long-press menu (step 5.3): the work is the list's (ListItem.view). ActionItem has no
    // visible, so each title follows the row's state.
    contextActions: [
        ActionSet {
            title: row.chat.name
            ActionItem {
                title: row.unread ? qsTr("Mark as read") : qsTr("Mark as unread")
                imageSource: row.unread ? "file:///usr/share/icons/bb_action_markread.png" : "file:///usr/share/icons/bb_action_markunread.png"
                onTriggered: row.ListItem.view.toggleUnread(row.chat)
            }
            ActionItem {
                title: row.chat.pinned ? qsTr("Unpin") : qsTr("Pin")
                imageSource: "asset:///images/pin_action.png"
                enabled: !row.ListItem.view.channelList
                onTriggered: row.ListItem.view.togglePin(row.chat)
            }
            ActionItem {
                title: row.chat.muted ? qsTr("Unmute") : qsTr("Mute…")
                imageSource: "asset:///images/mute_action.png"
                enabled: !row.ListItem.view.channelList
                onTriggered: row.ListItem.view.toggleMute(row.chat)
            }
            ActionItem {
                title: row.ListItem.view.channelList ? qsTr("Unfollow") : row.ListItem.view.archivedList ? qsTr("Unarchive") : qsTr("Archive")
                imageSource: row.ListItem.view.channelList ? "file:///usr/share/icons/bb_action_delete.png" : "file:///usr/share/icons/bb_action_archive.png"
                onTriggered: row.ListItem.view.toggleArchive(row.chat)
            }
        }
    ]

    // The row, its tile and the initials grow with the text size, so a row keeps Normal's
    // proportions: at 140 % the two lines filled the 9.4 du row edge to edge (Q10, 2026-11-01).
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        minHeight: ui.du(9.4 * row.ListItem.view.textScale)
        leftPadding: ui.du(1.5)
        rightPadding: ui.du(1.5)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        // The chat's picture (the engine's preview file, chat.photo) over the initials tile. The
        // file's name has the picture's id, so a new picture is a new path and reloads.
        Container {
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ui.du(7 * row.ListItem.view.textScale)
            preferredHeight: ui.du(7 * row.ListItem.view.textScale)
            background: Color.create(row.chat.color)
            layout: DockLayout {}
            ImageView {
                visible: row.hasPhoto
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                imageSource: row.hasPhoto ? "file://" + row.chat.photo : ""
                scalingMethod: ScalingMethod.AspectFill
                loadEffect: ImageViewLoadEffect.None
            }
            Label {
                visible: !row.hasPhoto
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                text: row.chat.initials
                textStyle.base: row.ListItem.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: Math.round(118 * row.ListItem.view.textScale)
                textStyle.color: Color.White
            }
        }
        Container {
            verticalAlignment: VerticalAlignment.Center
            leftMargin: ui.du(1.5)
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            Container {
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    text: row.chat.name
                    textStyle.base: row.ListItem.view.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: row.ListItem.view.bodySize
                }
                ImageView {
                    visible: row.chat.pinned
                    verticalAlignment: VerticalAlignment.Center
                    imageSource: "asset:///images/pin.png"
                    filterColor: ui.palette.secondaryTextOnPlain
                }
                ImageView {
                    visible: row.chat.muted
                    verticalAlignment: VerticalAlignment.Center
                    imageSource: "asset:///images/mute.png"
                    filterColor: ui.palette.secondaryTextOnPlain
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    text: row.chat.time || ""
                    // Grows with the text size, as the name and preview do (MessageItem's time).
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: Math.round(100 * row.ListItem.view.textScale)
                    textStyle.color: row.unread ? ui.palette.primary : ui.palette.secondaryTextOnPlain
                }
            }
            Container {
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                // Our last message's state (docs/protocol.md): sending, sent, delivered, read.
                ImageView {
                    visible: row.chat.out && row.chat.state < 4
                    verticalAlignment: VerticalAlignment.Center
                    rightMargin: 0
                    imageSource: row.chat.state == 0 ? "asset:///images/clock.png" : row.chat.state == 1 ? "asset:///images/tick.png" : "asset:///images/ticks.png"
                    filterColor: row.chat.state == 3 ? ui.palette.primary : ui.palette.secondaryTextOnPlain
                }
                Label {
                    visible: row.chat.out && row.chat.state == 4
                    verticalAlignment: VerticalAlignment.Center
                    text: "!"
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                    textStyle.fontWeight: FontWeight.Bold
                    textStyle.color: Color.create("#FF3333")
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    // Closer to the ticks than Cascades' default gap (about 20 px between labels):
                    // the user's, Q10, 2026-11-02.
                    leftMargin: ui.du(1)
                    // Without U+20E3, a keycap's frame, as MessageItem's body.
                    text: row.chat.preview.replace(/\u20E3/g, "")
                    textStyle.base: row.ListItem.view.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: row.ListItem.view.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                // The unread count, grey while muted, as WhatsApp does.
                Container {
                    visible: row.chat.unread > 0
                    verticalAlignment: VerticalAlignment.Center
                    leftPadding: ui.du(0.8)
                    rightPadding: ui.du(0.8)
                    background: row.chat.muted ? ui.palette.plain : ui.palette.primary
                    Label {
                        text: row.chat.unread
                        textStyle.base: SystemDefaults.TextStyles.SmallText
                        textStyle.fontWeight: FontWeight.Bold
                        textStyle.color: row.chat.muted ? ui.palette.textOnPlain : ui.palette.textOnPrimary
                    }
                }
                // Marked unread (-1): a dot in the badge's place, no number.
                Container {
                    visible: row.chat.unread < 0
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: ui.du(0.8)
                    preferredWidth: ui.du(1.6)
                    preferredHeight: ui.du(1.6)
                    background: row.chat.muted ? ui.palette.plain : ui.palette.primary
                }
            }
        }
    }
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        preferredHeight: ui.px(2)
        background: ui.palette.plain
    }
}
