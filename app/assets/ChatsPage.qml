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
        },
        ComponentDefinition {
            id: archivedDef
            source: "ArchivedPage.qml"
        }
    ]
    actions: [
        ActionItem {
            title: qsTr("New chat")
            // The stock Phone app's text message bubble; /usr/share/icons only has BBM's.
            imageSource: "asset:///images/message.png"
            ActionBar.placement: ActionBarPlacement.Signature
            onTriggered: page.newChat()
        },
        ActionItem {
            title: qsTr("Archived")
            imageSource: "file:///usr/share/icons/bb_action_archive.png"
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: nav.push(archivedDef.createObject())
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
        // Sticky: a bar that scrolls away resizes the list on every frame, and laying out rows
        // that hold several of our font's emoji again each frame made scrolling lag (Q10,
        // 2026-10-31).
        scrollBehavior: TitleBarScrollBehavior.Sticky
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ChatList {
            dataModel: chats
            emojiStyle: nav.emojiStyle
            bodySize: nav.bodySize
            smallSize: nav.smallSize
            onChatTapped: page.openChat(chat)
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
