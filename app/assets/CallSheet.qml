import bb.cascades 1.4
import hyperberry 1.0

// The call screen (step 7.4), in the look of the stock Phone app: one Sheet owned by main.qml, over
// everything. It opens on any `call` event that isn't `ended` and closes 2 s after `ended`. While the
// call rings: silence, message and the slide bar (CallRinging); else Speaker, Mute and End call
// (CallPanel). engine.call is {id, chat, out, state, reason, since, video, videoOurs, videoTheirs,
// videoAsked} (docs/plan/phase7.md, docs/protocol2.md). The stage is the Q10's 720 px screen, kept
// to the bottom, so what the stock app measures from there stays where it was; px() scales its px
// by the device's du. A call with video (step 7c.3) puts CallVideoView over the stage and under
// the panel: the peer's picture fills the sheet, and the controls hide a few seconds after it shows.
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
    // The call the audio link was started for, so connecting -> active doesn't restart it.
    property string audioFor: ""
    // The call the video link was started for.
    property string videoFor: ""
    // The call has video, or the peer asks for ours: the screen takes the video look.
    property bool isVideo: !ringing && !!(call.video || call.videoOurs || call.videoTheirs || call.videoAsked)
    // The peer's picture is on screen: it is the background, and the controls may hide.
    property bool showPicture: isVideo && !!call.videoTheirs && callVideo.pictureWidth > 0
    property bool controlsShown: true
    property string clock: "0:00"
    property string line: wording(phase, call.reason, call.video)
    // The clock shows from the answer to the hang-up, and stays at the end.
    property bool timed: (phase == "active" || ended) && call.since > 0
    // The caller's picture, if the chat has one.
    property bool hasPhoto: !!who.photo
    // The name of a caller we have no chat with: their number.
    property string number: (call.chat || "").split("@")[0]
    peekEnabled: false

    function px(n) {
        return ui.du(n / 9)
    }

    function wording(phase, reason, video) {
        switch (phase) {
        case "calling":
            return qsTr("Calling…")
        case "ringing":
            return video ? qsTr("Incoming video call") : qsTr("Incoming voice call")
        case "connecting":
            return qsTr("Connecting…")
        case "ended":
            var word = reasonWord(reason)
            return qsTr("Call ended") + (word != "" ? " · " + word : "")
        }
        return ""
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
        callVideo.sending = !!call.videoOurs
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
        if (st == "ringing") {
            ringControls.silenced = false
        }
        if (st == "active" && call.since > 0) {
            refreshClock()
            ticker.start()
        } else {
            ticker.stop()
        }
        if (!opened) {
            callAudio.muted = false
            callAudio.speaker = !!call.video
            open()
        }
        // After open(), so the controls the windows bind to are in the sheet. Video goes to the
        // loudspeaker, once per call (a voice call that gets video included).
        if ((st == "connecting" || st == "active") && (call.video || call.videoOurs || call.videoTheirs || call.videoAsked) && videoFor != call.id) {
            videoFor = call.id
            callVideo.front = true
            callAudio.speaker = true
            callVideo.start(call.id, videoView.remoteId, videoView.finderId)
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
            background: sheet.showPicture ? Color.Transparent : Color.create("#132a1c") // the stock #1b132a in the primary colour's hue
            layout: DockLayout {}
            Container {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Bottom
                preferredWidth: sheet.px(720)
                preferredHeight: sheet.px(720)
                layout: AbsoluteLayout {}
                ImageView {
                    visible: !sheet.showPicture
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.px(720)
                    scalingMethod: ScalingMethod.AspectFit
                    imageSource: "asset:///images/call_bg.png"
                }
                // The picture fills the area above the controls and fades to black at its foot.
                ImageView {
                    visible: sheet.hasPhoto && !sheet.showPicture
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.ringing ? sheet.px(510) : sheet.px(415)
                    scalingMethod: ScalingMethod.AspectFill
                    loadEffect: ImageViewLoadEffect.None
                    imageSource: sheet.hasPhoto ? "file://" + sheet.who.photo : ""
                }
                ImageView {
                    visible: sheet.hasPhoto && !sheet.showPicture
                    layoutProperties: AbsoluteLayoutProperties {
                        positionY: sheet.ringing ? sheet.px(250) : sheet.px(155)
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
                        positionY: sheet.ringing ? sheet.px(284) : sheet.px(312)
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
                        positionY: sheet.ringing ? sheet.px(360) : sheet.px(372)
                    }
                    preferredWidth: sheet.px(650)
                    text: sheet.line
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.White
                }
                Label {
                    visible: sheet.timed && !sheet.isVideo
                    layoutProperties: AbsoluteLayoutProperties {
                        positionX: sheet.px(467)
                        positionY: sheet.px(373)
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
                    visible: sheet.ringing
                    onSilence: engine.request("call.silence", {"id": sheet.call.id})
                    onAnswered: engine.request("call.answer", {"id": sheet.call.id})
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
                video: sheet.isVideo
                videoOn: !!sheet.call.videoOurs
                onTouched: sheet.wake()
                onHangup: engine.request("call.hangup", {"id": sheet.call.id})
                onVideoTapped: engine.request("call.video", {"id": sheet.call.id, "on": !sheet.call.videoOurs})
                onSwitchTapped: callVideo.front = !callVideo.front
            }
            // The header band, as the stock app's, under no status bar here.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                visible: !sheet.isVideo
                preferredHeight: sheet.px(55)
                background: Color.create("#0b1910") // the stock #0f0b19 in the primary colour's hue
                // Padding, not a margin: a margin doesn't count from the container's edge.
                leftPadding: sheet.px(30)
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
            // At the ear: black, and every touch stops here, so a cheek presses nothing.
            Container {
                visible: sheet.held && proximity.near && !callAudio.speaker
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                background: Color.Black
                onTouch: {}
            }
        }
    }
}
