import bb.cascades 1.4

// A toggle of the in-call grid (step 7.4), a cell as the stock Phone app's (Q10 screenshot,
// 2026-11-07): 222 x 90 px, a white icon and its label, on the grey #565656 while pressed or on.
// iconOn and textOn, when given, replace them while on (Mute shows the mic and "Unmute").
// Sizes in the Q10's px, scaled by px().
Container {
    id: toggle
    property string icon
    property string text
    property string iconOn: icon
    property string textOn: text
    property bool on: false
    property bool pressed: false
    signal tapped
    preferredWidth: px(222)
    preferredHeight: px(90)
    leftPadding: px(18)
    background: toggle.on || toggle.pressed ? Color.create("#565656") : Color.Transparent
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    function px(n) {
        return ui.px(n * screenWidth / 720)
    }
    ImageView {
        verticalAlignment: VerticalAlignment.Center
        preferredWidth: toggle.px(50)
        preferredHeight: toggle.px(50)
        scalingMethod: ScalingMethod.AspectFit
        imageSource: toggle.on ? toggle.iconOn : toggle.icon
    }
    Label {
        verticalAlignment: VerticalAlignment.Center
        leftMargin: toggle.px(20)
        preferredWidth: toggle.px(134)
        multiline: true
        text: toggle.on ? toggle.textOn : toggle.text
        textFormat: TextFormat.Plain
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: 80
        textStyle.color: Color.create("#e8e8e8")
    }
    onTouch: toggle.pressed = event.isDown() || event.isMove()
    onTouchExit: toggle.pressed = false
    gestureHandlers: TapHandler {
        onTapped: toggle.tapped()
    }
}
