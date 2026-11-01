import bb.cascades 1.4
import bb.system 1.2
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
        // and hands it to the system; a photo opens in our viewer instead.
        MediaOpener {
            id: media
            link: engine
        },
        ComponentDefinition {
            id: viewerDef
            source: "PhotoViewer.qml"
        },
        // Added for people and groups only (ActionItem has no visible).
        ActionItem {
            id: infoAction
            title: page.chat && page.chat.kind == 0 ? qsTr("Contact info") : qsTr("Group info")
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
        }
    ]
    keyListeners: KeyListener {
        onKeyPressed: page.typeKey(event)
    }
    // The first open creates the picker (about 1,400 cells' worth of model, none of them controls);
    // the time is logged for the 300 ms check (step 4.2).
    onPickingChanged: {
        if (!picking) {
            return
        }
        if (!pickerSlot.delegateActive) {
            var t = Date.now()
            pickerSlot.delegateActive = true
            console.log("emoji picker: created in " + (Date.now() - t) + " ms")
        }
        emojis.refresh()
    }
    // Inserts at the cursor and keeps the field focused, so the Q10 keyboard still types into it.
    function picked(emoji) {
        composer.editor.insertPlainText(emoji)
        emojis.use(emoji)
        composer.requestFocus()
    }
    property bool mic: !recorder.recording && !/\S/.test(composer.text)
    function send() {
        if (!/\S/.test(composer.text)) {
            return
        }
        // Cleared only once the engine has it: it writes the row before it replies, so nothing typed is lost.
        if (!ctl.send(composer.text)) {
            notSent.show()
            return
        }
        composer.text = ""
        page.followOwn = true
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
            var viewer = viewerDef.createObject()
            viewer.path = path
            nav.push(viewer)
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
        ListView {
            id: list
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            // The cards' long-press menu reaches the page through the list (ListItem.view).
            function reply(msg) {
                ctl.reply(msg)
                composer.requestFocus()
            }
            function copy(text) {
                ctl.copy(text)
            }
            // Voice notes: the player's state, for the bubbles (VoicePlayer.qml).
            property string playingId: player.playingId
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
            // and fetches it again when the user moved it out of Downloads.
            property string mediaLoadingId: media.loadingId
            function openMedia(msg) {
                media.get(page.chat.jid, msg.id, msg.media)
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
                    // A chip, like a button that isn't one (Q10, 2026-10-31). Containers have no
                    // border: the outer one's colour shows 2 px around the inner one. A list item
                    // is as wide as its content, so Center alone left it on the left: the spacers'
                    // spaceQuota stretches the row across the list, as the cards' text column does.
                    Container {
                        id: service
                        horizontalAlignment: HorizontalAlignment.Fill
                        topPadding: ui.du(1.5)
                        bottomPadding: 0
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                        }
                        Container {
                            background: Color.create("#5A5A5A")
                            leftPadding: ui.px(2)
                            rightPadding: ui.px(2)
                            topPadding: ui.px(2)
                            bottomPadding: ui.px(2)
                            Container {
                                background: ui.palette.plain
                                leftPadding: ui.du(2)
                                rightPadding: ui.du(2)
                                topPadding: ui.du(1)
                                bottomPadding: ui.du(1)
                                Label {
                                    horizontalAlignment: HorizontalAlignment.Center
                                    multiline: true
                                    text: ListItemData.text
                                    textFormat: TextFormat.Plain
                                    textStyle.base: service.ListItem.view.emojiStyle
                                    textStyle.fontSize: FontSize.PercentageValue
                                    textStyle.fontSizeValue: service.ListItem.view.smallSize
                                    textStyle.textAlign: TextAlign.Center
                                    textStyle.color: ui.palette.secondaryTextOnPlain
                                }
                            }
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                        }
                    }
                }
            ]
        }
        // The message being replied to, above the action bar. A tap cancels. Drawn as the quote in a
        // card is: a primary-colour bar, the name, one line of the text.
        Container {
            visible: ctl.replyName != ""
            horizontalAlignment: HorizontalAlignment.Fill
            background: ui.palette.plain
            leftPadding: ui.du(1.5)
            rightPadding: ui.du(1.5)
            topPadding: ui.du(1)
            bottomPadding: ui.du(1)
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            gestureHandlers: TapHandler {
                onTapped: ctl.cancelReply()
            }
            Container {
                verticalAlignment: VerticalAlignment.Fill
                preferredWidth: ui.du(0.5)
                background: ui.palette.primary
            }
            Container {
                leftMargin: ui.du(1)
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                Label {
                    bottomMargin: 0
                    text: qsTr("Replying to %1").arg(ctl.replyName)
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.fontWeight: FontWeight.Bold
                    textStyle.color: ui.palette.primary
                }
                Label {
                    topMargin: 0
                    bottomMargin: 0
                    multiline: false
                    // Without U+20E3, a keycap's frame, as MessageItem's body.
                    text: ctl.replyText.replace(/\u20E3/g, "")
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            Label {
                verticalAlignment: VerticalAlignment.Center
                leftMargin: ui.du(1)
                text: "\u2715"
                textFormat: TextFormat.Plain
                textStyle.base: nav.emojiStyle
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
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
