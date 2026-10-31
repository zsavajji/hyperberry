import bb.cascades 1.4

// A voice note in a card (step 4.3b), as Telega lays it out: the disc on the left; the waveform
// over the length on the right. The waveform (Waveform.cpp) is grey, and over it an image of only
// the bars played, in the primary colour. A clipping container as wide as the part played shrank
// its image instead, which then slid over the grey one (0.0.1.79). view is the chat page's list,
// which holds the player's state (playingId, loadingId, paused, posMs, durMs).
Container {
    id: bubble
    property variant msg
    property variant view
    property bool playing: view.playingId == msg.id
    property bool loading: view.loadingId == msg.id
    // The note's own length until the player knows it.
    property int total: playing && view.durMs > 0 ? view.durMs : msg.duration * 1000
    property real progress: playing && total > 0 ? Math.min(1, view.posMs / total) : 0
    // An int, so the played image is drawn again only when a bar changes: at most 64 times.
    property int bars: Math.ceil(progress * 64)
    topMargin: 0
    bottomMargin: 0
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }

    function clock(seconds) {
        return Math.floor(seconds / 60) + ":" + ("0" + seconds % 60).slice(-2)
    }

    Container {
        layout: DockLayout {
        }
        preferredWidth: 72
        preferredHeight: 72
        verticalAlignment: VerticalAlignment.Center
        ImageView {
            visible: !bubble.loading
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            imageSource: bubble.msg.mediaState == 3 && !bubble.playing ? "asset:///images/retry.png"
                         : bubble.playing && !bubble.view.paused ? "asset:///images/pause.png"
                         : "asset:///images/play.png"
        }
        ActivityIndicator {
            visible: bubble.loading
            running: bubble.loading
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
        }
        gestureHandlers: TapHandler {
            onTapped: bubble.view.playVoice(bubble.msg)
        }
    }
    Container {
        leftMargin: ui.du(1.5)
        verticalAlignment: VerticalAlignment.Center
        Container {
            layout: DockLayout {
            }
            ImageView {
                preferredWidth: 381
                preferredHeight: 45
                scalingMethod: ScalingMethod.None
                image: bubble.msg.wave
                filterColor: ui.palette.secondaryTextOnPlain
            }
            ImageView {
                visible: bubble.bars > 0
                preferredWidth: 381
                preferredHeight: 45
                scalingMethod: ScalingMethod.None
                image: bubble.bars > 0 ? bubble.view.playedWave(bubble.msg.waveData, bubble.bars) : bubble.msg.wave
                filterColor: ui.palette.primary
            }
        }
        Container {
            topMargin: ui.du(0.5)
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            ImageView {
                verticalAlignment: VerticalAlignment.Center
                imageSource: "asset:///images/mic.png"
                filterColor: bubble.msg.played ? ui.palette.primary : ui.palette.secondaryTextOnPlain
            }
            Label {
                verticalAlignment: VerticalAlignment.Center
                leftMargin: ui.du(0.5)
                text: bubble.clock(bubble.playing ? Math.max(0, Math.ceil((bubble.total - bubble.view.posMs) / 1000)) : bubble.msg.duration)
                textFormat: TextFormat.Plain
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
        }
    }
}
