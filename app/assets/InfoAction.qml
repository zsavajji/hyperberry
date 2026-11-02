import bb.cascades 1.4

// ⋮ → Contact info / Group info, and the title bar's tap (step 5.8): the info page of a person or a
// group, out of ChatPage.qml to keep that file short. The page sets the title (its translations
// are the page's). Added to the menu for people and groups only (ActionItem has no visible).
ActionItem {
    id: action
    // The chat page's chat (a ChatListModel row) and ChatController.
    property variant chat
    property variant chatCtl
    imageSource: "asset:///images/contacts_action.png"
    onTriggered: showInfo()
    attachedObjects: [
        ComponentDefinition {
            id: infoDef
            source: "GroupInfoPage.qml"
        },
        ComponentDefinition {
            id: contactDef
            source: "ContactInfoPage.qml"
        }
    ]
    // A person's page asks the engine itself, with a fresh row for the mute state; a group's gets the
    // controller, so it reads what the chat page already asked for. The chat page stays under it, so
    // it outlives the info page.
    function showInfo() {
        if (!chat || chat.kind > 1) {
            return
        }
        if (chat.kind == 0) {
            var c = contactDef.createObject()
            var row = chats.chat(chat.jid)
            // No row for a contact picked with no chat yet.
            c.chat = row.jid ? row : chat
            nav.push(c)
            return
        }
        var p = infoDef.createObject()
        p.chat = chat
        p.ctl = chatCtl
        nav.push(p)
    }
}
