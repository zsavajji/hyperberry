import bb.cascades 1.4

// The chat's own composer row (user's choice, 2026-11-08): Emoji | field | Attach | Send | ⋮. The
// system TextInputActionItem it replaces has no layout control: it grew a block taller than its
// text with the icons on their own row, Emoji couldn't go left, and it takes no text style, so
// emoji past Unicode 6 were boxes while typing. Here the field grows a line at a time (to 5) and
// every child is bottom-aligned, so the icons stay on the last line.
// Enter submits whatever the page makes of it (send, or a new line) and the field keeps the focus,
// so the next message can be typed at once. Send is the touch path to sending (the Q10 has no
// on-screen submit key), and Record while the field is empty, as WhatsApp's: a tap starts, the next
// one sends. The phone's own send and delete icons are read at run time, not shipped.
Container {
    id: bar
    property alias field: field
    // Empty field (nothing staged, nobody recording): Send shows Record.
    property bool mic
    property bool recording
    // A channel (chat.kind == 3): nothing to write, only the menu.
    property bool readOnly
    signal emoji
    signal discard
    signal attach
    signal send
    signal submitted
    // Forwarded: field.text notifies only when committed (as SearchBar's field), this follows the keys.
    signal changing(string text)
    signal menu
    horizontalAlignment: HorizontalAlignment.Fill
    // The stock bar's colours (docs/plan/ui.md).
    background: Color.create(settings.theme == 1 ? "#f0f0f0" : "#282828")
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    // Emoji, or Discard while recording.
    Container {
        verticalAlignment: VerticalAlignment.Bottom
        preferredWidth: ui.du(9)
        minHeight: ui.du(9)
        opacity: bar.readOnly ? 0.4 : 1
        layout: DockLayout {}
        gestureHandlers: TapHandler {
            onTapped: if (!bar.readOnly) bar.recording ? bar.discard() : bar.emoji()
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ui.du(8)
            preferredHeight: ui.du(8)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: bar.recording ? "file:///usr/share/icons/bb_action_delete.png"
                                       : "asset:///images/emoji.png"
            filterColor: ui.palette.textOnPlain
        }
    }
    // The field with Attach and Send inside it, as the stock bar's black box.
    Container {
        layoutProperties: StackLayoutProperties {
            spaceQuota: 1
        }
        verticalAlignment: VerticalAlignment.Fill
        background: Color.create(settings.theme == 1 ? "#ffffff" : "#000000")
        topPadding: ui.du(0.5)
        bottomPadding: ui.du(0.5)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        TextArea {
            id: field
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            verticalAlignment: VerticalAlignment.Center
            hintText: qsTr("Message")
            textFormat: TextFormat.Plain
            // The point of this row: the system composer couldn't take the emoji style.
            textStyle.base: nav.emojiStyle
            backgroundVisible: false
            focusHighlightEnabled: false
            // A tap on Send, Attach or Emoji (not focusable) leaves the focus here, so the next key
            // still goes through the input method (bb10-pitfalls).
            focusRetentionPolicyFlags: FocusRetentionPolicy.LoseToFocusable
            enabled: !bar.readOnly
            autoSize.maxLineCount: 5
            input {
                submitKey: SubmitKey.Send
                submitKeyFocusBehavior: SubmitKeyFocusBehavior.Keep
                onSubmitted: bar.submitted()
            }
            onTextChanging: bar.changing(text)
        }
        Container {
            verticalAlignment: VerticalAlignment.Bottom
            preferredWidth: ui.du(8)
            minHeight: ui.du(8)
            opacity: bar.readOnly ? 0.4 : 1
            layout: DockLayout {}
            gestureHandlers: TapHandler {
                onTapped: if (!bar.readOnly) bar.attach()
            }
            ImageView {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                preferredWidth: ui.du(6)
                preferredHeight: ui.du(6)
                scalingMethod: ScalingMethod.AspectFit
                imageSource: "asset:///images/attach_action.png"
                filterColor: ui.palette.textOnPlain
            }
        }
        Container {
            verticalAlignment: VerticalAlignment.Bottom
            preferredWidth: ui.du(8)
            minHeight: ui.du(8)
            opacity: bar.readOnly ? 0.4 : 1
            layout: DockLayout {}
            gestureHandlers: TapHandler {
                onTapped: if (!bar.readOnly) bar.send()
            }
            ImageView {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                preferredWidth: ui.du(6)
                preferredHeight: ui.du(6)
                scalingMethod: ScalingMethod.AspectFit
                imageSource: bar.mic ? "asset:///images/record.png"
                                     : "file:///usr/share/icons/bb_action_sendtextmessage.png"
                filterColor: ui.palette.textOnPlain
            }
        }
    }
    // The action menu (Search, Pinned, info, ...); always live, a channel needs it too.
    Container {
        verticalAlignment: VerticalAlignment.Bottom
        preferredWidth: ui.du(9)
        minHeight: ui.du(9)
        layout: DockLayout {}
        gestureHandlers: TapHandler {
            onTapped: bar.menu()
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ui.du(6)
            preferredHeight: ui.du(6)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/overflow_action.png"
            filterColor: ui.palette.textOnPlain
        }
    }
}
