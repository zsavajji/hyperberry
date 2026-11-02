import bb.cascades 1.4
import bb.system 1.2
import bb.cascades.pickers 1.0
import hyperberry 1.0

// One chat (PLAN.md §7, step 3.1): a FreeForm title bar with the tile, name and status line, and
// the messages as cards. chat is a ChatListModel row, set right after createObject().
Page {
    id: page
    property variant chat
    // Set by a send until our row goes in at the bottom.
    property bool followOwn: false
    // The emoji picker is open, in the keyboard's place; only the Emoji action closes it.
    property bool picking: false
    // The file picked to send, waiting for Send (step 6.4), and MediaOpener.fileInfo's of it.
    property string staged: ""
    property variant stagedInfo: ({})
    property string failedCmd: engine.failure.cmd || ""
    onFailedCmdChanged: {
        if (failedCmd == "msg.delete") {
            deleteFailed.show()
        } else if (failedCmd == "voice.play") {
            player.fail(engine.failure.code)
        } else if (failedCmd == "media.get") {
            mediaFailed.show()
        }
    }
    onChatChanged: {
        ctl.open(chat)
        if (chat.kind <= 1) {
            addAction(infoAction, ActionBarPlacement.InOverflow)
        }
        ctl.messages.itemAdded.connect(page.added)
        ctl.voiceReady.connect(player.ready)
        media.ready.connect(page.mediaReady)
        media.openFailed.connect(mediaFailed.show)
        ctl.voiceCancelled.connect(recorder.dropped)
    }
    attachedObjects: [
        // Attach (step 6.2): the system's picker, every file type; the engine sends by the extension.
        FilePicker {
            id: attachPicker
            type: FileType.Picture | FileType.Video | FileType.Music | FileType.Document | FileType.Other
            mode: FilePickerMode.Picker
            title: qsTr("Attach")
            onFileSelected: page.stage(selectedFiles[0])
        },
        ChatController {
            id: ctl
            link: engine
        },
        ComponentDefinition {
            id: infoDef
            source: "GroupInfoPage.qml"
        },
        ComponentDefinition {
            id: contactDef
            source: "ContactInfoPage.qml"
        },
        // A received photo, video, audio file or document (step 6.1): MediaOpener fetches the file
        // and hands it to the system; a photo opens in our viewer instead (nav.showPhoto).
        MediaOpener {
            id: media
            link: engine
        },
        // A location (step 6.5c): asks which maps app opens it, every time.
        MapChooser {
            id: maps
        },
        // Added for people and groups only (ActionItem has no visible).
        ActionItem {
            id: infoAction
            title: page.chat && page.chat.kind == 0 ? qsTr("Contact info") : qsTr("Group info")
            imageSource: "asset:///images/contacts_action.png"
            onTriggered: page.showInfo()
        },
        SystemToast {
            id: notSent
            body: qsTr("Not sent: HyperBerry is reconnecting. Try again in a moment.")
        },
        VoicePlayer {
            id: player
            jid: page.chat ? page.chat.jid : ""
            onFailed: voiceFailed.show()
            onOffline: notSent.show()
        },
        Recorder {
            id: recorder
            onFailed: notSent.show()
        },
        SystemToast {
            id: mediaFailed
            body: qsTr("Couldn't open this file")
        },
        SystemToast {
            id: voiceFailed
            body: qsTr("Couldn't play this voice note")
        },
        SystemToast {
            id: deleteFailed
            body: qsTr("Not deleted: HyperBerry is reconnecting. Try again in a moment.")
        },
        // Delete for me: the card is already hidden, the request goes when the toast closes. Several
        // deletes while it shows join one batch (show() on a showing toast keeps it up), and Undo
        // takes them all back.
        SystemToast {
            id: deletedToast
            body: qsTr("Message deleted")
            button.label: qsTr("Undo")
            autoUpdateEnabled: true
            onFinished: {
                if (value == SystemUiResult.ButtonSelection) {
                    ctl.undoDelete()
                } else {
                    ctl.commitDelete()
                }
            }
        },
        SystemDialog {
            id: revokeDialog
            property string msgId
            title: qsTr("Delete for everyone?")
            body: qsTr("It will be deleted for everyone in this chat.")
            confirmButton.label: qsTr("Delete")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection && !ctl.deleteForEveryone(msgId)) {
                    deleteFailed.show()
                }
            }
        }
    ]
    actions: [
        // The system composer (PLAN.md §7, step 3.2; user's choice A): Enter sends and the field keeps
        // the focus, so the next message can be typed at once. It takes no text style, so emoji past
        // Unicode 6 show as boxes until sent.
        TextInputActionItem {
            id: composer
            hintText: qsTr("Message")
            textFormat: TextFormat.Plain
            input {
                submitKey: SubmitKey.Send
                submitKeyFocusBehavior: SubmitKeyFocusBehavior.Keep
                // Off, Enter is a new line: the key still submits, so the field keeps its focus.
                onSubmitted: settings.enterSends ? page.send() : composer.editor.insertPlainText("\n")
            }
            onTextChanging: ctl.composing(text)
        },
        ActionItem {
            title: recorder.recording ? qsTr("Discard") : qsTr("Emoji")
            imageSource: recorder.recording ? "file:///usr/share/icons/bb_action_delete.png"
                                            : "asset:///images/emoji.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: recorder.recording ? recorder.discard() : page.picking = !page.picking
        },
        // The touch path to sending (the Q10 has no on-screen submit key). The phone's own icon, read
        // at run time, not shipped.
        // Record while the composer is empty, as WhatsApp's: a tap starts, the next one sends.
        ActionItem {
            title: page.mic ? qsTr("Record") : qsTr("Send")
            imageSource: page.mic ? "asset:///images/record.png"
                                  : "file:///usr/share/icons/bb_action_sendtextmessage.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: page.mic ? recorder.start() : recorder.recording ? recorder.finish() : page.send()
        },
        ActionItem {
            title: qsTr("Attach")
            imageSource: "asset:///images/attach_action.png"
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: attachPicker.open()
        },
        ActionItem {
            title: qsTr("Search")
            imageSource: "asset:///images/search.png"
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: searchBar.open()
        }
    ]
    keyListeners: KeyListener {
        onKeyPressed: page.typeKey(event)
    }
    // The first open creates the picker (about 1,400 cells' worth of model, none of them controls).
    onPickingChanged: {
        if (!picking) {
            return
        }
        if (!pickerSlot.delegateActive) {
            pickerSlot.delegateActive = true
        }
        emojis.refresh()
    }
    // Inserts at the cursor and keeps the field focused, so the Q10 keyboard still types into it.
    function picked(emoji) {
        composer.editor.insertPlainText(emoji)
        emojis.use(emoji)
        composer.requestFocus()
    }
    property bool mic: !recorder.recording && staged == "" && !/\S/.test(composer.text)
    // A staged file goes instead of a text (step 6.4).
    function send() {
        var text = composer.text.trim()
        if (staged == "" && text == "") {
            return
        }
        // Cleared only once the engine has it: it writes the row before it replies, so nothing typed is lost.
        if (!(staged == "" ? ctl.send(composer.text) : page.sendStaged(text))) {
            notSent.show()
            return
        }
        composer.text = ""
        page.unstage()
        ctl.cancelReply()
        page.followOwn = true
    }
    // A photo's or a video's caption rides in the message; the other kinds' text follows as a message.
    function sendStaged(text) {
        var args = {"chat": chat.jid, "path": staged}
        if (ctl.quoteId()) {
            args.quote = ctl.quoteId()
        }
        // A photo's thumbnail and size come from here: the engine decodes no picture (step 6.3).
        var info = media.photoInfo(staged)
        if (info.width) {
            args.width = info.width
            args.height = info.height
        }
        if (info.thumb) {
            args.thumb = info.thumb
        }
        if (stagedInfo.picture && text) {
            args.caption = text
        }
        if (!engine.request("media.send", args)) {
            return false
        }
        if (!stagedInfo.picture && text) {
            // The file carries the reply; the text after it is plain.
            ctl.cancelReply()
            ctl.send(text)
        }
        return true
    }
    // The picker's file waits in the staged bar; what is typed is its caption.
    function stage(path) {
        var info = media.fileInfo(path)
        if (!info.name) {
            return
        }
        staged = path
        stagedInfo = info
        composer.requestFocus()
    }
    // Drops the file; the composer keeps its text.
    function unstage() {
        staged = ""
        stagedInfo = {}
    }
    // Our own message is followed even when the list was scrolled up. Scrolling once the row is in:
    // a scroll before it, at send, left the new card below the screen, while an incoming one at the
    // bottom was followed by the stick-to-edge policy (Q10, 2026-10-31).
    function added(indexPath) {
        if (followOwn && indexPath[0] == 0) {
            followOwn = false
            list.scrollToPosition(ScrollPosition.Beginning, ScrollAnimation.None)
        }
    }
    // A search match (step 5.10): its day is loaded if it wasn't, then it's scrolled to and tinted.
    function jump(id) {
        list.foundId = id
        var i = ctl.messages.reveal(id)
        if (i >= 0) {
            list.scrollToItem([i], ScrollAnimation.None)
        }
    }
    // The info page of a person or a group. A person's page asks the engine itself, with a fresh
    // row for the mute state; a group's gets the controller, so it reads what the chat page already
    // asked for. The chat page stays under it, so it outlives the info page.
    function showInfo() {
        if (!chat || chat.kind > 1) {
            return
        }
        if (chat.kind == 0) {
            var c = contactDef.createObject()
            var row = chats.chat(chat.jid)
            // No row for a contact picked with no chat yet.
            c.chat = row.jid ? row : page.chat
            nav.push(c)
            return
        }
        var p = infoDef.createObject()
        p.chat = page.chat
        p.ctl = ctl
        nav.push(p)
    }
    // A photo opens in our viewer, anything else in the system's app for its type.
    function showMedia(kind, path) {
        if (kind == "photo") {
            nav.showPhoto(path)
        } else if (!media.openFile(path)) {
            mediaFailed.show()
        }
    }
    function mediaReady(msgId, kind, path) {
        showMedia(kind, path)
    }
    // Called by ChatsPage once the page is pushed: every key, the first included, goes through the
    // input method (auto-capitals, Alt and Shift), which a KeyListener bypasses (bb10-pitfalls).
    function focusComposer() {
        composer.requestFocus()
    }
    // On the Q10, typing anywhere on the page goes to the field (step 3.2). A key reaches the page only
    // when the field doesn't have the focus: it goes in at the end, and the field takes over from the
    // next key. Modifiers, Sym and Delete arrive cut to their low byte with a keycap of 0xE000 or more
    // (bb10-pitfalls); control characters (Enter, Backspace) and Ctrl shortcuts aren't text.
    function typeKey(event) {
        // The search field is inside the page, so its keys come here too (Q10, 0.0.1.122).
        if (searchBar.typing) {
            return
        }
        var c = event.unicode
        if (event.keycap >= 0xE000 || event.ctrlPressed || c.length == 0 || c < " " || c == "\u007f") {
            return
        }
        composer.text = composer.text + c
        composer.requestFocus()
        composer.editor.cursorPosition = composer.text.length
    }
    titleBar: ChatTitleBar {
        chat: page.chat
        status: ctl.status
        onTapped: page.showInfo()
    }
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        layout: StackLayout {
            orientation: LayoutOrientation.TopToBottom
        }
        SearchBar {
            id: searchBar
            jid: page.chat ? page.chat.jid : ""
            onJump: page.jump(id)
            onClosed: list.foundId = ""
        }
        // The list, and over it the note of an empty chat.
        Container {
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            layout: DockLayout {}
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            ListView {
                id: list
                // Filled: a DockLayout's child sits at its top left, at its own size, otherwise.
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                // The cards' long-press menu reaches the page through the list (ListItem.view).
                function reply(msg) {
                    ctl.reply(msg)
                    composer.requestFocus()
                }
                function copy(text) {
                    ctl.copy(text)
                }
                // Reactions (step 5.11): the bar over the composer, and the chip's who-reacted toast.
                function react(msg) {
                    reactBar.open(msg)
                }
                function whoReacted(id) {
                    reactBar.who(id)
                }
                // Polls (step 6.5e): a vote, and the toast of who chose an option, ReactBar's.
                function vote(id, options) {
                    engine.request("poll.vote", {"chat": page.chat.jid, "id": id, "options": options})
                }
                function showVoters(text) {
                    reactBar.say(text)
                }
                // Voice notes: the player's state, for the bubbles (VoicePlayer.qml).
                property string playingId: player.playingId
                // The search match, tinted (SearchBar.qml).
                property string foundId
                property string loadingId: player.loadingId
                property bool paused: player.paused
                property int posMs: player.posMs
                property int durMs: player.durMs
                function playVoice(msg) {
                    player.toggle(msg.id)
                }
                function playedWave(data, bars) {
                    return ctl.playedWave(data, bars)
                }
                // Photos, videos, audio files and documents: the message being fetched, for the bubbles
                // (MediaBubble.qml). Always through media.get: it answers at once when the file is here,
                // and fetches it again when the user moved it out of Downloads. A location and a contact
                // have no file.
                property string mediaLoadingId: media.loadingId
                function openMedia(msg) {
                    if (msg.media == "location") {
                        maps.choose(msg.lat, msg.lon, msg.place)
                    } else if (msg.media == "contact") {
                        media.importContact(msg.vcard)
                    } else {
                        media.get(page.chat.jid, msg.id, msg.media)
                    }
                }
                function deleteForMe(id) {
                    ctl.deleteForMe(id)
                    deletedToast.show()
                }
                function deleteForEveryone(id) {
                    revokeDialog.msgId = id
                    revokeDialog.show()
                }
                // The cards reach the shared emoji style (main.qml) through ListItem.view.
                property variant emojiStyle: nav.emojiStyle
                property int bodySize: nav.bodySize
                property int smallSize: nav.smallSize
                property real textScale: settings.textScale
                dataModel: ctl.messages
                // Bottom to top: the model is newest first, so the list opens at the newest message
                // with no scroll call, and an older page is appended at the visual top without
                // moving the rows on screen.
                layout: StackListLayout {
                    orientation: LayoutOrientation.BottomToTop
                }
                // The newest card keeps the cards' gap above the action bar.
                bottomPadding: ui.du(1.5)
                // The beginning is the bottom: a new message is followed only when the list was
                // already there.
                stickToEdgePolicy: ListViewStickToEdgePolicy.Beginning
                attachedObjects: [
                    ListScrollStateHandler {
                        // Older pages are appends, so asking on every change can't cascade.
                        onFirstVisibleItemChanged: ctl.nearTop(firstVisibleItem)
                        // The list runs bottom to top, so its beginning is the bottom.
                        onAtBeginningChanged: ctl.setAtBottom(atBeginning)
                    }
                ]
                listItemComponents: [
                    ListItemComponent {
                        type: "header"
                        Header {
                            title: ListItemData
                        }
                    },
                    ListItemComponent {
                        type: "msg"
                        MessageItem {
                            msg: ListItemData
                        }
                    },
                    ListItemComponent {
                        type: "service"
                        ServiceItem {
                            msg: ListItemData
                        }
                    }
                ]
            }
            // ctl.messages is null until the page opens the chat, hence the first test.
            EmptyNote {
                visible: !!ctl.messages && ctl.messages.empty
            }
        }
        ReactBar {
            id: reactBar
            jid: page.chat ? page.chat.jid : ""
        }
        ReplyBar {
            name: ctl.replyName
            quote: ctl.replyText
            onCancelled: ctl.cancelReply()
        }
        StagedBar {
            info: page.stagedInfo
            onCancelled: page.unstage()
        }
        // Typing while recording leaves the recording alone: the text stays for later, and Send
        // finishes the recording first.
        RecordingBar {
            visible: recorder.recording
            clock: recorder.clock
        }
        // Created at the first open and kept (delegateActive stays true).
        ControlDelegate {
            id: pickerSlot
            source: "EmojiPicker.qml"
            delegateActive: false
            visible: page.picking
            horizontalAlignment: HorizontalAlignment.Fill
            onControlChanged: {
                if (control) {
                    control.picked.connect(page.picked)
                }
            }
        }
    }
}
