import bb.cascades 1.4
import hyperberry 1.0

// The sticker page (docs/plan/stickers.md, S3), opened from the emoji picker's Stickers button: a grid of four columns,
// Favourites then Recent. The whole of it is StickerGrid, in C++ (StickerPicker holds the data). A tap on a sticker sends it and
// closes the page (the chat does both: picked); a long press shows it large over a dimmed page with Send and Add to or Remove
// from favourites.
Page {
    id: page
    // A tap on a sticker, or Send in the preview: its hash, which sticker.send takes.
    signal picked(string hash)
    titleBar: TitleBar {
        title: qsTr("Stickers")
    }
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        StickerGrid {
            link: engine
            pressColor: nav.pressColor
            onPicked: page.picked(hash)
        }
    }
}
