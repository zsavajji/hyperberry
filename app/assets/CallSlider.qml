import bb.cascades 1.4

// The slide-to-answer bar of the ringing call (step 7.4), as the stock Phone app's: the knob is
// dragged to the green handset on the left to answer, to the red one on the right to decline, and
// slides back to the middle if let go short of either. The track (arcs, handsets, chevrons) is one
// image, since Cascades draws no arcs. Measured in the Q10's 720 px; px() scales them.
Container {
    id: slider
    signal answered
    signal declined
    // Where the finger went down, in window px: the knob follows its distance from here, not the
    // finger's place on the knob, so it doesn't jump.
    property real startX: 0
    // The knob's offset while dragged, kept here rather than read back from translationX.
    property real at: 0
    property bool dragging: false
    // The knob's centre to an end's icon is 260 px; it counts within 40 px of it.
    property real reach: px(260)
    property real grab: px(40)
    preferredWidth: px(665)
    preferredHeight: px(148)
    layout: AbsoluteLayout {}
    function px(n) {
        return ui.du(n / 9)
    }
    ImageView {
        preferredWidth: slider.px(665)
        preferredHeight: slider.px(148)
        scalingMethod: ScalingMethod.AspectFit
        imageSource: "asset:///images/call_track.png"
    }
    // The phone's own handset, tinted green (the track holds only the red one); 66 px centred
    // on the left end's icon.
    ImageView {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: slider.px(40)
            positionY: slider.px(40)
        }
        preferredWidth: slider.px(66)
        preferredHeight: slider.px(66)
        scalingMethod: ScalingMethod.AspectFit
        imageSource: "file:///usr/share/icons/ic_phone.png"
        filterColor: Color.create("#1bd862") // the primary colour, the user's ask (2026-11-07)
    }
    ImageView {
        id: knob
        layoutProperties: AbsoluteLayoutProperties {
            positionX: slider.px(278)
            positionY: slider.px(17)
        }
        preferredWidth: slider.px(110)
        preferredHeight: slider.px(110)
        scalingMethod: ScalingMethod.AspectFit
        imageSource: "asset:///images/call_knob.png"
        // No implicit animation of translationX, which would leave the knob trailing the finger;
        // the slide back is the explicit one below.
        attachedObjects: ImplicitAnimationController {
            propertyName: "translationX"
            enabled: false
        }
        animations: TranslateTransition {
            id: back
            toX: 0
            duration: 150
        }
    }
    // The whole track takes the drag, not the knob: on the Q10 (0.0.1.184) the knob alone locked
    // under the finger. Touches on the images bubble up to here, wherever on the track the finger
    // goes. The knob follows the finger's distance from where it went down; the 220 px needed to
    // reach an end are the guard against a brush in the pocket.
    onTouch: {
        if (event.isDown()) {
            dragging = true
            back.stop()
            startX = event.windowX
            console.log("call slider: down")
        } else if (event.isMove() && dragging) {
            at = Math.max(-reach, Math.min(reach, event.windowX - startX))
            knob.translationX = at
        } else if ((event.isUp() || event.isCancel()) && dragging) {
            dragging = false
            var end = event.isCancel() ? 0 : at
            console.log("call slider: let go at " + Math.round(end) + (event.isCancel() ? " (cancelled)" : ""))
            at = 0
            back.play()
            if (end <= -(reach - grab)) {
                answered()
            } else if (end >= reach - grab) {
                declined()
            }
        }
    }
}
