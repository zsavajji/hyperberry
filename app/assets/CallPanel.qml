import bb.cascades 1.4

// The controls of a call that isn't ringing (step 7.4): the black panel with Speaker and Mute (a
// WhatsApp call has no keypad, note or second call), and the red End call bar. Over the call
// screen's 720 px stage, whose size px() scales.
Container {
    id: panel
    property bool ended: false
    property bool pressed: false
    signal hangup
    preferredWidth: px(720)
    preferredHeight: px(720)
    layout: AbsoluteLayout {}
    function px(n) {
        return ui.du(n / 9)
    }
    Container {
        layoutProperties: AbsoluteLayoutProperties {
            positionY: panel.px(415)
        }
        preferredWidth: panel.px(720)
        preferredHeight: panel.px(305)
        background: Color.Black
    }
    CallToggle {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: panel.px(36)
            positionY: panel.px(442)
        }
        icon: "asset:///images/call_speaker.png"
        text: qsTr("Speaker")
        on: callAudio.speaker
        onTapped: callAudio.speaker = !callAudio.speaker
    }
    CallToggle {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: panel.px(272)
            positionY: panel.px(442)
        }
        icon: "asset:///images/call_mute.png"
        text: qsTr("Mute")
        on: callAudio.muted
        onTapped: callAudio.muted = !callAudio.muted
    }
    Container {
        layoutProperties: AbsoluteLayoutProperties {
            positionX: panel.px(18)
            positionY: panel.px(610)
        }
        preferredWidth: panel.px(684)
        preferredHeight: panel.px(93)
        enabled: !panel.ended
        opacity: panel.ended ? 0.5 : 1
        background: panel.pressed ? Color.create("#b3181a") : Color.create("#ff3334")
        layout: DockLayout {}
        ImageView {
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Center
            leftMargin: panel.px(14)
            preferredWidth: panel.px(62)
            preferredHeight: panel.px(22)
            scalingMethod: ScalingMethod.Fill
            imageSource: "asset:///images/call_end.png"
        }
        Label {
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Center
            leftMargin: panel.px(96)
            text: qsTr("End call")
            textFormat: TextFormat.Plain
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: 80
            textStyle.color: Color.White
        }
        onTouch: panel.pressed = event.isDown() || event.isMove()
        onTouchExit: panel.pressed = false
        gestureHandlers: TapHandler {
            onTapped: panel.hangup()
        }
    }
}
