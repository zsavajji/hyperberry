import bb.cascades 1.4
import bb.multimedia 1.4

// The chat page's one voice-note player (step 4.3b). Starting a note stops the one playing; the
// page's destruction ends it. The bubbles read playingId, loadingId, paused, posMs and durMs
// through the page's list. ponytail: the speaker only; the earpiece and proximity come later.
MediaPlayer {
    id: player
    property string jid
    property string playingId: ""
    property string loadingId: ""
    // The voice.play request in flight: a later tap replaces it, and its reply is dropped.
    property string req: ""
    property bool paused: mediaState == MediaState.Paused
    property int posMs: Number(position) || 0
    property int durMs: Number(duration) || 0
    // Anything but a failed download, which the bubble shows as retry.
    signal failed
    // No engine: the request never went.
    signal offline
    statusInterval: 100
    onPlaybackCompleted: {
        stop()
        playingId = ""
    }

    function toggle(id) {
        if (playingId == id) {
            if (paused) {
                play()
            } else {
                pause()
            }
            return
        }
        stop()
        playingId = ""
        req = engine.request("voice.play", { "chat": jid, "id": id })
        if (req == "") {
            offline()
            return
        }
        loadingId = id
    }
    // ChatController's voiceReady: the WAV of the request in flight.
    function ready(id, path) {
        if (id != req || loadingId == "") {
            return
        }
        req = ""
        sourceUrl = "file://" + path
        playingId = loadingId
        loadingId = ""
        if (play() != MediaError.None) {
            playingId = ""
            failed()
        }
    }
    function fail(code) {
        loadingId = ""
        req = ""
        if (code != "download_failed") {
            failed()
        }
    }
}
