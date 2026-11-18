import bb.cascades 1.4

// One tile of the Stories grid (StoriesPage), in the look of a stock Active Frame without its bar (the user's
// design, 2026-11-12): the poster's tile story fills it, their picture sits over it top left in a ring that is
// the primary colour while something is unseen, the unseen count top right, the name and the time along the
// bottom over a dark band. story is a row of StoriesModel; the view supplies the emoji style, the sizes and the
// press colour, as the chat list's does. A poster the phone muted is dimmed, as WhatsApp greys their updates.
Container {
    property variant story
    id: tile
    opacity: tile.story.muted ? 0.5 : 1.0
    layout: DockLayout {}
    horizontalAlignment: HorizontalAlignment.Fill
    verticalAlignment: VerticalAlignment.Fill
    // A text story is its own colour; a picture waits on a dark ground until it is drawn.
    background: tile.story.kind == 0 ? Color.create(tile.story.bg) : Color.create("#1A1A1A")
    ImageView {
        visible: !!tile.story.tile
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        imageSource: tile.story.tile ? "file://" + tile.story.tile : ""
        scalingMethod: ScalingMethod.AspectFill
        loadEffect: ImageViewLoadEffect.None
    }
    Label {
        visible: tile.story.kind == 0
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Center
        multiline: true
        text: tile.story.text
        textFormat: TextFormat.Plain
        textStyle.base: tile.ListItem.view.emojiStyle
        textStyle.textAlign: TextAlign.Center
        textStyle.color: Color.create(tile.story.fg)
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: tile.ListItem.view.smallSize
    }
    ImageView {
        visible: tile.story.kind == 2
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Center
        preferredWidth: zoom * ui.du(6)
        preferredHeight: zoom * ui.du(6)
        imageSource: "asset:///images/play.png"
        opacity: 0.85
    }
    // The name and the time, over a band.
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Bottom
        background: Color.create(0.0, 0.0, 0.0, 0.55)
        leftPadding: zoom * ui.du(1)
        rightPadding: zoom * ui.du(1)
        topPadding: zoom * ui.du(0.6)
        bottomPadding: zoom * ui.du(0.6)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        Label {
            verticalAlignment: VerticalAlignment.Center
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            text: tile.story.name
            textFormat: TextFormat.Plain
            textStyle.base: tile.ListItem.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: tile.ListItem.view.smallSize
            textStyle.color: Color.White
        }
        Label {
            verticalAlignment: VerticalAlignment.Center
            leftMargin: zoom * ui.du(1)
            text: tile.story.time
            textFormat: TextFormat.Plain
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: tile.ListItem.view.smallSize
            textStyle.color: Color.create("#CCCCCC")
        }
    }
    // The picture and the count sit in a layer of their own, padded off the tile's edges.
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        layout: DockLayout {}
        leftPadding: zoom * ui.du(0.8)
        topPadding: zoom * ui.du(0.8)
        rightPadding: zoom * ui.du(0.8)
        Container {
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Top
            leftPadding: zoom * ui.px(3)
            topPadding: zoom * ui.px(3)
            rightPadding: zoom * ui.px(3)
            bottomPadding: zoom * ui.px(3)
            background: tile.story.unseen > 0 ? ui.palette.primary : Color.create("#7D7D7D")
            Container {
                preferredWidth: zoom * ui.du(6)
                preferredHeight: zoom * ui.du(6)
                background: Color.create(tile.story.color)
                layout: DockLayout {}
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Center
                    text: tile.story.initials
                    textFormat: TextFormat.Plain
                    textStyle.base: tile.ListItem.view.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: 100 * zoom
                    textStyle.color: Color.White
                }
                ImageView {
                    visible: !!tile.story.photo
                    horizontalAlignment: HorizontalAlignment.Fill
                    verticalAlignment: VerticalAlignment.Fill
                    imageSource: tile.story.photo ? "file://" + tile.story.photo : ""
                    scalingMethod: ScalingMethod.AspectFill
                    loadEffect: ImageViewLoadEffect.None
                }
            }
        }
        Container {
            visible: tile.story.unseen > 0
            horizontalAlignment: HorizontalAlignment.Right
            verticalAlignment: VerticalAlignment.Top
            minWidth: zoom * ui.du(3.4)
            minHeight: zoom * ui.du(3.4)
            leftPadding: zoom * ui.du(0.8)
            rightPadding: zoom * ui.du(0.8)
            background: ui.palette.primary
            layout: DockLayout {}
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                text: tile.story.unseen
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: tile.ListItem.view.smallSize
                textStyle.color: Color.White
            }
        }
    }
    // A pressed tile, as the chat rows give feedback (they step off the page colour; here a veil).
    Container {
        visible: tile.ListItem.active
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        background: Color.create(1.0, 1.0, 1.0, 0.18)
    }
}
