import bb.cascades 1.4

// A person's tile (the chat list's): their picture over the initials on the chat's colour. The
// caller sets size (px), color, photo (a file path or ""), initials, and emojiStyle for the initials.
Container {
    id: tile
    property int size: 72
    property string color: "#779933"
    property string photo: ""
    property string initials: ""
    property variant emojiStyle
    property int fontPercent: 118
    preferredWidth: size
    preferredHeight: size
    background: Color.create(color)
    layout: DockLayout {}
    ImageView {
        visible: tile.photo != ""
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        imageSource: tile.photo != "" ? "file://" + tile.photo : ""
        scalingMethod: ScalingMethod.AspectFill
        loadEffect: ImageViewLoadEffect.None
    }
    Label {
        visible: tile.photo == ""
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Center
        text: tile.initials
        textFormat: TextFormat.Plain
        textStyle.base: tile.emojiStyle
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: tile.fontPercent
        textStyle.color: Color.White
    }
}
