import bb.cascades 1.4

// A call-screen button (step 7.4): a coloured block with a word. `on` marks a toggle that is
// switched on (it takes the primary colour).
Container {
    id: button
    property string text
    property string fill: "#505050"
    property bool on: false
    signal tapped
    preferredWidth: ui.du(14)
    preferredHeight: ui.du(9.4)
    layout: DockLayout {}
    background: button.on ? ui.palette.primary : Color.create(button.fill)
    gestureHandlers: TapHandler {
        onTapped: button.tapped()
    }
    Label {
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Center
        text: button.text
        textFormat: TextFormat.Plain
        textStyle.color: Color.White
    }
}
