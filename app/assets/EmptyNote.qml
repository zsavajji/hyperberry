import bb.cascades 1.4

// The note over an empty chat's list (moved out of ChatPage.qml for step 6.5d); the page sets visible.
Label {
    horizontalAlignment: HorizontalAlignment.Center
    verticalAlignment: VerticalAlignment.Center
    multiline: false
    text: qsTr("No messages here")
    textFormat: TextFormat.Plain
    textStyle.base: nav.emojiStyle
    textStyle.fontSize: FontSize.PercentageValue
    textStyle.fontSizeValue: nav.bodySize
    textStyle.color: ui.palette.secondaryTextOnPlain
}
