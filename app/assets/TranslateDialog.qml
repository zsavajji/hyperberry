import bb.cascades 1.4

// A message's translation (long-press > Translate), or its original when the card shows the translation: a dimmed
// page with a card that scrolls, over the chat. chatCtl is the page's ChatController and lang the language to
// translate into; the page connects the controller's translated and translateFailed to got and failed.
Dialog {
    id: dlg
    property variant chatCtl
    property string lang
    // wait: the spinner, text: the message, fail: a line
    property string mode: "wait"
    property string msgId: ""
    property string heading: ""
    property string body: ""
    // The screen's height, from the dim layer that fills it: the card takes at most 80 % of it.
    property real screenH: 0
    function show(title, text, how) {
        heading = title
        body = text
        mode = how
        open()
    }
    // The card shows the translation already: the dialog tells what was sent, with no request.
    function showOriginal(msg) {
        msgId = msg.id
        show(qsTr("Original"), msg.original, "text")
    }
    function ask(msg) {
        msgId = msg.id
        show(chatCtl.languageName(lang), "", "wait")
        chatCtl.translate(msg.id, lang)
    }
    function got(id, text, from, to) {
        if (id != msgId || mode != "wait") {
            return
        }
        var name = chatCtl.languageName(to)
        heading = from ? chatCtl.languageName(from) + " → " + name : name
        if (text == "") {
            body = qsTr("Already in %1").arg(name)
            mode = "fail"
        } else {
            body = text
            mode = "text"
        }
    }
    function failed(code) {
        if (mode != "wait") {
            return
        }
        body = code == "no_key" ? qsTr("Set up the translation service in Settings")
             : code == "unsupported" ? qsTr("This service can't translate into this language")
             : code == "rate_limited" ? qsTr("The translation service is busy. Try again later.")
             : code == "offline" || code == "no_engine" ? qsTr("Couldn't reach the translation service")
             : qsTr("Couldn't translate this message")
        mode = "fail"
    }
    onOpened: closeButton.requestFocus()
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        // The dim layer is its own sibling under the card: a tap on the card never reaches it.
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            background: Color.create("#B3000000")
            gestureHandlers: TapHandler {
                onTapped: dlg.close()
            }
            attachedObjects: LayoutUpdateHandler {
                onLayoutFrameChanged: dlg.screenH = layoutFrame.height
            }
        }
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Center
            leftMargin: zoom * ui.du(2)
            rightMargin: zoom * ui.du(2)
            maxHeight: dlg.screenH > 0 ? dlg.screenH * 0.8 : 100000
            background: nav.pageBackground
            topPadding: zoom * ui.du(2)
            bottomPadding: zoom * ui.du(2)
            leftPadding: zoom * ui.du(2)
            rightPadding: zoom * ui.du(2)
            Label {
                text: dlg.heading
                textFormat: TextFormat.Plain
                textStyle.base: nav.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: nav.bodySize * 1.1
                textStyle.fontWeight: FontWeight.W500
            }
            // The wait, or the text with room to scroll.
            Busy {
                visible: dlg.mode == "wait"
                running: dlg.mode == "wait"
                topMargin: zoom * ui.du(4)
                bottomMargin: zoom * ui.du(4)
            }
            ScrollView {
                visible: dlg.mode != "wait"
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                Container {
                    Label {
                        multiline: true
                        text: dlg.body
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: dlg.mode == "fail" ? ui.palette.secondaryTextOnPlain : ui.palette.textOnPlain
                    }
                }
            }
            Container {
                topMargin: zoom * ui.du(2)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Button {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    text: qsTr("Copy")
                    enabled: dlg.mode == "text"
                    onClicked: {
                        dlg.chatCtl.copy(dlg.body)
                        dlg.close()
                    }
                }
                Button {
                    id: closeButton
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    text: qsTr("Close")
                    onClicked: dlg.close()
                }
            }
        }
    }
}
