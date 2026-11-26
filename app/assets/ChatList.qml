import bb.cascades 1.4
import bb.system 1.2
import hyperberry 1.0

// The rows of a chat list, for ChatsPage and ArchivedPage: the model is set by the page, which
// takes chatTapped. The rows are C++ (ChatRows.cpp): they reach the work through the list's
// functions and properties, and the long-press menu calls the functions.
ListView {
    id: list
    // The Passport's keyboard scrolls the list (KeyboardScroll), which takes the focus for it (ChatsPage's refocus; the
    // user, 2026-11-25: it did nothing), and on a touch too: it scrolled once, then stopped, the focus gone elsewhere. The
    // Classic's trackpad steps through the rows as before.
    focusPolicy: screenWidth >= 1440 ? FocusPolicy.KeyAndTouch : FocusPolicy.Default
    // A touch on the list still lost it the focus (the user, 2026-11-25): set by the page while it is the top one, the
    // list takes it back a moment after a touch ends. Not once a chat opened over it: its composer has the focus then.
    property bool refocusOnTouch: false
    onTouch: {
        if (refocusOnTouch && (event.isUp() || event.isCancel())) {
            refocusTimer.start()
        }
    }
    eventHandlers: KeyboardScroll {
        list: list
    }
    // The shared emoji style and sizes (main.qml), set by the page.
    property variant emojiStyle
    property int bodySize
    property int smallSize
    property real textScale: settings.textScale
    property bool compact: settings.compact
    // Bright\'s darker text (CardStyle.h).
    property bool bright: settings.theme == 1
    property string pressColor: nav.pressColor
    // True on the Archived page, where "Archive" reads "Unarchive".
    property bool archivedList: false
    // True on the Channels page: the row's menu has Unfollow for Archive, and no pin or mute.
    property bool channelList: false
    // The rows' Call reads it here: a list item can't see the engine context property (0.0.1.191).
    property bool linked: engine.linked
    signal chatTapped(variant chat)

    function set(jid, args) {
        args.chat = jid
        engine.request("chat.set", args)
    }
    // A failed call.start is worded by main.qml's toast, whoever sent it.
    function call(chat) {
        engine.request("call.start", {"chat": chat.jid})
    }
    function toggleUnread(chat) {
        set(chat.jid, {"unread": chat.unread == 0})
    }
    function togglePin(chat) {
        set(chat.jid, {"pin": !chat.pinned})
    }
    function toggleArchive(chat) {
        if (channelList) {
            unfollowDialog.jid = chat.jid
            unfollowDialog.body = chat.name
            unfollowDialog.show()
            return
        }
        set(chat.jid, {"archive": !archivedList})
    }
    function toggleMute(chat) {
        if (chat.muted) {
            set(chat.jid, {"mute": 0})
        } else {
            muteDialog.jid = chat.jid
            // Filled here: a SystemListDialog isn't a Cascades control and has no
            // onCreationCompleted; assigning one failed the whole page (0.0.1.76).
            if (!muteDialog.filled) {
                muteDialog.appendItem(qsTr("8 hours"), true, true)
                muteDialog.appendItem(qsTr("1 week"))
                muteDialog.appendItem(qsTr("Always"))
                muteDialog.filled = true
            }
            muteDialog.show()
        }
    }
    function deleteChat(chat) {
        deleteDialog.jid = chat.jid
        deleteDialog.body = chat.name
        deleteDialog.show()
    }
    function pickTranslate(chat) {
        translateDialog.pick(chat.jid, chat.translate)
    }
    attachedObjects: [
        Timer {
            id: refocusTimer
            singleShot: true
            interval: 100
            onTimeout: {
                if (list.refocusOnTouch) {
                    list.requestFocus()
                }
            }
        },
        TranslatePicker {
            id: translateDialog
        },
        SystemDialog {
            id: unfollowDialog
            property string jid
            title: qsTr("Unfollow this channel?")
            confirmButton.label: qsTr("Unfollow")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    engine.request("channel.unfollow", {"chat": jid})
                }
            }
        },
        // As the contact page's Delete chat: gone here, and on the main phone (chat.delete).
        SystemDialog {
            id: deleteDialog
            property string jid
            title: qsTr("Delete this chat?")
            confirmButton.label: qsTr("Delete")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    engine.request("chat.delete", {"chat": jid})
                }
            }
        },
        SystemListDialog {
            id: muteDialog
            property string jid
            property bool filled: false
            title: qsTr("Mute for…")
            confirmButton.label: qsTr("Mute")
            cancelButton.label: qsTr("Cancel")
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
    listItemProvider: ChatRows {}
}
