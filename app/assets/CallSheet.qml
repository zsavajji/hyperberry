import bb.cascades 1.4
import hyperberry 1.0

// The call screen (step 7.4), in the look of the stock Phone app: one Sheet owned by main.qml, over
// everything. It opens on any `call` event that isn't `ended` and closes 2 s after `ended`. While the
// call rings: silence, message and the slide bar (CallRinging); else Speaker, Mute and End call
// (CallPanel). engine.call is {id, chat, out, state, reason, since} (docs/plan/phase7.md). The
// stage is the Q10's 720 px screen, kept to the bottom, so what the stock app measures from
// there stays where it was; px() scales its px by the device's du.
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
    property string clock: "0:00"
    property string line: wording(phase, call.reason)
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

    function wording(phase, reason) {
        switch (phase) {
        case "calling":
            return qsTr("Calling…")
        case "ringing":
            return qsTr("Incoming voice call")
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
    onClosed: {
        syncHolds(false)
        callAudio.stop()
        audioFor = ""
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
        if (st == "ended") {
            if (call.since > 0) {
                refreshClock()
            }
            ticker.stop()
            closer.start()
            callAudio.stop()
            audioFor = ""
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
            callAudio.speaker = false
            open()
        }
    }
    attachedObjects: [
        Timer {
            id: ticker
            interval: 1000
            onTimeout: sheet.refreshClock()
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
            background: Color.create("#132a1c") // the stock #1b132a in the primary colour's hue
            layout: DockLayout {}
            Container {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Bottom
                preferredWidth: sheet.px(720)
                preferredHeight: sheet.px(720)
                layout: AbsoluteLayout {}
                ImageView {
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.px(720)
                    scalingMethod: ScalingMethod.AspectFit
                    imageSource: "asset:///images/call_bg.png"
                }
                // The picture fills the area above the controls and fades to black at its foot.
                ImageView {
                    visible: sheet.hasPhoto
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.ringing ? sheet.px(510) : sheet.px(415)
                    scalingMethod: ScalingMethod.AspectFill
                    loadEffect: ImageViewLoadEffect.None
                    imageSource: sheet.hasPhoto ? "file://" + sheet.who.photo : ""
                }
                ImageView {
                    visible: sheet.hasPhoto
                    layoutProperties: AbsoluteLayoutProperties {
                        positionY: sheet.ringing ? sheet.px(250) : sheet.px(155)
                    }
                    preferredWidth: sheet.px(720)
                    preferredHeight: sheet.px(260)
                    scalingMethod: ScalingMethod.AspectFit
                    imageSource: "asset:///images/call_fade.png"
                }
                Label {
                    layoutProperties: AbsoluteLayoutProperties {
                        positionX: sheet.px(30)
                        positionY: sheet.ringing ? sheet.px(291) : sheet.px(339)
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
                    layoutProperties: AbsoluteLayoutProperties {
                        positionX: sheet.px(29)
                        positionY: sheet.ringing ? sheet.px(365) : sheet.px(386)
                    }
                    preferredWidth: sheet.px(650)
                    text: sheet.line
                    textFormat: TextFormat.Plain
                    textStyle.color: Color.White
                }
                Label {
                    visible: sheet.timed
                    layoutProperties: AbsoluteLayoutProperties {
                        positionX: sheet.px(467)
                        positionY: sheet.px(364)
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
                CallPanel {
                    visible: !sheet.ringing
                    ended: sheet.ended
                    onHangup: engine.request("call.hangup", {"id": sheet.call.id})
                }
            }
            // The header band, as the stock app's, under no status bar here.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                preferredHeight: sheet.px(55)
                background: Color.create("#0b1910") // the stock #0f0b19 in the primary colour's hue
                layout: DockLayout {}
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: sheet.px(30)
                    text: qsTr("WhatsApp: voice call")
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
