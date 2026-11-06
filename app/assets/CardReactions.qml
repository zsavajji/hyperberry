import bb.cascades 1.4

// A text card's reactions, under it, built only when it has some. As MessageItem's: a tap says who
// reacted.
Container {
    id: chip
    property variant msg
    property variant view
    background: ui.palette.plain
    leftPadding: ui.du(1.5 * chip.view.textScale) + ui.px(2)
    rightPadding: ui.du(1)
    topPadding: ui.du(0.5)
    bottomPadding: ui.du(0.5)
    gestureHandlers: TapHandler {
        onTapped: chip.view.whoReacted(chip.msg.id)
    }
    Label {
        topMargin: 0
        bottomMargin: 0
        text: (chip.msg.reactions || "") + (chip.msg.reactionCount > 1 ? " " + chip.msg.reactionCount : "")
        textFormat: TextFormat.Plain
        textStyle.base: chip.view.emojiStyle
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: chip.view.smallSize
    }
}
