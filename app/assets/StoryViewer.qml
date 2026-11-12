import bb.cascades 1.4
import bb.multimedia 1.4
import bb.system 1.2
import hyperberry 1.0

// The story viewer (docs/plan/stories.md): a Sheet, full screen on black, that slides in from the right over
// the tab, as Android's does. Top to bottom: one progress segment per story of the poster, then Back, their
// picture, name and the story's time; the story fills the rest. A tap on the left third goes back one story, a
// tap anywhere else forward; after the last the sheet closes. A photo or text story lasts 5 s, a video its own
// length. Showing a story marks it seen and sends the read receipt (storyList.markSeen). A story whose file isn't
// here is fetched (storyList.fetch) while its thumbnail shows; the next video is fetched ahead.
// The video plays through MediaPlayer into a ForeignWindowControl, which sits under Cascades' hole, so the
// segments and the header draw over it (the Q10 video gate, 2026-11-12; sound and taps on controls over it
// worked). Start it with createObject() and start(row), row being a StoriesModel row; it destroys itself on close.
Sheet {
    id: sheet
    // The tile's row: poster (the key), name, initials, color, photo.
    property variant poster: ({})
    // The poster's stories (StoriesModel.storyOf's maps); replaced whole when one changes.
    property variant items: []
    property int index: -1
    property variant cur: sheet.index >= 0 && sheet.index < sheet.items.length ? sheet.items[sheet.index] : ({})
    // How far through the shown story, 0 to 1; a photo or text story's is counted by the ticker, a video's is the player's.
    property real progress: 0
    // The segments' Containers, one per story. A JS array held in a property: only whole-array writes stick.
    property variant segs: []
    property string marked: ","
    // Held while the user types a reply (step 5): the ticker and a video wait.
    property bool paused: false
    // Seconds a photo or text story is shown.
    property real seconds: 5
    property bool videoShown: sheet.cur.kind == 2 && !!sheet.cur.video

    // The first story not seen, else the first.
    function start(row) {
        sheet.poster = row
        sheet.items = storyList.storiesOf(row.poster)
        var first = 0
        for (var i = sheet.items.length - 1; i >= 0; i--) {
            if (!sheet.items[i].seen) {
                first = i
            }
        }
        var made = []
        for (var j = 0; j < sheet.items.length; j++) {
            var seg = segDef.createObject()
            bar.add(seg)
            made.push(seg)
        }
        sheet.segs = made
        sheet.show(first)
        sheet.open()
    }
    function show(i) {
        if (i >= sheet.items.length) {
            sheet.close()
            return
        }
        i = Math.max(i, 0)
        player.stop()
        sheet.index = i
        sheet.progress = 0
        for (var j = 0; j < sheet.segs.length; j++) {
            sheet.segs[j].fill = j < i ? 1 : 0
        }
        var s = sheet.items[i]
        if (sheet.marked.indexOf("," + s.id + ",") < 0) {
            sheet.marked += s.id + ","
            if (!s.seen) {
                storyList.markSeen(sheet.poster.poster, s.id)
            }
        }
        if (!s.ready) {
            storyList.fetch(sheet.poster.poster, s.id)
        } else if (s.kind == 2) {
            sheet.playVideo(s.video)
        }
        var ahead = sheet.items[i + 1]
        if (ahead && ahead.kind == 2 && !ahead.ready) {
            storyList.fetch(sheet.poster.poster, ahead.id)
        }
        ticker.start()
    }
    function next() {
        sheet.show(sheet.index + 1)
    }
    function prev() {
        sheet.show(sheet.index - 1)
    }
    function playVideo(path) {
        player.stop()
        player.sourceUrl = "file://" + path
        var e = player.play()
        if (e != 0) {
            console.log("story video: play() " + e)
        }
    }
    function setProgress(p) {
        sheet.progress = Math.min(p, 1)
        if (sheet.index >= 0 && sheet.index < sheet.segs.length) {
            sheet.segs[sheet.index].fill = sheet.progress
        }
    }
    function tick() {
        if (sheet.paused || sheet.cur.kind == 2 || sheet.index < 0) {
            return
        }
        sheet.setProgress(sheet.progress + 0.1 / sheet.seconds)
        if (sheet.progress >= 1) {
            sheet.next()
        }
    }
    // A fetched file is here: put the story's row back, and play it if it is the one on screen.
    function ready(p, id) {
        if (p != sheet.poster.poster) {
            return
        }
        for (var i = 0; i < sheet.items.length; i++) {
            if (sheet.items[i].id == id) {
                var all = sheet.items
                all[i] = storyList.storyOf(p, id)
                sheet.items = all
                if (i == sheet.index && all[i].kind == 2 && all[i].video) {
                    sheet.playVideo(all[i].video)
                }
                return
            }
        }
    }
    function failed(p, id) {
        if (p == sheet.poster.poster) {
            loadFailed.show()
        }
    }
    onCreationCompleted: {
        // Not a Connections: Cascades' QML has none. Both live and die with the sheet.
        storyList.storyReady.connect(sheet.ready)
        storyList.storyFailed.connect(sheet.failed)
    }
    onClosed: {
        ticker.stop()
        player.stop()
        sheet.destroy()
    }
    attachedObjects: [
        Timer {
            id: ticker
            interval: 100
            onTimeout: sheet.tick()
        },
        MediaPlayer {
            id: player
            videoOutput: VideoOutput.PrimaryDisplay
            windowId: surface.windowId
            windowGroupId: surface.windowGroup
            onPositionChanged: {
                if (sheet.cur.kind == 2 && player.duration > 0 && !sheet.paused) {
                    sheet.setProgress(player.position / player.duration)
                }
            }
            onPlaybackCompleted: sheet.next()
            onError: console.log("story video: error " + mediaError + " at " + position)
        },
        SystemToast {
            id: loadFailed
            body: qsTr("Couldn't load the story")
        },
        // One progress segment: a track with a lighter part as long as the story has played, laid out by
        // quotas so no width is ever worked out.
        ComponentDefinition {
            id: segDef
            Container {
                id: seg
                property real fill: 0
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                leftMargin: ui.px(2)
                rightMargin: ui.px(2)
                preferredHeight: ui.px(4)
                background: Color.create(1.0, 1.0, 1.0, 0.35)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    verticalAlignment: VerticalAlignment.Fill
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: Math.max(seg.fill, 0.001)
                    }
                    background: Color.White
                }
                Container {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: Math.max(1 - seg.fill, 0.001)
                    }
                }
            }
        }
    ]
    content: Page {
        Container {
            id: stage
            layout: DockLayout {}
            background: Color.Black
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            attachedObjects: LayoutUpdateHandler {
                id: area
            }
            // A photo, or a video's thumbnail until its file is here. Hidden while the video plays: the video's
            // window is under the Cascades layer, and a picture over its hole would cover it.
            ImageView {
                visible: sheet.cur.kind == 1 || (sheet.cur.kind == 2 && !sheet.videoShown)
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                imageSource: sheet.cur.pic ? "file://" + sheet.cur.pic : ""
                scalingMethod: ScalingMethod.AspectFit
                loadEffect: ImageViewLoadEffect.None
            }
            // A text story: its colours, its words.
            Container {
                visible: sheet.cur.kind == 0
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                background: Color.create(sheet.cur.bg ? sheet.cur.bg : "#000000")
                layout: DockLayout {}
                leftPadding: ui.du(3)
                rightPadding: ui.du(3)
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Center
                    multiline: true
                    text: sheet.cur.text
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.textAlign: TextAlign.Center
                    textStyle.color: Color.create(sheet.cur.fg ? sheet.cur.fg : "#FFFFFF")
                    textStyle.fontSize: FontSize.XLarge
                }
            }
            // The video, fitted to the screen at its own shape (9:16 when the row doesn't say).
            ForeignWindowControl {
                id: surface
                windowId: "storyVideo"
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                property real vw: sheet.cur.width > 0 && sheet.cur.height > 0 ? sheet.cur.width : 9
                property real vh: sheet.cur.width > 0 && sheet.cur.height > 0 ? sheet.cur.height : 16
                property real fit: Math.min(area.layoutFrame.width / vw, area.layoutFrame.height / vh)
                preferredWidth: vw * fit
                preferredHeight: vh * fit
                visible: boundToWindow && sheet.videoShown
                updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
            }
            // Waiting for the file.
            ActivityIndicator {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                running: sheet.cur.kind != 0 && !sheet.cur.ready
                visible: running
            }
            // Taps: the left third is back, the rest forward. Under the header, which takes its own.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                gestureHandlers: TapHandler {
                    onTapped: {
                        if (event.x < area.layoutFrame.width / 3) {
                            sheet.prev()
                        } else {
                            sheet.next()
                        }
                    }
                }
            }
            // The segments, and under them Back, the poster and the time, over a band so white reads on any picture.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                background: Color.create(0.0, 0.0, 0.0, 0.45)
                topPadding: ui.du(0.6)
                Container {
                    id: bar
                    horizontalAlignment: HorizontalAlignment.Fill
                    leftPadding: ui.du(0.5)
                    rightPadding: ui.du(0.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    // Back: a box 10 du square, the touch target, with the stock arrow in it.
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(10)
                        preferredHeight: ui.du(10)
                        layout: DockLayout {}
                        ImageView {
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            preferredWidth: ui.du(5)
                            preferredHeight: ui.du(5)
                            imageSource: "asset:///images/back_action.png"
                            filterColor: Color.White
                        }
                        gestureHandlers: TapHandler {
                            onTapped: sheet.close()
                        }
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(6)
                        preferredHeight: ui.du(6)
                        background: Color.create(sheet.poster.color ? sheet.poster.color : "#087099")
                        layout: DockLayout {}
                        Label {
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            text: sheet.poster.initials
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 100
                            textStyle.color: Color.White
                        }
                        ImageView {
                            visible: !!sheet.poster.photo
                            horizontalAlignment: HorizontalAlignment.Fill
                            verticalAlignment: VerticalAlignment.Fill
                            imageSource: sheet.poster.photo ? "file://" + sheet.poster.photo : ""
                            scalingMethod: ScalingMethod.AspectFill
                            loadEffect: ImageViewLoadEffect.None
                        }
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        leftMargin: ui.du(1.2)
                        Label {
                            text: sheet.poster.name
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                            textStyle.color: Color.White
                            bottomMargin: 0
                        }
                        Label {
                            text: sheet.cur.time ? sheet.cur.time : ""
                            textFormat: TextFormat.Plain
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.smallSize
                            textStyle.color: Color.create("#CCCCCC")
                            topMargin: 0
                        }
                    }
                }
            }
        }
    }
}
