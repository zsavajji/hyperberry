import bb.cascades 1.4

// A photo, video, audio file, document or sticker in a card (step 6.1). A picture is the message's
// thumbnail, a video with a play mark and its length; a file is a row of an icon, its name and its
// size. A tap opens it. view is the chat page's list, which holds the fetch (mediaLoadingId) and
// opens the file (openMedia). A sticker (step 6.5b) is its picture alone, small, with no caption and
// no tap: the model fetches its file by itself.
Container {
    id: bubble
    property variant msg
    property variant view
    property bool picture: msg.media == "photo" || msg.media == "video"
    property bool sticker: msg.media == "sticker"
    property bool loading: view.mediaLoadingId == msg.id
    // A caption follows a picture, with no margin of its own.
    topMargin: 0
    bottomMargin: msg.text != "" ? ui.du(0.5) : 0
    // Air above and below a picture, so it doesn't touch the card's name line or caption (user).
    topPadding: picture || sticker ? ui.du(1) : 0
    bottomPadding: picture || sticker ? ui.du(1) : 0
    gestureHandlers: TapHandler {
        onTapped: {
            if (!bubble.sticker)
                bubble.view.openMedia(bubble.msg)
        }
    }

    Container {
        visible: bubble.picture
        layout: DockLayout {
        }
        preferredWidth: ui.du(40)
        // A tall photo is cut at 50 du; AspectFill crops it.
        preferredHeight: Math.min(ui.du(50), ui.du(40) * bubble.msg.ratio)
        ImageView {
            visible: bubble.msg.hasThumb
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            image: bubble.msg.thumb
            scalingMethod: ScalingMethod.AspectFill
            loadEffect: ImageViewLoadEffect.None
        }
        // No thumbnail: the card is plain, so the box is the page's colour.
        Container {
            visible: !bubble.msg.hasThumb
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            background: ui.palette.background
            layout: DockLayout {
            }
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                text: bubble.msg.media == "video" ? qsTr("Video") : qsTr("Photo")
                textFormat: TextFormat.Plain
                textStyle.base: bubble.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: bubble.view.bodySize
                textStyle.fontStyle: FontStyle.Italic
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
        }
        Label {
            visible: bubble.msg.media == "video"
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            text: "▶"
            textFormat: TextFormat.Plain
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: 300
            textStyle.color: Color.White
        }
        Label {
            visible: bubble.msg.media == "video" && bubble.msg.durationText != ""
            horizontalAlignment: HorizontalAlignment.Right
            verticalAlignment: VerticalAlignment.Bottom
            text: bubble.msg.durationText
            textFormat: TextFormat.Plain
            textStyle.base: SystemDefaults.TextStyles.SmallText
            textStyle.color: Color.White
        }
        ActivityIndicator {
            visible: bubble.loading
            running: bubble.loading
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
        }
    }

    // The sticker's picture, fit in a square; always an Image in thumb, an empty one without a picture,
    // as for a photo, so a recycled card never keeps the previous row's.
    ImageView {
        visible: bubble.sticker && bubble.msg.hasThumb
        topMargin: 0
        preferredWidth: ui.du(20)
        preferredHeight: ui.du(20)
        image: bubble.msg.thumb
        scalingMethod: ScalingMethod.AspectFit
        loadEffect: ImageViewLoadEffect.None
    }
    // Until it is here: no picture and no thumbnail.
    Label {
        visible: bubble.sticker && !bubble.msg.hasThumb
        topMargin: 0
        bottomMargin: 0
        text: qsTr("Sticker")
        textFormat: TextFormat.Plain
        textStyle.base: bubble.view.emojiStyle
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: bubble.view.bodySize
        textStyle.fontStyle: FontStyle.Italic
        textStyle.color: ui.palette.secondaryTextOnPlain
    }

    Container {
        visible: !bubble.picture && !bubble.sticker
        // Fills the card, so a long name is cut by the column and the spinner is at the right.
        horizontalAlignment: HorizontalAlignment.Fill
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        Label {
            verticalAlignment: VerticalAlignment.Center
            text: bubble.msg.media == "audio" ? "🎵" : "📄"
            textFormat: TextFormat.Plain
            textStyle.base: bubble.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: 200
        }
        Container {
            verticalAlignment: VerticalAlignment.Center
            leftMargin: ui.du(1)
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            Label {
                bottomMargin: 0
                multiline: false
                text: bubble.msg.media == "audio" ? qsTr("Audio file") : (bubble.msg.fileName || qsTr("Document"))
                textFormat: TextFormat.Plain
                textStyle.base: bubble.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: bubble.view.bodySize
            }
            // The size, and the length of an audio file when it is known.
            Label {
                topMargin: 0
                bottomMargin: 0
                multiline: false
                text: [bubble.msg.sizeText, bubble.msg.durationText].filter(function (s) {
                    return s != ""
                }).join(" · ")
                textFormat: TextFormat.Plain
                textStyle.base: bubble.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: bubble.view.smallSize
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
        }
        ActivityIndicator {
            visible: bubble.loading
            running: bubble.loading
            verticalAlignment: VerticalAlignment.Center
            leftMargin: ui.du(1)
        }
    }
}
