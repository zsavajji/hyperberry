import bb.cascades 1.4

// Over the action bar while a voice note records (step 4.4b), where "Replying to" sits. The
// composer's status line was meant to run the counter, but it never showed on the Q10 (0.0.1.81).
Container {
    property string clock
    horizontalAlignment: HorizontalAlignment.Fill
    background: ui.palette.plain
    leftPadding: zoom * ui.du(1.5)
    rightPadding: zoom * ui.du(1.5)
    topPadding: zoom * ui.du(1)
    bottomPadding: zoom * ui.du(1)
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    Label {
        verticalAlignment: VerticalAlignment.Center
        text: "●"
        textStyle.color: Color.create("#FF3333")
    }
    Label {
        verticalAlignment: VerticalAlignment.Center
        text: qsTr("Recording %1").arg(clock)
        textFormat: TextFormat.Plain
        textStyle.fontSize: FontSize.PercentageValue
        textStyle.fontSizeValue: nav.bodySize
    }
}
