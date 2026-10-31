import bb.cascades 1.4

// The Active Frame (PLAN.md step 5.2). A keyboard phone shows only the top half of its 310 x 211
// px, so the count and the last line sit there. It takes no touch. Created by main.qml, so nav
// and chats are in its scope.
SceneCover {
    content: Container {
        background: Color.Black
        preferredWidth: 310
        preferredHeight: 211
        leftPadding: 16
        rightPadding: 16
        topPadding: 12
        layout: StackLayout {
        }
        Label {
            visible: chats.unreadTotal > 0
            text: chats.unreadTotal
            textStyle.base: SystemDefaults.TextStyles.BodyText
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: 250
            textStyle.color: Color.White
        }
        Label {
            text: chats.unreadTotal > 0 ? qsTr("unread") : qsTr("No new messages")
            textStyle.base: SystemDefaults.TextStyles.SmallText
            textStyle.color: Color.Gray
        }
        // The emoji style, so a preview's emoji draw; it ends in an ellipsis where it's cut.
        Label {
            text: chats.lastLine
            textStyle.base: nav.emojiStyle
            textStyle.color: Color.White
            multiline: false
        }
    }
}
