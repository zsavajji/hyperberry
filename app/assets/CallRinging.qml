import bb.cascades 1.4

// The controls of a ringing call (step 7.4): silence the ringtone, answer with a message (which
// declines), and the slide bar. Over the call screen's 720 px stage, whose size px() scales.
Container {
    id: ringing
    property bool silenced: false
    signal silence
    signal message
    signal answered
    signal declined
    preferredWidth: px(720)
    preferredHeight: px(720)
    layout: AbsoluteLayout {}
    function px(n) {
        return ui.du(n / 9)
    }
    // A round button: its disc and the white glyph on it.
    Container {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: ringing.px(308)
            positionY: ringing.px(419)
        }
        opacity: ringing.silenced ? 0.35 : 1
        layout: DockLayout {}
        ImageView {
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.Fill
            imageSource: "asset:///images/call_btn.png"
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ringing.px(50)
            preferredHeight: ringing.px(50)
            scalingMethod: ScalingMethod.Fill
            imageSource: "asset:///images/call_silence.png"
        }
        gestureHandlers: TapHandler {
            onTapped: {
                if (!ringing.silenced) {
                    ringing.silenced = true
                    ringing.silence()
                }
            }
        }
    }
    Container {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: ringing.px(588)
            positionY: ringing.px(419)
        }
        layout: DockLayout {}
        ImageView {
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.Fill
            imageSource: "asset:///images/call_btn.png"
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ringing.px(50)
            preferredHeight: ringing.px(50)
            scalingMethod: ScalingMethod.Fill
            imageSource: "asset:///images/call_message.png"
        }
        gestureHandlers: TapHandler {
            onTapped: ringing.message()
        }
    }
    CallSlider {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: ringing.px(27)
            positionY: ringing.px(554)
        }
        onAnswered: ringing.answered()
        onDeclined: ringing.declined()
    }
}
