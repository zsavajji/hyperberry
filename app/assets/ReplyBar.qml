import bb.cascades 1.4

// The message being replied to, or being edited, above the action bar (moved out of
// ChatPage.qml for step 6.4). A tap cancels. Drawn as the quote in a card is: a primary-colour
// bar, the name, one line of the text. The Classic's trackpad reaches it (the user, 2026-11-20): FocusPolicy.Key, not
// KeyAndTouch, so a touch leaves the focus (the composer's keyboard) where it is; it is lit as the pressed colour while it
// has the focus, and a click (Enter) cancels.
Container {
    id: bar
    property string name
    property string quote
    property bool editing
    signal cancelled()
    visible: name != ""
    horizontalAlignment: HorizontalAlignment.Fill
    focusPolicy: FocusPolicy.Key
    background: bar.focused ? Color.create(nav.pressColor) : ui.palette.plain
    leftPadding: zoom * ui.du(1.5)
    rightPadding: zoom * ui.du(1.5)
    topPadding: zoom * ui.du(1)
    bottomPadding: zoom * ui.du(1)
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    gestureHandlers: TapHandler {
        onTapped: cancelled()
    }
    keyListeners: KeyListener {
        onKeyReleased: {
            if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                cancelled()
            }
        }
    }
    Container {
        verticalAlignment: VerticalAlignment.Fill
        preferredWidth: zoom * ui.du(0.5)
        background: ui.palette.primary
    }
    Container {
        leftMargin: zoom * ui.du(1)
        layoutProperties: StackLayoutProperties {
            spaceQuota: 1
        }
        Label {
            bottomMargin: 0
            text: editing ? qsTr("Edit message") : qsTr("Replying to %1").arg(name)
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
            // Without U+20E3, a keycap's frame, as the message card's body (MessageCard.cpp).
            text: quote.replace(/⃣/g, "")
            textFormat: TextFormat.Plain
            textStyle.base: nav.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: nav.smallSize
            textStyle.color: ui.palette.secondaryTextOnPlain
        }
    }
    Label {
        verticalAlignment: VerticalAlignment.Center
        leftMargin: zoom * ui.du(1)
        text: "✕"
        textFormat: TextFormat.Plain
        textStyle.base: nav.emojiStyle
        textStyle.color: ui.palette.secondaryTextOnPlain
    }
}
