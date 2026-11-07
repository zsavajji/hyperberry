import bb.cascades 1.4

// A toggle of the in-call grid (step 7.4): a white icon with a label beside it, both in the primary
// colour while `on`. Sizes in the Q10's px, scaled by px().
Container {
    id: toggle
    property string icon
    property string text
    property bool on: false
    signal tapped
    preferredWidth: px(220)
    preferredHeight: px(70)
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    function px(n) {
        return ui.du(n / 9)
    }
    ImageView {
        verticalAlignment: VerticalAlignment.Center
        preferredWidth: toggle.px(50)
        preferredHeight: toggle.px(50)
        scalingMethod: ScalingMethod.AspectFit
        imageSource: toggle.icon
        filterColor: toggle.on ? ui.palette.primary : Color.White
    }
    Label {
        verticalAlignment: VerticalAlignment.Center
        // The icon is 50 px wide and the label starts 70 px from its left edge.
        leftMargin: toggle.px(20)
        preferredWidth: toggle.px(140)
        multiline: true
        text: toggle.text
        textFormat: TextFormat.Plain
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: 80
        textStyle.color: toggle.on ? ui.palette.primary : Color.create("#e8e8e8")
    }
    gestureHandlers: TapHandler {
        onTapped: toggle.tapped()
    }
}
