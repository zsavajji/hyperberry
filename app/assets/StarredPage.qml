import bb.cascades 1.4

// The starred messages (the phone's and ours), newest first, from chats.starred(): the chat's name, who sent it in a
// group, the message's first line and when. A tap opens the chat at that message, and Back comes here. The rows are read
// when the page is made and again whenever it is the top page again, since a chat over it may have starred or unstarred.
// ponytail: a star that arrives while this page is on top shows when it is uncovered next; refill on msgsChanged if that matters.
Page {
    id: page
    property int count: 0
    attachedObjects: [
        ArrayDataModel {
            id: rows
        },
        ComponentDefinition {
            id: chatDef
            source: "ChatPage.qml"
        }
    ]
    titleBar: TitleBar {
        title: qsTr("Starred messages")
        scrollBehavior: TitleBarScrollBehavior.Sticky
    }
    function fill() {
        var list = chats.starred()
        rows.clear()
        if (list.length > 0) {
            rows.append(list)
        }
        page.count = list.length
    }
    // main.qml's nav calls this when the page is the top again.
    function uncovered() {
        page.fill()
    }
    // The chat opens, its messages come a moment later, and the page jumps to the message when they are in (ChatPage.jump).
    function openMessage(row) {
        var chat = chats.chat(row.chat)
        if (!chat.jid) {
            return
        }
        var chatPage = chatDef.createObject()
        chatPage.chat = chat
        nav.push(chatPage)
        chatPage.jump(row.id)
    }
    onCreationCompleted: page.fill()
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
            property variant emojiStyle: nav.emojiStyle
            property int bodySize: nav.bodySize
            property int smallSize: nav.smallSize
            property string pressColor: nav.pressColor
            property variant lineColor: nav.lineColor
            dataModel: rows
            onTriggered: page.openMessage(rows.data(indexPath))
            listItemComponents: ListItemComponent {
                type: ""
                Container {
                    id: row
                    horizontalAlignment: HorizontalAlignment.Fill
                    background: row.ListItem.active ? Color.create(row.ListItem.view.pressColor) : Color.Transparent
                    Container {
                        horizontalAlignment: HorizontalAlignment.Fill
                        leftPadding: zoom * ui.du(1.5)
                        rightPadding: zoom * ui.du(1.5)
                        topPadding: zoom * ui.du(1)
                        bottomPadding: zoom * ui.du(1)
                        // The chat, and in a group who sent it, with the time at the right.
                        Container {
                            horizontalAlignment: HorizontalAlignment.Fill
                            layout: StackLayout {
                                orientation: LayoutOrientation.LeftToRight
                            }
                            Label {
                                verticalAlignment: VerticalAlignment.Center
                                layoutProperties: StackLayoutProperties {
                                    spaceQuota: 1
                                }
                                text: ListItemData.sender ? ListItemData.chatName + " · " + ListItemData.sender : ListItemData.chatName
                                textFormat: TextFormat.Plain
                                textStyle.base: row.ListItem.view.emojiStyle
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: row.ListItem.view.bodySize
                            }
                            Label {
                                verticalAlignment: VerticalAlignment.Center
                                leftMargin: zoom * ui.du(1)
                                text: ListItemData.time
                                textFormat: TextFormat.Plain
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: row.ListItem.view.smallSize
                                textStyle.color: ui.palette.secondaryTextOnPlain
                            }
                        }
                        Label {
                            text: ListItemData.text
                            textFormat: TextFormat.Plain
                            textStyle.base: row.ListItem.view.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: row.ListItem.view.smallSize
                            textStyle.color: ui.palette.secondaryTextOnPlain
                        }
                    }
                    Container {
                        horizontalAlignment: HorizontalAlignment.Fill
                        preferredHeight: ui.px(2)
                        background: row.ListItem.view.lineColor
                    }
                }
            }
        }
        Label {
            visible: page.count == 0
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("No starred messages")
        }
    }
}
