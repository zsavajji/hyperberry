import bb.cascades 1.4
import bb.system 1.2
import hyperberry 1.0

// The story composer (docs/plan/stories.md): a Sheet for posting a story of our own, from StoriesPage. A photo
// or a video picked from the phone gets a caption; a text story is words on a colour, chosen in a bar along the
// bottom as the chat's emoji picker is: an icon (a mosaic of the colours) opens a strip of them, and typing again
// closes it. A bar along the top says who it is shared with (everyone, or the people StoryAudienceSheet chose). Post sends story.post and closes: the story shows up in "My story" at once, and a failure
// comes back as a toast (storyList.postFailed). compose(mode, path) sets it up: mode is "photo", "video" or
// "text", path the file for the first two.
Sheet {
    id: sheet
    property string mode: "text"
    property string path: ""
    // MediaOpener.fileInfo of the file: its name and size.
    property variant info: ({})
    // A photo's picture to look at, as a file: the real one scaled to the screen (fileInfo's is a 96 px thumbnail).
    property string preview: ""
    property int colour: 0
    // WhatsApp's own text-status colours, near enough.
    property variant colours: ["#075E54", "#128C7E", "#6A1B9A", "#C62828", "#1565C0", "#37474F"]
    // The colour strip is open.
    property bool picking: false
    // The chat keys of the people it goes to; none is everyone (the account's status privacy settings).
    property variant audience: []
    property bool valid: sheet.mode == "text" ? story.text.replace(/\s/g, "") != "" : sheet.path != ""

    function compose(mode, path) {
        sheet.mode = mode
        sheet.path = path
        sheet.colour = 0
        sheet.picking = false
        sheet.audience = []
        audienceSheet.reset()
        sheet.info = path ? media.fileInfo(path) : ({})
        sheet.preview = mode == "photo" ? media.composePreview(path) : ""
        caption.text = ""
        story.text = ""
        sheet.open()
    }
    function post() {
        var args = {}
        if (sheet.audience.length > 0) {
            args.audience = sheet.audience
        }
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
    // The strip's cells, made once from the palette.
    onCreationCompleted: {
        for (var i = 0; i < sheet.colours.length; i++) {
            var cell = swatchDef.createObject()
            cell.index = i
            cell.tint = sheet.colours[i]
            cell.tapped.connect(sheet.pick)
            strip.add(cell)
        }
    }
    function pick(i) {
        sheet.colour = i
    }
    attachedObjects: [
        // One colour of the strip: the colour in a frame that is white and thick for the one in use.
        ComponentDefinition {
            id: swatchDef
            Container {
                id: cell
                property int index
                property string tint
                property bool marked: sheet.colour == cell.index
                signal tapped(int index)
                leftMargin: zoom * ui.du(0.7)
                rightMargin: zoom * ui.du(0.7)
                preferredWidth: zoom * ui.du(8)
                preferredHeight: zoom * ui.du(8)
                // A DockLayout, so the colour inside fills the frame: in a StackLayout it got no height and only the frame showed.
                layout: DockLayout {}
                background: cell.marked ? Color.White : Color.create(1.0, 1.0, 1.0, 0.45)
                leftPadding: cell.marked ? zoom * ui.px(5) : ui.px(2)
                topPadding: cell.marked ? zoom * ui.px(5) : ui.px(2)
                rightPadding: cell.marked ? zoom * ui.px(5) : ui.px(2)
                bottomPadding: cell.marked ? zoom * ui.px(5) : ui.px(2)
                gestureHandlers: TapHandler {
                    onTapped: cell.tapped(cell.index)
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    verticalAlignment: VerticalAlignment.Fill
                    background: Color.create(cell.tint)
                }
            }
        },
        StoryAudienceSheet {
            id: audienceSheet
            onChosen: sheet.audience = jids
        },
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
                imageSource: sheet.preview ? "file://" + sheet.preview : ""
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
                    preferredWidth: zoom * ui.du(9)
                    preferredHeight: zoom * ui.du(9)
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
                leftPadding: zoom * ui.du(3)
                rightPadding: zoom * ui.du(3)
                TextArea {
                    id: story
                    hintText: qsTr("Write something")
                    textFormat: TextFormat.Plain
                    backgroundVisible: false
                    textStyle.base: nav.emojiStyle
                    textStyle.color: Color.White
                    textStyle.textAlign: TextAlign.Center
                    textStyle.fontSize: FontSize.XLarge
                    onFocusedChanged: {
                        if (story.focused) {
                            sheet.picking = false
                        }
                    }
                }
            }
            // The colour bar of a text story: the strip over the icon, as the emoji picker is over the composer.
            Container {
                visible: sheet.mode == "text"
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Bottom
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                Container {
                    id: strip
                    visible: sheet.picking
                    horizontalAlignment: HorizontalAlignment.Center
                    topPadding: zoom * ui.du(1.2)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                }
                // The icon, in a box 10 du high that is the touch target: a mosaic of the palette in a frame that
                // takes the primary colour while the strip is open.
                Container {
                    leftPadding: zoom * ui.du(1)
                    layout: DockLayout {}
                    preferredHeight: zoom * ui.du(10)
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        background: sheet.picking ? ui.palette.primary : Color.White
                        leftPadding: zoom * ui.px(3)
                        topPadding: zoom * ui.px(3)
                        rightPadding: zoom * ui.px(3)
                        bottomPadding: zoom * ui.px(3)
                        Container {
                            layout: StackLayout {
                                orientation: LayoutOrientation.LeftToRight
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[0])
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[1])
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[2])
                            }
                        }
                        Container {
                            layout: StackLayout {
                                orientation: LayoutOrientation.LeftToRight
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[3])
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[4])
                            }
                            Container {
                                preferredWidth: zoom * ui.du(2.2)
                                preferredHeight: zoom * ui.du(2.2)
                                background: Color.create(sheet.colours[5])
                            }
                        }
                    }
                    gestureHandlers: TapHandler {
                        onTapped: {
                            sheet.picking = !sheet.picking
                            story.loseFocus()
                        }
                    }
                }
            }
            // The caption of a photo or a video, along the bottom.
            Container {
                visible: sheet.mode != "text"
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Bottom
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                leftPadding: zoom * ui.du(1)
                rightPadding: zoom * ui.du(1)
                topPadding: zoom * ui.du(1)
                bottomPadding: zoom * ui.du(1)
                TextField {
                    id: caption
                    hintText: qsTr("Add a caption")
                    input.submitKey: SubmitKey.Done
                }
            }
            // Who sees it: a bar along the top that opens the audience sheet.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                minHeight: zoom * ui.du(8)
                leftPadding: zoom * ui.du(1.5)
                rightPadding: zoom * ui.du(1.5)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                gestureHandlers: TapHandler {
                    onTapped: audienceSheet.open()
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    text: qsTr("Share with")
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.create("#CCCCCC")
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    leftMargin: zoom * ui.du(1)
                    text: sheet.audience.length == 0 ? qsTr("Everyone") : qsTr("%1 chosen").arg(sheet.audience.length)
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.White
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    text: "\u203A"
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.White
                    textStyle.fontSize: FontSize.XLarge
                }
            }
        }
    }
}
