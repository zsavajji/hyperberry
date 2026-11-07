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
        scalingMethod: ScalingMethod.Fill
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
        scalingMethod: ScalingMethod.Fill
        imageSource: "file:///usr/share/icons/ic_phone.png"
        filterColor: Color.create("#709830")
    }
    ImageView {
        id: knob
        layoutProperties: AbsoluteLayoutProperties {
            positionX: slider.px(278)
            positionY: slider.px(17)
        }
        preferredWidth: slider.px(110)
        preferredHeight: slider.px(110)
        scalingMethod: ScalingMethod.Fill
        imageSource: "asset:///images/call_knob.png"
        // Cascades animates every translationX change by itself, which would leave the knob
        // trailing the finger: off while dragging, on again so a short release slides it back.
        attachedObjects: ImplicitAnimationController {
            id: follow
            propertyName: "translationX"
        }
        onTouch: {
            if (event.isDown()) {
                follow.enabled = false
                slider.startX = event.windowX
            } else if (event.isMove()) {
                knob.translationX = Math.max(-slider.reach, Math.min(slider.reach, event.windowX - slider.startX))
            } else if (event.isUp() || event.isCancel()) {
                var at = knob.translationX
                follow.enabled = true
                knob.translationX = 0
                if (event.isCancel()) {
                    return
                }
                if (at <= -(slider.reach - slider.grab)) {
                    slider.answered()
                } else if (at >= slider.reach - slider.grab) {
                    slider.declined()
                }
            }
        }
    }
}
