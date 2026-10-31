import bb.cascades 1.4
import bb.system 1.2

// The rows of a chat list, for ChatsPage and ArchivedPage: the model is set by the page, which
// takes chatTapped. The rows' long-press menu reaches the work through the list (ListItem.view).
ListView {
    id: list
    // The shared emoji style and sizes (main.qml), set by the page.
    property variant emojiStyle
    property int bodySize
    property int smallSize
    // True on the Archived page, where "Archive" reads "Unarchive".
    property bool archivedList: false
    signal chatTapped(variant chat)

    function set(jid, args) {
        args.chat = jid
        engine.request("chat.set", args)
    }
    function toggleUnread(chat) {
        set(chat.jid, {"unread": chat.unread == 0})
    }
    function togglePin(chat) {
        set(chat.jid, {"pin": !chat.pinned})
    }
    function toggleArchive(chat) {
        set(chat.jid, {"archive": !archivedList})
    }
    function toggleMute(chat) {
        if (chat.muted) {
            set(chat.jid, {"mute": 0})
        } else {
            muteDialog.jid = chat.jid
            muteDialog.show()
        }
    }
    attachedObjects: [
        SystemListDialog {
            id: muteDialog
            property string jid
            title: qsTr("Mute for…")
            confirmButton.label: qsTr("Mute")
            cancelButton.label: qsTr("Cancel")
            onCreationCompleted: {
                appendItem(qsTr("8 hours"), true, true)
                appendItem(qsTr("1 week"))
                appendItem(qsTr("Always"))
            }
            onFinished: {
                if (value != SystemUiResult.ConfirmButtonSelection) {
                    return
                }
                var i = selectedIndices[0]
                var now = Math.floor(new Date().getTime() / 1000)
                list.set(jid, {"mute": i == 0 ? now + 8 * 3600 : i == 1 ? now + 7 * 24 * 3600 : -1})
            }
        }
    ]
    onTriggered: {
        if (indexPath.length < 2) {
            return
        }
        chatTapped(dataModel.data(indexPath))
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
