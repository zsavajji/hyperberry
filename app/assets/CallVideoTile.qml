import bb.cascades 1.4

// One tile of a call's video (CallVideoView): the native window CallVideo draws one sender's picture
// into, and over its foot the sender's name. A 1:1 call shows tile 0 alone; a group call has four. The
// window sits under Cascades' hole, so neither this container nor the control may have a background
// (0.0.1.190); only the name's band has one, and it covers just the name. The view places and sizes
// the tile (cell: x, y, width, height in the frame's px) and shows or hides it.
Container {
    id: tile
    // Which tile of CallVideo this is (0 to 3), for the crop.
    property int index: 0
    property alias windowId: control.windowId
    property variant cell: [0, 0, 0, 0]
    // Whose picture it is, "" in a 1:1 call and while their name isn't known: no label then.
    property string name: ""
    signal tapped
    layoutProperties: AbsoluteLayoutProperties {
        positionX: tile.cell[0]
        positionY: tile.cell[1]
    }
    preferredWidth: cell[2]
    preferredHeight: cell[3]
    layout: DockLayout {}
    // The picture fills the tile: the control is the tile's size and CallVideo crops the picture to
    // its aspect (Screen's source viewport), as in a 1:1 call.
    ForeignWindowControl {
        id: control
        updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
        // Shown while the people drawer is out too (264 hid it and the page went black): whether the
        // window follows the page's slide is logged as the drawer settles (CallVideo::logWindows).
        visible: tile.visible
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        attachedObjects: LayoutUpdateHandler {
            onLayoutFrameChanged: {
                if (layoutFrame.width > 0 && layoutFrame.height > 0) {
                    callVideo.setTileViewSize(tile.index, layoutFrame.width, layoutFrame.height)
                }
            }
        }
    }
    // The name, white on a dark band only as wide as it is, at the tile's bottom left.
    Container {
        visible: tile.name != ""
        horizontalAlignment: HorizontalAlignment.Left
        verticalAlignment: VerticalAlignment.Bottom
        maxWidth: tile.cell[2]
        leftPadding: pxZoom * ui.px(8)
        rightPadding: pxZoom * ui.px(8)
        background: Color.create("#99000000")
        Label {
            multiline: false
            text: tile.name
            textFormat: TextFormat.Plain
            textStyle.base: nav.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: nav.smallSize
            textStyle.color: Color.White
        }
    }
    gestureHandlers: TapHandler {
        onTapped: tile.tapped()
    }
}
