import bb.cascades 1.4
import bb.multimedia 1.4
import bb.system 1.2
import hyperberry 1.0

// The story viewer (docs/plan/stories.md): a Page pushed on the Stories tab's pane, full screen on black, so it
// slides in from the right as Android's does and the system's Back (the Classic's key, the swipe) pops it. Top to bottom: one progress segment per story of the poster, then Back, their
// picture, name and the story's time; the story fills the rest. A tap on the left third goes back one story, a
// tap anywhere else forward; after the last the sheet closes. A photo or text story lasts 5 s, a video its own
// length. Showing a story marks it seen and sends the read receipt (storyList.markSeen). A story whose file isn't
// here is fetched (storyList.fetch) while its thumbnail shows; the next video is fetched ahead.
// The video plays through MediaPlayer into a ForeignWindowControl, which sits under Cascades' hole, so the
// segments and the header draw over it (the Q10 video gate, 2026-11-12; sound and taps on controls over it
// worked). StoriesPage makes one per open (start(row), row being a StoriesModel row) and pops it on finished().
// At the bottom, a reply field and a heart: a reply goes to the poster's chat quoting the story (story.reply),
// the heart is a reaction to it (story.react). The story waits while the field has focus.
// Our own stories (poster "me") have no reply bar or heart, and a trash can in the header takes one back (story.delete).
Page {
    id: sheet
    actionBarVisibility: ChromeVisibility.Hidden
    // Asks StoriesPage to pop this page: after the last story, Back, a deleted story.
    signal finished()
    property bool closing: false
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
    // Held while the user types a reply: the ticker and a video wait.
    property bool paused: false
    // When the field last lost focus: a touch elsewhere takes it at touch-down, so the tap that follows is only
    // the finger leaving the keyboard, not a move to the next story.
    property real lostAt: 0
    property string lastReply: ""
    // The stories whose heart we sent, this time open only.
    property string reacted: ","
    property bool hearted: sheet.reacted.indexOf("," + sheet.cur.id + ",") >= 0
    // Seconds a photo or text story is shown.
    property real seconds: 5
    property bool videoShown: sheet.cur.kind == 2 && !!sheet.cur.video
    // The video is on screen (playing, or paused by us): until then its thumbnail stays and the spinner turns, since the
    // player takes a second or so to prepare after the file is here.
    property bool playing: false
    // The emoji picker is open: the story waits.
    property bool picking: false
    property bool own: sheet.poster.poster == "me"

    // The story startId when it is given (a tap on a quote of it) and there, else the first one not seen, else the first.
    function start(row, startId) {
        bar.removeAll()
        sheet.marked = ","
        sheet.poster = row
        sheet.items = storyList.storiesOf(row.poster)
        var first = 0
        for (var i = sheet.items.length - 1; i >= 0; i--) {
            if (!sheet.items[i].seen) {
                first = i
            }
        }
        for (var k = 0; startId && k < sheet.items.length; k++) {
            if (sheet.items[k].id == startId) {
                first = k
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
    }
    // Once: the ticker could ask again during the pop's slide.
    function close() {
        if (sheet.closing) {
            return
        }
        sheet.closing = true
        ticker.stop()
        player.stop()
        sheet.finished()
    }
    function show(i) {
        console.log("story: show " + i + " of " + sheet.items.length)
        if (i >= sheet.items.length) {
            sheet.close()
            return
        }
        i = Math.max(i, 0)
        player.stop()
        sheet.playing = false
        sheet.picking = false
        reply.text = ""
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
    function sendReply() {
        var text = reply.text.replace(/^\s+|\s+$/g, "")
        if (text == "" || sheet.index < 0) {
            return
        }
        sheet.lastReply = text
        storyList.reply(sheet.poster.poster, sheet.cur.id, text)
        reply.text = ""
        reply.loseFocus()
    }
    function heart() {
        sheet.react("\u2764\uFE0F")
    }
    // A reaction to the story on screen: the heart, or the emoji picked. Marks it, once per story.
    function react(emoji) {
        sheet.picking = false
        sheet.paused = reply.focused
        if (sheet.index < 0 || sheet.hearted) {
            return
        }
        sheet.reacted += sheet.cur.id + ","
        storyList.react(sheet.poster.poster, sheet.cur.id, emoji)
        emojis.use(emoji)
    }
    function togglePicker() {
        if (!pickerSlot.delegateActive) {
            pickerSlot.delegateActive = true // made at the first open, then kept
        }
        sheet.picking = !sheet.picking
        sheet.paused = sheet.picking || reply.focused
        if (sheet.picking) {
            reply.loseFocus()
        }
    }
    function replyFailed(p, id) {
        if (p == sheet.poster.poster) {
            reply.text = sheet.lastReply
            sendFailed.body = qsTr("Couldn't send the reply")
            sendFailed.show()
        }
    }
    function reactFailed(p, id) {
        if (p == sheet.poster.poster) {
            sheet.reacted = sheet.reacted.split("," + id + ",").join(",")
            sendFailed.body = qsTr("Couldn't send the heart")
            sendFailed.show()
        }
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
        storyList.replyFailed.connect(sheet.replyFailed)
        storyList.reactFailed.connect(sheet.reactFailed)
    }
    onPausedChanged: {
        if (sheet.videoShown) {
            if (sheet.paused) {
                player.pause()
            } else {
                player.play()
            }
        }
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
            onMediaStateChanged: {
                // ponytail: logged to catch why a poster's last story didn't close (the user, 2026-11-13); drop once found.
                console.log("story video: state " + mediaState + " at " + position + " of " + duration)
                sheet.playing = (mediaState == MediaState.Started || mediaState == MediaState.Paused)
            }
            onPositionChanged: {
                if (sheet.cur.kind == 2 && player.duration > 0 && !sheet.paused) {
                    sheet.setProgress(player.position / player.duration)
                }
            }
            onPlaybackCompleted: {
                console.log("story video: completed")
                sheet.next()
            }
            onError: console.log("story video: error " + mediaError + " at " + position)
        },
        SystemToast {
            id: loadFailed
            body: qsTr("Couldn't load the story")
        },
        SystemToast {
            id: sendFailed
        },
        // Taking one of our own stories back: it goes from everyone, so it asks.
        SystemDialog {
            id: confirmDelete
            property string storyId: ""
            title: qsTr("Delete this story?")
            body: qsTr("It is taken back from everyone who saw it.")
            confirmButton.label: qsTr("Delete")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                sheet.paused = false
                if (result == SystemUiResult.ConfirmButtonSelection) {
                    engine.request("story.delete", {"id": confirmDelete.storyId})
                    sheet.close()
                }
            }
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
            visible: sheet.cur.kind == 1 || (sheet.cur.kind == 2 && !sheet.playing)
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
        // Waiting: for a photo's file, for a video's file and then for the player to show its first frame.
        ActivityIndicator {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ui.du(9)
            preferredHeight: ui.du(9)
            running: (sheet.cur.kind == 1 && !sheet.cur.ready) || (sheet.cur.kind == 2 && !sheet.playing)
            visible: running
        }
        // Taps: the left third is back, the rest forward. Under the header, which takes its own.
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            gestureHandlers: TapHandler {
                onTapped: {
                    if (new Date().getTime() - sheet.lostAt < 500) {
                        return
                    }
                    if (event.x < area.layoutFrame.width / 3) {
                        sheet.prev()
                    } else {
                        sheet.next()
                    }
                }
            }
        }
        // The bottom of the story, not on our own: the emoji picker when it is open, over the reply bar. The picker
        // is made at the first open and kept, as the chat's is.
        Container {
            visible: !sheet.own
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Bottom
            // On a ground of its own, with a line above: over a story the picker would otherwise be emoji on a picture.
            Container {
                visible: sheet.picking
                horizontalAlignment: HorizontalAlignment.Fill
                background: Color.create("#161616")
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: ui.px(1)
                    background: Color.create("#3A3A3A")
                }
                ControlDelegate {
                    id: pickerSlot
                    source: "EmojiPicker.qml"
                    delegateActive: false
                    horizontalAlignment: HorizontalAlignment.Fill
                    onControlChanged: {
                        if (control) {
                            control.picked.connect(sheet.react)
                        }
                    }
                }
            }
            // The reply field, the emoji button and the heart, over a band.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                leftPadding: ui.du(1)
                rightPadding: ui.du(0.5)
                topPadding: ui.du(1)
                bottomPadding: ui.du(1)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                TextField {
                    id: reply
                    verticalAlignment: VerticalAlignment.Center
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    hintText: qsTr("Reply")
                    input.submitKey: SubmitKey.Send
                    input.onSubmitted: sheet.sendReply()
                    onFocusedChanged: {
                        if (reply.focused) {
                            sheet.picking = false
                        }
                        sheet.paused = reply.focused || sheet.picking
                        if (!reply.focused) {
                            sheet.lostAt = new Date().getTime()
                        }
                    }
                }
                // The emoji button: opens the picker to react with any emoji. A box 10 du square, the touch target.
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: ui.du(10)
                    preferredHeight: ui.du(10)
                    layout: DockLayout {}
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(5.5)
                        preferredHeight: ui.du(5.5)
                        imageSource: "asset:///images/emoji.png"
                        filterColor: sheet.picking ? ui.palette.primary : Color.White
                        opacity: sheet.picking ? 1.0 : 0.75
                    }
                    gestureHandlers: TapHandler {
                        onTapped: sheet.togglePicker()
                    }
                }
                // The heart: the quick reaction. A box 10 du square, the touch target.
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: ui.du(10)
                    preferredHeight: ui.du(10)
                    layout: DockLayout {}
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(5.5)
                        preferredHeight: ui.du(5.5)
                        imageSource: "asset:///images/heart.png"
                        filterColor: sheet.hearted ? Color.create("#E53935") : Color.White
                        opacity: sheet.hearted ? 1.0 : 0.75
                    }
                    gestureHandlers: TapHandler {
                        onTapped: sheet.heart()
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
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
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
                // Delete, ours only: a box 10 du square, the touch target.
                Container {
                    visible: sheet.own
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: ui.du(10)
                    preferredHeight: ui.du(10)
                    layout: DockLayout {}
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(5)
                        preferredHeight: ui.du(5)
                        imageSource: "asset:///images/delete_action.png"
                        filterColor: Color.White
                    }
                    gestureHandlers: TapHandler {
                        onTapped: {
                            sheet.paused = true
                            confirmDelete.storyId = sheet.cur.id
                            confirmDelete.show()
                        }
                    }
                }
            }
        }
    }
}
