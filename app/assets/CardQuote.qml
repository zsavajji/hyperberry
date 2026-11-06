import bb.cascades 1.4

// The message a text card replies to, on a tint a little off the card's colour, built only when it
// has one. As MessageItem's quote, whose comments say why.
Container {
    id: quote
    property variant msg
    property variant view
    horizontalAlignment: HorizontalAlignment.Fill
    background: Color.create(quote.view.quoteTint)
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    Container {
        verticalAlignment: VerticalAlignment.Fill
        minWidth: ui.du(0.5)
        maxWidth: ui.du(0.5)
        background: ui.palette.primary
    }
    Container {
        leftMargin: ui.du(1)
        rightPadding: ui.du(1)
        topPadding: ui.du(0.5)
        bottomPadding: ui.du(0.5)
        layoutProperties: StackLayoutProperties {
            spaceQuota: 1
        }
        Label {
            visible: quote.msg.quoteName != ""
            bottomMargin: 0
            text: quote.msg.quoteName
            textFormat: TextFormat.Plain
            textStyle.base: quote.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: quote.view.smallSize
            textStyle.fontWeight: FontWeight.Bold
            textStyle.color: quote.msg.quoteColor ? Color.create(quote.msg.quoteColor) : ui.palette.primary
        }
        Label {
            topMargin: 0
            multiline: true
            autoSize.maxLineCount: 2
            text: quote.msg.quote.replace(/\u20E3/g, "")
            textFormat: TextFormat.Plain
            textStyle.base: quote.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: quote.view.smallSize
            textStyle.color: ui.palette.secondaryTextOnPlain
        }
    }
}
