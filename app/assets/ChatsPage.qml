import bb.cascades 1.4

// The chat list (PLAN.md §7, step 2.4): chats.db's chats under date headers, drawn by
// ChatListModel (the context property chats) before the engine answers.
Page {
    id: page
    // The new chat page while it's open, so picking can remove it from the stack.
    property variant picker
    attachedObjects: [
        ComponentDefinition {
            id: chatDef
            source: "ChatPage.qml"
        },
        ComponentDefinition {
            id: newChatDef
            source: "NewChatPage.qml"
        }
    ]
    actions: [
        ActionItem {
            title: qsTr("New chat")
            // The stock Phone app's text message bubble; /usr/share/icons only has BBM's.
            imageSource: "asset:///images/message.png"
            ActionBar.placement: ActionBarPlacement.Signature
            onTriggered: page.newChat()
        }
    ]
    function openChat(chat) {
        var chatPage = chatDef.createObject()
        chatPage.chat = chat
        nav.push(chatPage)
        chatPage.focusComposer()
    }
    function newChat() {
        picker = newChatDef.createObject()
        picker.picked.connect(page.chatPicked)
        nav.push(picker)
        picker.focusSearch()
    }
    function chatPicked(chat) {
        openChat(chat)
        // The picker goes out from under the chat, so Back returns to the list, as in WhatsApp.
        // destroy() is deferred, so destroying the page whose signal is running is safe;
        // main.qml's show() removes and destroys pages the same way.
        var old = picker
        picker = null
        nav.remove(old)
        old.destroy()
    }
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
            property int bodySize: nav.bodySize
            property int smallSize: nav.smallSize
            dataModel: chats
            onTriggered: {
                if (indexPath.length < 2) {
                    return
                }
                page.openChat(dataModel.data(indexPath))
            }
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
