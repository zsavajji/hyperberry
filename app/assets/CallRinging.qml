import bb.cascades 1.4

// The controls of a ringing call (step 7.4): on a video call, whether to answer with our video (the
// user's idea, 2026-11-07, where the stock app's silence button was); answer with a message (which
// declines); and the slide bar. Over the call screen's 720 px stage, whose size px() scales. The two buttons take the
// Classic's trackpad focus (the user, 2026-11-20), lit as a grey disc (#565656, the call toggles' grey) while they have it,
// and a click (Enter) taps them.
Container {
    id: ringing
    // The call offers video; answering sends ours while withVideo (the sheet sets it on as offered).
    property bool video: false
    property bool withVideo: true
    signal message
    signal answered
    signal declined
    preferredWidth: px(720)
    preferredHeight: px(720)
    layout: AbsoluteLayout {}
    function px(n) {
        return ui.px(n * screenWidth / 720)
    }
    // A round button: its disc and the white glyph on it.
    Container {
        id: videoBtn
        layoutProperties: AbsoluteLayoutProperties {
            positionX: ringing.px(308)
            positionY: ringing.px(419)
        }
        focusPolicy: FocusPolicy.KeyAndTouch
        visible: ringing.video
        layout: DockLayout {}
        ImageView {
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/call_btn.png"
        }
        ImageView {
            visible: videoBtn.focused
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/dot.png"
            filterColor: Color.create("#565656")
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ringing.px(50)
            preferredHeight: ringing.px(50)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: ringing.withVideo ? "asset:///images/call_video.png" : "asset:///images/call_video_off.png"
        }
        gestureHandlers: TapHandler {
            onTapped: ringing.withVideo = !ringing.withVideo
        }
        keyListeners: KeyListener {
            onKeyReleased: {
                if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                    ringing.withVideo = !ringing.withVideo
                }
            }
        }
    }
    Container {
        id: msgBtn
        layoutProperties: AbsoluteLayoutProperties {
            positionX: ringing.px(588)
            positionY: ringing.px(419)
        }
        focusPolicy: FocusPolicy.KeyAndTouch
        layout: DockLayout {}
        ImageView {
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/call_btn.png"
        }
        ImageView {
            visible: msgBtn.focused
            preferredWidth: ringing.px(104)
            preferredHeight: ringing.px(104)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/dot.png"
            filterColor: Color.create("#565656")
        }
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: ringing.px(50)
            preferredHeight: ringing.px(50)
            scalingMethod: ScalingMethod.AspectFit
            imageSource: "asset:///images/call_message.png"
        }
        gestureHandlers: TapHandler {
            onTapped: ringing.message()
        }
        keyListeners: KeyListener {
            onKeyReleased: {
                if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                    ringing.message()
                }
            }
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
