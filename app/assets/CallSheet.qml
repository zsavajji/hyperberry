import bb.cascades 1.4
import hyperberry 1.0

// The call screen (step 7.4): one Sheet owned by main.qml, over everything. It opens on any `call`
// event that isn't `ended` and closes 2 s after `ended`. Answer and Decline while the call rings,
// else Mute, Speaker and End. engine.call is {id, chat, out, state, reason, since} (docs/plan/phase7.md).
Sheet {
    id: sheet
    property variant call: engine.call
    property string phase: call.state || ""
    property bool ringing: phase == "ringing"
    property bool ended: phase == "ended"
    // The chat's row, read when a call comes in (its name and picture); {} for a chat we don't have.
    property variant who: ({})
    // A call is live from dialling to hang-up; the proximity sensor and the awake screen are held
    // for exactly that (and released when the sheet closes, whatever the state).
    property bool live: phase == "calling" || phase == "connecting" || phase == "active"
    property bool held: false
    // The call the audio link was started for, so connecting -> active doesn't restart it.
    property string audioFor: ""
    property string clock: "0:00"
    property string line: wording(phase, call.reason, clock)
    peekEnabled: false

    function wording(phase, reason, clock) {
        switch (phase) {
        case "calling":
            return qsTr("Calling…")
        case "ringing":
            return qsTr("Incoming voice call")
        case "connecting":
            return qsTr("Connecting…")
        case "active":
            return clock
        case "ended":
            return qsTr("Call ended")
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
    onCallChanged: {
        if (phase == "") {
            return
        }
        syncHolds(live)
        if (ended) {
            ticker.stop()
            closer.start()
            callAudio.stop()
            audioFor = ""
            return
        }
        if ((phase == "connecting" || phase == "active") && audioFor != call.id) {
            audioFor = call.id
            callAudio.start(call.id)
        }
        closer.stop()
        who = chats.chat(call.chat)
        if (phase == "active" && call.since > 0) {
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
            layout: DockLayout {}
            Container {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                layout: StackLayout {}
                CallTile {
                    horizontalAlignment: HorizontalAlignment.Center
                    size: ui.du(24)
                    color: sheet.who.color || "#087099"
                    photo: sheet.who.photo || ""
                    initials: sheet.who.initials || ""
                    emojiStyle: nav.emojiStyle
                    fontPercent: 300
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    topMargin: ui.du(3)
                    text: sheet.who.name || ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: 150
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    text: sheet.line
                    textFormat: TextFormat.Plain
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                Label {
                    visible: sheet.ended && sheet.reasonWord(sheet.call.reason) != ""
                    horizontalAlignment: HorizontalAlignment.Center
                    text: sheet.reasonWord(sheet.call.reason)
                    textFormat: TextFormat.Plain
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                Container {
                    visible: sheet.ringing
                    horizontalAlignment: HorizontalAlignment.Center
                    topMargin: ui.du(6)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    CallButton {
                        text: qsTr("Decline")
                        fill: "#D93B30"
                        onTapped: engine.request("call.reject", {"id": sheet.call.id})
                    }
                    CallButton {
                        leftMargin: ui.du(3)
                        text: qsTr("Answer")
                        fill: "#2E9E4F"
                        onTapped: engine.request("call.answer", {"id": sheet.call.id})
                    }
                }
                Container {
                    visible: !sheet.ringing && !sheet.ended
                    horizontalAlignment: HorizontalAlignment.Center
                    topMargin: ui.du(6)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    CallButton {
                        text: qsTr("Mute")
                        on: callAudio.muted
                        onTapped: callAudio.muted = !callAudio.muted
                    }
                    CallButton {
                        leftMargin: ui.du(2)
                        text: qsTr("Speaker")
                        on: callAudio.speaker
                        onTapped: callAudio.speaker = !callAudio.speaker
                    }
                    CallButton {
                        leftMargin: ui.du(2)
                        text: qsTr("End")
                        fill: "#D93B30"
                        onTapped: engine.request("call.hangup", {"id": sheet.call.id})
                    }
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
