import bb.cascades 1.4

// The Active Frame (PLAN.md step 5.2). The Q10 shows all of its 310 x 211 px, above the system's
// title strip (screenshot, 0.0.1.76): the count at 250 % with the labels' default spacing pushed
// the last line under the strip. Margins are set, since unset ones put about 20 px between
// labels. It takes no touch. Created by main.qml, so nav and chats are in its scope.
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
            textStyle.fontSizeValue: 200
            textStyle.color: Color.White
            topMargin: 0
            bottomMargin: 0
        }
        Label {
            text: chats.unreadTotal > 0 ? qsTr("unread") : qsTr("No new messages")
            textStyle.base: SystemDefaults.TextStyles.SmallText
            textStyle.color: Color.Gray
            topMargin: 0
            bottomMargin: 0
        }
        // The emoji style, so a preview's emoji draw (they do, in a cover: 0.0.1.76). Two lines,
        // ending in an ellipsis where it's cut.
        Label {
            text: chats.lastLine
            textStyle.base: nav.emojiStyle
            textStyle.color: Color.White
            multiline: true
            autoSize.maxLineCount: 2
            topMargin: 6
            bottomMargin: 0
        }
    }
}
