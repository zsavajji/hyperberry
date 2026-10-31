import bb.cascades 1.4

// The chat list (PLAN.md §7, step 2.4): chats.db's chats under date headers, drawn by
// ChatListModel (the context property chats) before the engine answers.
Page {
    titleBar: TitleBar {
        title: engine.connected && engine.conn == "ready" ? qsTr("Chats") : qsTr("Connecting…")
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            // The rows reach the shared emoji style (main.qml) through ListItem.view.
            property variant emojiStyle: nav.emojiStyle
            dataModel: chats
            listItemComponents: [
                ListItemComponent {
                    type: "header"
                    Header {
                        title: ListItemData
                    }
                },
                ListItemComponent {
                    type: "item"
                    ChatRow {
                        chat: ListItemData
                    }
                }
            ]
        }
        // The engine doesn't say when a history sync starts, so an empty list says what's
        // coming rather than spinning.
        Label {
            visible: chats.empty
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            multiline: true
            textStyle.textAlign: TextAlign.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("No chats yet.\nYour phone sends your recent chats shortly after linking.")
        }
    }
}
