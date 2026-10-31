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
        // The count and its word side by side: the count's line alone is about 86 px tall at 200 %,
        // and the word under it left the second message line touching the strip (0.0.1.78). Sizes
        // follow the stock frames' small type (0.0.1.80 looked oversized beside Settings').
        Container {
            topMargin: 0
            bottomMargin: 0
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            Label {
                visible: chats.unreadTotal > 0
                text: chats.unreadTotal
                textStyle.base: SystemDefaults.TextStyles.BodyText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 130
                textStyle.color: Color.White
                topMargin: 0
                bottomMargin: 0
            }
            Label {
                verticalAlignment: VerticalAlignment.Bottom
                leftMargin: 12
                text: chats.unreadTotal > 0 ? qsTr("unread") : qsTr("No new messages")
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 85
                textStyle.color: Color.Gray
                topMargin: 0
                bottomMargin: 0
            }
        }
        // The emoji style, so a preview's emoji draw (they do, in a cover: 0.0.1.76), at the chat
        // list's preview size. Up to three lines, ending in an ellipsis where it's cut.
        Label {
            text: chats.lastLine
            textStyle.base: nav.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: 74
            textStyle.color: Color.White
            multiline: true
            autoSize.maxLineCount: 3
            topMargin: 8
            bottomMargin: 0
        }
    }
}
