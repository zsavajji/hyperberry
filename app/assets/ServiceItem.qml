import bb.cascades 1.4

// A service line in a chat ("service" rows of MessageModel, kind 6; msg has the text), moved out
// of ChatPage.qml to keep it under 500 lines. A chip, like a button that isn't one (Q10,
// 2026-10-31). Containers have no border: the outer one's colour shows 2 px around the inner one.
// A list item is as wide as its content, so Center alone left it on the left: the spacers'
// spaceQuota stretches the row across the list, as the cards' text column does.
Container {
    id: service
    property variant msg
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
                text: service.msg.text
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
