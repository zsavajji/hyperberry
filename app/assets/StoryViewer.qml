import bb.cascades 1.4
import bb.multimedia 1.4
import bb.system 1.2
import hyperberry 1.0

// The story viewer (docs/plan/stories.md): a Page pushed on the Stories tab's pane, full screen on black, so it
// slides in from the right as Android's does and the system's Back (the Classic's key, the swipe) pops it. Top to bottom: one progress segment per story of the poster, then Back, their
// picture, name and the story's time; the story fills the rest. A tap on the left third goes back one story, a
// tap anywhere else forward; after the last the sheet closes. A photo or text story lasts 5 s, a video its own
// length. Showing a story marks it seen and sends the read receipt (storyList.markSeen). A story whose file isn't
// here is fetched (storyList.fetch) while its thumbnail shows; the next video is fetched ahead. A photo shows its
// thumbnail until its sharp picture is made (storyList.prepare, on a worker thread, for the shown story and the next).
// The video plays through MediaPlayer into a ForeignWindowControl, which sits under Cascades' hole, so the
// segments and the header draw over it (the Q10 video gate, 2026-11-12; sound and taps on controls over it
// worked). StoriesPage makes one per open (start(row), row being a StoriesModel row) and pops it on finished().
// At the bottom, a reply field and a heart: a reply goes to the poster's chat quoting the story (story.reply),
// the heart is a reaction to it (story.react). The story waits while the field has focus.
// Our own stories (poster "me") have no reply bar or heart, and a trash can in the header takes one back (story.delete).
// On them a band at the bottom has an eye and the number of people who viewed the story; a tap pushes
// StoryViewersPage over this page and the story waits until it is the pane's top again.
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
    // How many viewed the story on screen; ours only.
    property int views: 0

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
    function countViews() {
        sheet.views = sheet.own && sheet.cur.id ? storyList.viewCount(sheet.cur.id) : 0
    }
    // The viewers of the story on screen, on a page of its own over this one: the story waits meanwhile.
    function showViewers() {
        if (sheet.index < 0 || sheet.closing) {
            return
        }
        sheet.paused = true
        var p = viewersDef.createObject()
        p.load(sheet.cur.id)
        storiesNav.push(p)
    }
    // main.qml calls this when this page is the pane's top again (Back from the viewers' page): the story goes on.
    function uncovered() {
        if (!sheet.closing) {
            sheet.paused = reply.focused || sheet.picking
        }
    }
    // The push has ended (main.qml): Cascades gave the Reply field the focus during it on the Q10 (the user, 2026-11-22: a
    // photo story neither moved nor closed), and a focused field holds the story. Its focus change sets paused.
    function pushed() {
        reply.loseFocus()
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
        } else if (s.kind == 1) {
            storyList.prepare(sheet.poster.poster, s.id)
        }
        var ahead = sheet.items[i + 1]
        if (ahead && ahead.kind == 2 && !ahead.ready) {
            storyList.fetch(sheet.poster.poster, ahead.id)
        } else if (ahead && ahead.kind == 1 && ahead.ready) {
            storyList.prepare(sheet.poster.poster, ahead.id)
        }
        sheet.countViews()
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
    // A tap on the story: the left third back, the rest forward.
    function tapped(x) {
        if (new Date().getTime() - sheet.lostAt < 500) {
            return
        }
        if (x < area.layoutFrame.width / 3) {
            sheet.prev()
        } else {
            sheet.next()
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
        if (sheet.paused || sheet.index < 0) {
            return
        }
        // A video's position comes once a second (mm-renderer's status interval): in between the ticker moves its segment.
        if (sheet.cur.kind == 2) {
            if (player.mediaState == MediaState.Started && player.duration > 0) {
                sheet.setProgress(sheet.progress + 100 / player.duration)
            }
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
                // The shown photo and the next get their sharp picture; when it is made this comes again and finds it there.
                if (all[i].kind == 1 && (i == sheet.index || i == sheet.index + 1)) {
                    storyList.prepare(p, id)
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
        storyList.viewsChanged.connect(sheet.countViews)
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
        ComponentDefinition {
            id: viewersDef
            source: "StoryViewersPage.qml"
        },
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
            onMediaStateChanged: sheet.playing = (mediaState == MediaState.Started || mediaState == MediaState.Paused)
            onPositionChanged: {
                // The player's word corrects the ticker's guess: always when ahead, when behind only past half a
                // second (a stall), so the segment doesn't step back a little every second.
                var p = player.duration > 0 ? player.position / player.duration : 0
                if (sheet.cur.kind == 2 && player.duration > 0 && !sheet.paused
                        && (p > sheet.progress || sheet.progress - p > 500 / player.duration)) {
                    sheet.setProgress(p)
                }
            }
            onPlaybackCompleted: sheet.next()
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
                preferredHeight: pxZoom * ui.px(4)
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
        // Four lines of the caption's text, unseen and under everything: the caption scrolls past this height.
        Label {
            opacity: 0
            touchPropagationMode: TouchPropagationMode.None
            multiline: true
            text: "A\nB\nC\nD"
            textFormat: TextFormat.Plain
            textStyle.base: nav.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: nav.bodySize
            attachedObjects: LayoutUpdateHandler {
                id: fourLines
            }
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
            leftPadding: zoom * ui.du(3)
            rightPadding: zoom * ui.du(3)
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
            preferredWidth: zoom * ui.du(9)
            preferredHeight: zoom * ui.du(9)
            running: (sheet.cur.kind == 1 && !sheet.cur.ready) || (sheet.cur.kind == 2 && !sheet.playing)
            visible: running
        }
        // Taps: the left third is back, the rest forward. Under the header, which takes its own.
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            gestureHandlers: TapHandler {
                onTapped: sheet.tapped(event.x)
            }
        }
        // The bottom of the story: a photo's or video's caption, then on others' the emoji picker when it is open over
        // the reply bar, on ours the viewers band. The picker is made at the first open and kept, as the chat's is.
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Bottom
            // The caption, over a band so white reads on any picture; past four lines it scrolls. A tap on it moves through the
            // stories as elsewhere.
            Container {
                visible: (sheet.cur.kind == 1 || sheet.cur.kind == 2) && !!sheet.cur.text && !sheet.picking
                horizontalAlignment: HorizontalAlignment.Fill
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                leftPadding: zoom * ui.du(2)
                rightPadding: zoom * ui.du(2)
                topPadding: zoom * ui.du(1)
                bottomPadding: zoom * ui.du(1)
                // As tall as the caption, at most four lines: a ScrollView alone takes all the room it's given.
                ScrollView {
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: capFrame.layoutFrame.height > 0 ? Math.min(capFrame.layoutFrame.height, fourLines.layoutFrame.height) : fourLines.layoutFrame.height
                    Label {
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        text: sheet.cur.text ? sheet.cur.text : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.textAlign: TextAlign.Center
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: Color.White
                        attachedObjects: LayoutUpdateHandler {
                            id: capFrame
                        }
                    }
                }
                gestureHandlers: TapHandler {
                    onTapped: sheet.tapped(event.x)
                }
            }
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
                visible: !sheet.own
                horizontalAlignment: HorizontalAlignment.Fill
                background: Color.create(0.0, 0.0, 0.0, 0.55)
                leftPadding: zoom * ui.du(1)
                rightPadding: zoom * ui.du(0.5)
                topPadding: zoom * ui.du(1)
                bottomPadding: zoom * ui.du(1)
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
                // The five buttons here and in the header (emoji, heart, viewers, Back, delete) are reached by the Classic's
                // trackpad (the user, 2026-11-20): lit with a pale veil, and a click (Enter) does what a tap does. The emoji and
                // the heart sit beside the Reply field, so they are FocusPolicy.Key: a touch leaves the focus (and the keyboard)
                // in the field; the other three take touch too.
                Container {
                    id: emojiBtn
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: zoom * ui.du(10)
                    preferredHeight: zoom * ui.du(10)
                    layout: DockLayout {}
                    focusPolicy: FocusPolicy.Key
                    background: emojiBtn.focused ? Color.create("#33ffffff") : Color.Transparent
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(5.5)
                        preferredHeight: zoom * ui.du(5.5)
                        imageSource: "asset:///images/emoji.png"
                        filterColor: sheet.picking ? ui.palette.primary : Color.White
                        opacity: sheet.picking ? 1.0 : 0.75
                    }
                    gestureHandlers: TapHandler {
                        onTapped: sheet.togglePicker()
                    }
                    keyListeners: KeyListener {
                        onKeyReleased: {
                            if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                sheet.togglePicker()
                            }
                        }
                    }
                }
                // The heart: the quick reaction. A box 10 du square, the touch target.
                Container {
                    id: heartBtn
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: zoom * ui.du(10)
                    preferredHeight: zoom * ui.du(10)
                    layout: DockLayout {}
                    focusPolicy: FocusPolicy.Key
                    background: heartBtn.focused ? Color.create("#33ffffff") : Color.Transparent
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(5.5)
                        preferredHeight: zoom * ui.du(5.5)
                        imageSource: "asset:///images/heart.png"
                        filterColor: sheet.hearted ? Color.create("#E53935") : Color.White
                        opacity: sheet.hearted ? 1.0 : 0.75
                    }
                    gestureHandlers: TapHandler {
                        onTapped: sheet.heart()
                    }
                    keyListeners: KeyListener {
                        onKeyReleased: {
                            if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                sheet.heart()
                            }
                        }
                    }
                }
            }
            // Ours: who viewed it. An eye and the count over a band, where the others' reply bar is; a tap slides the list in.
            Container {
                id: viewersBtn
                visible: sheet.own
                horizontalAlignment: HorizontalAlignment.Fill
                preferredHeight: zoom * ui.du(10)
                // Focused: the band lighter, the veil over its black.
                background: viewersBtn.focused ? Color.create("#80555555") : Color.create(0.0, 0.0, 0.0, 0.55)
                focusPolicy: FocusPolicy.KeyAndTouch
                layout: DockLayout {}
                Container {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Center
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    ImageView {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(5.5)
                        preferredHeight: zoom * ui.du(5.5)
                        imageSource: "asset:///images/eye.png"
                        filterColor: Color.White
                    }
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        leftMargin: zoom * ui.du(1)
                        text: "" + sheet.views
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: Color.White
                    }
                }
                gestureHandlers: TapHandler {
                    onTapped: sheet.showViewers()
                }
                keyListeners: KeyListener {
                    onKeyReleased: {
                        if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                            sheet.showViewers()
                        }
                    }
                }
            }
        }
        // The segments, and under them Back, the poster and the time, over a band so white reads on any picture.
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Top
            background: Color.create(0.0, 0.0, 0.0, 0.45)
            topPadding: zoom * ui.du(0.6)
            Container {
                id: bar
                horizontalAlignment: HorizontalAlignment.Fill
                leftPadding: zoom * ui.du(0.5)
                rightPadding: zoom * ui.du(0.5)
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
                    id: backBtn
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: zoom * ui.du(10)
                    preferredHeight: zoom * ui.du(10)
                    layout: DockLayout {}
                    focusPolicy: FocusPolicy.KeyAndTouch
                    background: backBtn.focused ? Color.create("#33ffffff") : Color.Transparent
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(5)
                        preferredHeight: zoom * ui.du(5)
                        imageSource: "asset:///images/back_action.png"
                        filterColor: Color.White
                    }
                    gestureHandlers: TapHandler {
                        onTapped: sheet.close()
                    }
                    keyListeners: KeyListener {
                        onKeyReleased: {
                            if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                sheet.close()
                            }
                        }
                    }
                }
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: zoom * ui.du(6)
                    preferredHeight: zoom * ui.du(6)
                    background: Color.create(sheet.poster.color ? sheet.poster.color : "#087099")
                    layout: DockLayout {}
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        text: sheet.poster.initials
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 100 * zoom
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
                    leftMargin: zoom * ui.du(1.2)
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
                    id: deleteBtn
                    visible: sheet.own
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: zoom * ui.du(10)
                    preferredHeight: zoom * ui.du(10)
                    layout: DockLayout {}
                    focusPolicy: FocusPolicy.KeyAndTouch
                    background: deleteBtn.focused ? Color.create("#33ffffff") : Color.Transparent
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(5)
                        preferredHeight: zoom * ui.du(5)
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
                    keyListeners: KeyListener {
                        onKeyReleased: {
                            if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
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
}
