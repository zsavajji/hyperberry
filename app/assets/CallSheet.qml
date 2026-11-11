import bb.cascades 1.4
import hyperberry 1.0

// The call screen (step 7.4), in the look of the stock Phone app: one Sheet owned by main.qml, over
// everything. It opens on any `call` event that isn't `ended` and closes 2 s after `ended`. While the
// call rings: video or not, message and the slide bar (CallRinging); else Speaker, Mute and End call
// (CallPanel). engine.call is {id, chat, out, state, reason, since, video, videoOurs, videoTheirs,
// videoAsked} (docs/plan/phase7.md, docs/protocol2.md). The stage is the Q10's 720 px screen, kept
// to the bottom, so what the stock app measures from there stays where it was; px() scales its px
// to the screen's width (not by du: the Classic is 8 px/du to the Q10's 9, which left the stage
// 640 px wide on a Q20). On a tall screen (the Z10) the stage runs up to the top and its contents
// move down by shift, as the stock app's do; the caller's picture fills from the top, and the panel
// lays its controls out as the stock Z10 grid. A call with video (step 7c.3) puts CallVideoView
// over the stage and under the panel: the peer's picture fills the sheet, and the controls hide a
// few seconds after it shows.
// A group call (step 7d.G4) adds group, by and people to engine.call: it rings as the group's, and
// while it is active the middle line names who is in it, and a drawer from the left (CallDrawer) lists
// them. With video (step 7e) the view shows up to four of the people's pictures at once, tiles 0 to 3
// (CallVideoView).
Sheet {
    id: sheet
    property variant call: engine.call
    property string phase: call.state || ""
    property bool ringing: phase == "ringing"
    property bool ended: phase == "ended"
    // The chat's row, read when a call comes in (its name and picture); {} for a chat we don't have.
    property variant who: ({})
    // The proximity sensor and the awake screen are held while the call is live, from dialling to
    // hang-up (and released when the sheet closes, whatever the state).
    property bool held: false
    // At the ear: the screen goes black and takes no touch. Never on the loudspeaker, where the phone
    // is held away (a video call starts there).
    property bool atEar: held && proximity.near && !callAudio.speaker
    // Our own preview hidden by the panel's toggle (the user's ask); our video still goes out. Shown
    // again with each call.
    property bool previewHidden: false
    // The call the audio link was started for, so connecting -> active doesn't restart it.
    property string audioFor: ""
    // The call the video link was started for.
    property string videoFor: ""
    // The call has video, or the peer asks for ours: the screen takes the video look.
    property bool isVideo: !ringing && !!(call.video || call.videoOurs || call.videoTheirs || call.videoAsked)
    // The peer's picture is on screen: it is the background, and the controls may hide. A group
    // call has one when any of its tiles shows a picture (the view says which).
    property bool showPicture: isVideo && (group ? videoView.shownMask != 0 : !!call.videoTheirs && callVideo.pictureWidth > 0)
    property bool controlsShown: true
    property string clock: "0:00"
    // What wording reads is passed in, so a change in any of it (people, say) redraws the line.
    property string line: wording(phase, call.reason, call.video, !!call.group, call.by || "", call.people)
    // A group call: it rings as the group's, and its video is a grid of tiles, not one picture.
    property bool group: !!call.group
    // The clock shows from the answer to the hang-up, and stays at the end.
    property bool timed: (phase == "active" || ended) && call.since > 0
    // The caller's picture, the full size once the engine has it, else the chat's preview.
    property bool hasPhoto: !!(call.picture || who.photo)
    // The name of a caller we have no chat with: their number.
    property string number: (call.chat || "").split("@")[0]
    peekEnabled: false

    // What a tall screen adds above the 720 stage (0 on a square one).
    property real extra: ui.px(Math.max(0, screenHeight - screenWidth))
    // How far the stage's contents rise from the foot of a tall screen, from the stock Z10's frames
    // (2026-11-11): ringing, the name sits at 511 px and the slide bar ends at 1007, the foot left
    // empty; in a call the name sits at 640 and the controls take the bottom 540 px. 0 when square.
    property real lift: Math.min(extra, ringing ? px(274) : px(192))
    // Where the stage's 720 square starts: the Q10's layout moved down this far.
    property real shift: extra - lift
    function px(n) {
        return ui.px(n * screenWidth / 720)
    }

    function wording(phase, reason, video, group, by, people) {
        switch (phase) {
        case "calling":
            return qsTr("Calling…")
        case "ringing":
            if (group) {
                if (video) {
                    return by != "" ? qsTr("Incoming group video call · %1").arg(by) : qsTr("Incoming group video call")
                }
                return by != "" ? qsTr("Incoming group voice call · %1").arg(by) : qsTr("Incoming group voice call")
            }
            return video ? qsTr("Incoming video call") : qsTr("Incoming voice call")
        case "connecting":
            return qsTr("Connecting…")
        case "active":
            return group ? peopleLine(people) : ""
        case "ended":
            var word = reasonWord(reason)
            return qsTr("Call ended") + (word != "" ? " · " + word : "")
        }
        return ""
    }
    // Who is in a group call: the connected people's names, "Anna", "Anna and Bob" or "Anna, Bob and 2
    // more"; "" while none is (or their names are not looked up yet), so the line keeps its old text.
    function peopleLine(people) {
        var names = []
        for (var i = 0; people && i < people.length; i++) {
            if (people[i].connected && people[i].name != "") {
                names.push(people[i].name)
            }
        }
        if (names.length == 0) {
            return ""
        }
        if (names.length == 1) {
            return names[0]
        }
        if (names.length == 2) {
            return qsTr("%1 and %2").arg(names[0]).arg(names[1])
        }
        return qsTr("%1, %2 and %3 more").arg(names[0]).arg(names[1]).arg(names.length - 2)
    }
    function reasonWord(reason) {
        switch (reason) {
        case "declined":
            return qsTr("Declined")
        case "busy":
            return qsTr("Busy")
        case "no_answer":
            return qsTr("No answer")
        case "failed":
            return qsTr("Call failed")
        case "offline":
            return qsTr("Offline")
        }
        return ""
    }
    // m:ss from the call's own start, so a sheet opened late (a Hub tap) shows the true time.
    function refreshClock() {
        var secs = Math.max(0, Math.floor((new Date().getTime() - call.since) / 1000))
        var s = secs % 60
        clock = Math.floor(secs / 60) + ":" + (s < 10 ? "0" : "") + s
    }
    function syncHolds(wanted) {
        if (wanted && !held) {
            held = true
            proximity.acquire(sheet)
        } else if (!wanted && held) {
            held = false
            proximity.release(sheet)
        }
    }
    // Shows the controls and starts their countdown to hiding (the hider says when it is allowed).
    function wake() {
        controlsShown = true
        hider.start()
    }
    function toggleControls() {
        if (!showPicture) {
            return
        }
        if (controlsShown) {
            controlsShown = false
            hider.stop()
        } else {
            wake()
        }
    }
    function stopVideo() {
        callVideo.stop()
        videoFor = ""
        videoView.focusTile = -1
        previewHidden = false
    }
    // Without a picture the controls stay; with it they count down.
    onShowPictureChanged: {
        if (showPicture) {
            wake()
        } else {
            controlsShown = true
            hider.stop()
        }
    }
    onClosed: {
        syncHolds(false)
        callAudio.stop()
        audioFor = ""
        stopVideo()
        drawer.reset()
    }
    // The state is read from call itself, never from phase, ended or ringing: in Qt 4.8 this
    // handler can run before those bindings catch up with the same change. Reading them left the
    // clock at 0:00 and the sheet on "Call ended" on the Q10 (0.0.1.181).
    onCallChanged: {
        var st = call.state || ""
        if (st == "") {
            return
        }
        syncHolds(st == "calling" || st == "connecting" || st == "active")
        // Answered without video, ours stays off while connecting too: the engine turns it off only
        // as the call goes active, and the camera mustn't show the user meanwhile.
        callVideo.sending = !!call.videoOurs && (call.out || st == "active" || ringControls.withVideo)
        // The controls stay while the call isn't on or the peer asks for our video; else they count down.
        if (st != "active" || call.videoAsked) {
            controlsShown = true
            hider.stop()
        } else if (controlsShown) {
            hider.start()
        }
        if (st == "ended") {
            if (call.since > 0) {
                refreshClock()
            }
            ticker.stop()
            closer.start()
            callAudio.stop()
            audioFor = ""
            stopVideo()
            return
        }
        if ((st == "connecting" || st == "active") && audioFor != call.id) {
            audioFor = call.id
            callAudio.start(call.id)
        }
        closer.stop()
        who = chats.chat(call.chat)
        if (st == "active" && call.since > 0) {
            refreshClock()
            ticker.start()
        } else {
            ticker.stop()
        }
        if (!opened) {
            callAudio.muted = false
            callAudio.speaker = !!call.video
            ringControls.withVideo = !!call.video
            open()
        }
        // After open(), so the controls the windows bind to are in the sheet. Video goes to the
        // loudspeaker, once per call (a voice call that gets video included).
        if ((st == "connecting" || st == "active") && (call.video || call.videoOurs || call.videoTheirs || call.videoAsked) && videoFor != call.id) {
            videoFor = call.id
            callVideo.front = true
            callAudio.speaker = true
            callVideo.start(call.id, videoView.remoteIds, videoView.finderId)
        }
        // The drawer's rows follow the call event's people, from call itself (as above).
        if (call.group) {
            sidebar.sync(call.people)
        }
    }
    attachedObjects: [
        Timer {
            id: ticker
            interval: 1000
            onTimeout: sheet.refreshClock()
        },
        // The controls hide 5 s after the last touch, once the peer's picture shows.
        Timer {
            id: hider
            interval: 5000
            singleShot: true
            onTimeout: {
                if (sheet.showPicture && sheet.call.state == "active" && !sheet.call.videoAsked) {
                    sheet.controlsShown = false
                }
            }
        },
        Timer {
            id: closer
            interval: 2000
            singleShot: true
            onTimeout: sheet.close()
        }
    ]
    // A Page with no field, so the Q10's keys have nowhere to go; the composer under the sheet is
    // in the window below and gets none (bb10-pitfalls: keys follow the focused control).
    Page {
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            // The peer's picture shows through the sheet's hole: no background over it.
            background: sheet.showPicture ? Color.Transparent : Color.Black
            layout: DockLayout {}
            // The people drawer's sidebar (CallDrawer has the logic): it never moves, and sits UNDER the
            // page, which slides right off it (the stock sidebar's reveal); it shows only while the drawer
            // does, as an opaque sidebar under the page would hide the video's windows.
            CallSidebar {
                id: sidebar
            }
            // The call screen's page: everything but the sidebar and the ear's black cover. The drawer
            // slides it right by the sidebar's width (translationX is set by CallDrawer.place(), the
            // glide is slide). In a voice call the page is black while the drawer shows; with video it
            // never has a background, or the hole for the video's windows would be painted over
            // (0.0.1.190), and the windows stay on (264 hid them and the page went black).
            // Not clipped: the shadow below lies just left of it.
            Container {
                id: callPage
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                background: drawer.shown && !sheet.isVideo ? Color.Black : Color.Transparent
                clipContentToBounds: false
                layout: DockLayout {}
                // The drawer sets translationX itself, as the finger drags; an implicit animation
                // would trail it.
                attachedObjects: ImplicitAnimationController {
                    propertyName: "translationX"
                    enabled: false
                }
                animations: TranslateTransition {
                    id: slide
                    duration: 200
                    easingCurve: StockCurve.CubicOut
                    onEnded: drawer.settled()
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Bottom
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.px(720) + sheet.extra
                    layout: AbsoluteLayout {}
                    // Fills a tall screen above the background picture, whose foot is black like the rest
                    // below.
                    Container {
                        layoutProperties: AbsoluteLayoutProperties {
                            positionX: 0
                            positionY: 0
                        }
                        visible: !sheet.showPicture && sheet.shift > 0
                        preferredWidth: sheet.px(720)
                        preferredHeight: sheet.shift
                        background: Color.create("#132a1c") // the stock #1b132a in the primary colour's hue
                    }
                    ImageView {
                        visible: !sheet.showPicture
                        layoutProperties: AbsoluteLayoutProperties {
                            positionY: sheet.shift
                        }
                        preferredWidth: sheet.px(720)
                        preferredHeight: sheet.px(720)
                        scalingMethod: ScalingMethod.AspectFit
                        imageSource: "asset:///images/call_bg.png"
                    }
                    // The picture fills the area above the controls and fades to black at its foot.
                    ImageView {
                        visible: sheet.hasPhoto && !sheet.showPicture
                        preferredWidth: sheet.px(720)
                        preferredHeight: (sheet.ringing ? sheet.px(510) : sheet.px(415)) + sheet.shift
                        scalingMethod: ScalingMethod.AspectFill
                        loadEffect: ImageViewLoadEffect.None
                        imageSource: sheet.hasPhoto ? "file://" + (sheet.call.picture || sheet.who.photo) : ""
                    }
                    ImageView {
                        visible: sheet.hasPhoto && !sheet.showPicture
                        layoutProperties: AbsoluteLayoutProperties {
                            positionY: (sheet.ringing ? sheet.px(250) : sheet.px(155)) + sheet.shift
                        }
                        preferredWidth: sheet.px(720)
                        preferredHeight: sheet.px(260)
                        scalingMethod: ScalingMethod.AspectFit
                        imageSource: "asset:///images/call_fade.png"
                    }
                    // The video view's top band says the name, the line and the clock of a call with video.
                    Label {
                        visible: !sheet.isVideo
                        layoutProperties: AbsoluteLayoutProperties {
                            positionX: sheet.px(30)
                            positionY: (sheet.ringing ? sheet.px(284) : sheet.px(312)) + sheet.shift
                        }
                        preferredWidth: sheet.timed ? sheet.px(450) : sheet.px(650)
                        multiline: false
                        text: sheet.who.name || sheet.number
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: sheet.ringing ? 200 : 170
                        textStyle.color: Color.White
                    }
                    Label {
                        visible: !sheet.isVideo
                        layoutProperties: AbsoluteLayoutProperties {
                            positionX: sheet.px(29)
                            positionY: (sheet.ringing ? sheet.px(360) : sheet.px(372)) + sheet.shift
                        }
                        // Short of the clock when it shows: a group's names run long.
                        preferredWidth: sheet.timed ? sheet.px(430) : sheet.px(650)
                        text: sheet.line
                        textFormat: TextFormat.Plain
                        textStyle.color: Color.White
                    }
                    Label {
                        visible: sheet.timed && !sheet.isVideo
                        layoutProperties: AbsoluteLayoutProperties {
                            positionX: sheet.px(467)
                            positionY: sheet.px(373) + sheet.shift
                        }
                        preferredWidth: sheet.px(220)
                        text: sheet.clock
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 95
                        textStyle.textAlign: TextAlign.Right
                        textStyle.color: Color.White
                    }
                    CallRinging {
                        id: ringControls
                        layoutProperties: AbsoluteLayoutProperties {
                            positionY: sheet.shift
                        }
                        visible: sheet.ringing
                        video: !!sheet.call.video
                        // A voice call is answered as one; a video call with or without ours.
                        onAnswered: engine.request("call.answer", sheet.call.video ? {"id": sheet.call.id, "video": ringControls.withVideo} : {"id": sheet.call.id})
                        onDeclined: engine.request("call.reject", {"id": sheet.call.id})
                        // Declines, then the caller's chat opens with the composer, as a contact page's
                        // Message does, for a chat we may not have yet.
                        onMessage: {
                            engine.request("call.reject", {"id": sheet.call.id})
                            sheet.close()
                            nav.openRow(sheet.who.jid ? sheet.who : {"jid": sheet.call.chat, "name": sheet.number, "initials": "", "color": "#087099"})
                        }
                    }
                }
                // Over the stage, under the panel: the panel's taps and picture are above the video's.
                CallVideoView {
                    id: videoView
                }
                CallPanel {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Bottom
                    visible: !sheet.ringing && (!sheet.isVideo || sheet.controlsShown)
                    ended: sheet.ended
                    lift: sheet.lift
                    video: sheet.isVideo
                    group: sheet.group
                    videoOn: !!sheet.call.videoOurs
                    previewHidden: sheet.previewHidden
                    onTouched: sheet.wake()
                    onHangup: engine.request("call.hangup", {"id": sheet.call.id})
                    onVideoTapped: engine.request("call.video", {"id": sheet.call.id, "on": !sheet.call.videoOurs})
                    onSwitchTapped: callVideo.front = !callVideo.front
                    onPreviewTapped: sheet.previewHidden = !sheet.previewHidden
                }
                // The header band, as the stock app's, under no status bar here.
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    verticalAlignment: VerticalAlignment.Top
                    visible: !sheet.isVideo
                    preferredHeight: sheet.px(55)
                    background: Color.create("#0b1910") // the stock #0f0b19 in the primary colour's hue
                    // Padding, not a margin: a margin doesn't count from the container's edge. A group call
                    // has the People button at the left.
                    leftPadding: sheet.group ? sheet.px(80) : sheet.px(30)
                    layout: DockLayout {}
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        text: sheet.call.video ? qsTr("WhatsApp: video call") : qsTr("WhatsApp: voice call")
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 85
                        textStyle.color: Color.create("#e8e8e8")
                    }
                }
                // The People button of a group call (CallDrawer): a 72 px touch target at the top left
                // (in a voice call over the header band, in a video call with the controls). The icon
                // sits in the band's height. CallDrawer's edge strip lies over its left 30 px and passes
                // a tap there on.
                Container {
                    visible: drawer.buttonShown
                    horizontalAlignment: HorizontalAlignment.Left
                    verticalAlignment: VerticalAlignment.Top
                    preferredWidth: sheet.px(72)
                    preferredHeight: sheet.px(72)
                    topPadding: sheet.px(3)
                    layout: DockLayout {}
                    ImageView {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Top
                        preferredWidth: sheet.px(48)
                        preferredHeight: sheet.px(48)
                        scalingMethod: ScalingMethod.AspectFit
                        loadEffect: ImageViewLoadEffect.None
                        imageSource: "asset:///images/contacts_action.png"
                        filterColor: Color.White
                    }
                    gestureHandlers: TapHandler {
                        onTapped: drawer.toggle()
                    }
                }
                // The shadow the page casts on the sidebar: the sidebar's last 12 px, from clear to black
                // at 20 % at the page's edge, so just left of the page and moving with it.
                ImageView {
                    visible: drawer.shown
                    horizontalAlignment: HorizontalAlignment.Left
                    verticalAlignment: VerticalAlignment.Fill
                    translationX: -sheet.px(12)
                    preferredWidth: sheet.px(12)
                    scalingMethod: ScalingMethod.Fill
                    loadEffect: ImageViewLoadEffect.None
                    imageSource: "asset:///images/call_drawer_shadow.png"
                }
                // The dimmed page: black over it, up to 70 % as the page slides, moving and fading with
                // it. It takes the touches of the page that shows while the drawer is out: a tap closes
                // the drawer, a drag to the left pushes it shut (CallDrawer's thresholds), and nothing
                // under it is pressed.
                Container {
                    id: dim
                    visible: drawer.shown
                    horizontalAlignment: HorizontalAlignment.Fill
                    verticalAlignment: VerticalAlignment.Fill
                    opacity: 0
                    background: Color.create("#b3000000")
                    attachedObjects: ImplicitAnimationController {
                        propertyName: "opacity"
                        enabled: false
                    }
                    animations: FadeTransition {
                        id: fade
                        duration: 200
                    }
                    onTouch: drawer.dragTouch(event, 0)
                    onTouchExit: {
                        if (drawer.dragging) {
                            drawer.release()
                        }
                    }
                    gestureHandlers: TapHandler {
                        onTapped: drawer.glide(false)
                    }
                }
            }
            // The people drawer's logic, edge strip and hint: still, over the page and under the ear's
            // black.
            CallDrawer {
                id: drawer
            }
            // At the ear: black, and every touch stops here, so a cheek presses nothing. Our camera's
            // viewfinder is a window above Cascades that this can't cover: it hides itself (CallVideoView).
            Container {
                visible: sheet.atEar
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                background: Color.Black
                onTouch: {}
            }
        }
    }
}
