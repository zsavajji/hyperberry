import bb.cascades 1.4

// One row of the Calls tab (step 7.4): the chat's tile, its name, what the call was (red when
// missed) and when. call is a row of CallsModel; the work is the list's (ListItem.view).
Container {
    id: row
    property variant call
    horizontalAlignment: HorizontalAlignment.Fill
    background: row.ListItem.active ? Color.create(row.ListItem.view.pressColor) : Color.Transparent
    contextActions: [
        ActionSet {
            title: row.call.name
            ActionItem {
                title: qsTr("Call")
                imageSource: "file:///usr/share/icons/ic_phone.png"
                onTriggered: row.ListItem.view.callBack(row.call)
            }
            ActionItem {
                title: qsTr("Open chat")
                imageSource: "file:///usr/share/icons/bb_action_sendtextmessage.png"
                onTriggered: row.ListItem.view.openChat(row.call)
            }
        }
    ]
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        minHeight: ui.du(9.4 * row.ListItem.view.textScale)
        leftPadding: ui.du(1.5)
        rightPadding: ui.du(1.5)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        CallTile {
            verticalAlignment: VerticalAlignment.Center
            size: ui.du(7 * row.ListItem.view.textScale)
            color: row.call.color
            photo: row.call.photo
            initials: row.call.initials
            emojiStyle: row.ListItem.view.emojiStyle
            fontPercent: Math.round(118 * row.ListItem.view.textScale)
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
                    text: row.call.name
                    textFormat: TextFormat.Plain
                    textStyle.base: row.ListItem.view.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: row.ListItem.view.bodySize
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    text: row.call.time
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: Math.round(100 * row.ListItem.view.textScale)
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            Label {
                text: row.call.detail
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: row.ListItem.view.smallSize
                textStyle.color: row.call.missed ? Color.create("#FF3333") : ui.palette.secondaryTextOnPlain
            }
        }
    }
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        preferredHeight: ui.px(2)
        background: ui.palette.plain
    }
}
