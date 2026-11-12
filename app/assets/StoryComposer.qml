import bb.cascades 1.4
import bb.system 1.2
import hyperberry 1.0

// The story composer (docs/plan/stories.md): a Sheet for posting a story of our own, from StoriesPage. A photo
// or a video picked from the phone gets a caption; a text story is words on a colour, the colour changed by a
// tap on the swatch. Post sends story.post and closes: the story shows up in "My story" at once, and a failure
// comes back as a toast (storyList.postFailed). compose(mode, path) sets it up: mode is "photo", "video" or
// "text", path the file for the first two.
Sheet {
    id: sheet
    property string mode: "text"
    property string path: ""
    // MediaOpener.fileInfo of the file: its name and size, and a photo's picture.
    property variant info: ({})
    property int colour: 0
    // WhatsApp's own text-status colours, near enough.
    property variant colours: ["#075E54", "#128C7E", "#6A1B9A", "#C62828", "#1565C0", "#37474F"]
    property bool valid: sheet.mode == "text" ? story.text.replace(/\s/g, "") != "" : sheet.path != ""

    function compose(mode, path) {
        sheet.mode = mode
        sheet.path = path
        sheet.colour = 0
        sheet.info = path ? media.fileInfo(path) : ({})
        caption.text = ""
        story.text = ""
        sheet.open()
    }
    function post() {
        var args = {}
        if (sheet.mode == "text") {
            args.text = story.text.substring(0, 700) // WhatsApp's limit
            args.bg = sheet.colours[sheet.colour]
            args.fg = "#FFFFFF"
        } else {
            args.path = sheet.path
            if (caption.text.replace(/\s/g, "") != "") {
                args.caption = caption.text
            }
            if (sheet.mode == "video") {
                // Its thumbnail: a play mark on grey, so the phones that get it show a video and not a grey box.
                var poster = media.videoPoster()
                if (poster) {
                    args.thumb = poster
                }
            }
            if (sheet.mode == "photo") {
                // The engine decodes no picture: the thumbnail and the size come from here, as for a photo in a chat.
                var photo = media.photoInfo(sheet.path)
                if (photo.width) {
                    args.width = photo.width
                    args.height = photo.height
                }
                if (photo.thumb) {
                    args.thumb = photo.thumb
                }
            }
        }
        if (!engine.request("story.post", args)) {
            notPosted.show()
            return
        }
        sheet.close()
    }
    attachedObjects: [
        MediaOpener {
            id: media
            link: engine
        },
        SystemToast {
            id: notPosted
            body: qsTr("Couldn't post the story")
        }
    ]
    content: Page {
        titleBar: TitleBar {
            title: qsTr("New story")
            dismissAction: ActionItem {
                title: qsTr("Cancel")
                onTriggered: sheet.close()
            }
            acceptAction: ActionItem {
                title: qsTr("Post")
                enabled: sheet.valid
                onTriggered: sheet.post()
            }
        }
        Container {
            layout: DockLayout {}
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            background: sheet.mode == "text" ? Color.create(sheet.colours[sheet.colour]) : Color.Black
            // A photo, as the engine will send it.
            ImageView {
                visible: sheet.mode == "photo"
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                image: sheet.info.image
                scalingMethod: ScalingMethod.AspectFit
                loadEffect: ImageViewLoadEffect.None
            }
            // A video: no picture of it to show here, only which file it is.
            Container {
                visible: sheet.mode == "video"
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                ImageView {
                    horizontalAlignment: HorizontalAlignment.Center
                    preferredWidth: ui.du(9)
                    preferredHeight: ui.du(9)
                    imageSource: "asset:///images/play.png"
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    text: sheet.info.name ? sheet.info.name : ""
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.White
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    text: sheet.info.sizeText ? sheet.info.sizeText : ""
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.create("#CCCCCC")
                }
            }
            // A text story: the words in the middle on the colour, the swatch (the next colour) at the bottom right.
            Container {
                visible: sheet.mode == "text"
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Center
                leftPadding: ui.du(3)
                rightPadding: ui.du(3)
                TextArea {
                    id: story
                    hintText: qsTr("Write something")
                    textFormat: TextFormat.Plain
                    backgroundVisible: false
                    textStyle.base: nav.emojiStyle
                    textStyle.color: Color.White
                    textStyle.textAlign: TextAlign.Center
                    textStyle.fontSize: FontSize.XLarge
                }
            }
            Container {
                visible: sheet.mode == "text"
                horizontalAlignment: HorizontalAlignment.Right
                verticalAlignment: VerticalAlignment.Bottom
                rightMargin: ui.du(2)
                bottomMargin: ui.du(2)
                preferredWidth: ui.du(7)
                preferredHeight: ui.du(7)
                background: Color.create(sheet.colours[(sheet.colour + 1) % sheet.colours.length])
                gestureHandlers: TapHandler {
                    onTapped: sheet.colour = (sheet.colour + 1) % sheet.colours.length
                }
            }
            // The caption of a photo or a video, along the bottom.
            Container {
                visible: sheet.mode != "text"
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Bottom
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                leftPadding: ui.du(1)
                rightPadding: ui.du(1)
                topPadding: ui.du(1)
                bottomPadding: ui.du(1)
                TextField {
                    id: caption
                    hintText: qsTr("Add a caption")
                    input.submitKey: SubmitKey.Done
                }
            }
        }
    }
}
