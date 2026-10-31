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
    onChatChanged: {
        ctl.open(chat)
        ctl.messages.itemAdded.connect(page.added)
    }
    attachedObjects: [
        ChatController {
            id: ctl
            link: engine
        },
        SystemToast {
            id: notSent
            body: qsTr("Not sent: HyperBerry is reconnecting. Try again in a moment.")
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
                onSubmitted: page.send()
            }
            onTextChanging: ctl.composing(text)
        },
        // The touch path to sending (the Q10 has no on-screen submit key). The phone's own icon, read
        // at run time, not shipped.
        ActionItem {
            title: qsTr("Send")
            imageSource: "file:///usr/share/icons/bb_action_sendtextmessage.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: page.send()
        }
    ]
    keyListeners: KeyListener {
        onKeyPressed: page.typeKey(event)
    }
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
    titleBar: TitleBar {
        kind: TitleBarKind.FreeForm
        // The status line must stay; the default let the bar scroll away with the list (Q10).
        scrollBehavior: TitleBarScrollBehavior.Sticky
        // Sized as the stock Text Messages conversation (Q10, 2026-10-31): an 85 px bar, the
        // picture 81 px square flush at the right, 2 px from the bar's edges and 7 px from the
        // screen's.
        kindProperties: FreeFormTitleBarKindProperties {
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                leftPadding: ui.du(1.5)
                rightPadding: ui.px(7)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    // Two lines fit 85 px only without the labels' default margins (about 20 px),
                    // and below the stock title's 25 px capitals (125 %; 100 % draws 20 px):
                    // the name 22 px (110 %), the status 15 px (75 %). Q10, 2026-10-31.
                    Label {
                        bottomMargin: 0
                        text: page.chat ? page.chat.name : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 110
                    }
                    Label {
                        visible: ctl.status != ""
                        topMargin: 0
                        text: ctl.status
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 75
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
                // The initials tile where the stock app has the picture; photos come in Phase 6.
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: ui.du(1.5)
                    preferredWidth: ui.px(81)
                    preferredHeight: ui.px(81)
                    background: page.chat ? Color.create(page.chat.color) : Color.Transparent
                    layout: DockLayout {}
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        text: page.chat ? page.chat.initials : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 150
                        textStyle.color: Color.White
                    }
                }
            }
        }
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            id: list
            // The cards reach the shared emoji style (main.qml) through ListItem.view.
            property variant emojiStyle: nav.emojiStyle
            property int bodySize: nav.bodySize
            property int smallSize: nav.smallSize
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
    }
}
