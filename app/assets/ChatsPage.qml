import bb.cascades 1.4

// A placeholder: step 2.4 replaces it with the chat list.
Page {
    titleBar: TitleBar {
        title: qsTr("Chats")
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        Label {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            multiline: true
            textStyle.textAlign: TextAlign.Center
            text: !engine.connected ? qsTr("Starting…") : qsTr("Linked as %1\nWhatsApp: %2").arg(engine.link.name || engine.link.me || "").arg(engine.conn)
        }
    }
}
