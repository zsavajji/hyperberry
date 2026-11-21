import bb.cascades 1.4
import "languages.js" as Languages

// A message's translation (long-press > Translate), or its original when the card shows the translation: a dimmed
// page with a card that scrolls, over the chat. chatCtl is the page's ChatController and defaultLang the language
// to translate into at first (the chat's, else the phone's); a select in the card picks another, per message, and lang
// is the one now showing. The page connects the controller's translated and translateFailed to got and failed.
Dialog {
    id: dlg
    property variant chatCtl
    property string defaultLang
    property string lang
    // True while the code sets the select, whose change is then not the user's pick.
    property bool setting: false
    // wait: the spinner, text: the message, fail: a line
    property string mode: "wait"
    property string msgId: ""
    property string heading: ""
    property string body: ""
    // The message as it was sent: what the select's first option, Original, shows.
    property string original: ""
    // The screen's height, from the dim layer that fills it: the card takes at most 80 % of it.
    property real screenH: 0
    // languages.js's name; the phone's language when the list has it not (nb, from a Norwegian phone) from QLocale.
    function nameOf(code) {
        return Languages.name(code) || chatCtl.languageName(code)
    }
    function show(title, text, how) {
        heading = title
        body = text
        mode = how
        open()
    }
    // The card shows the translation already: the dialog tells what was sent, with no request.
    function showOriginal(msg) {
        msgId = msg.id
        original = msg.original
        lang = ""
        select("")
        show(qsTr("Original"), original, "text")
    }
    function ask(msg) {
        msgId = msg.id
        original = msg.text
        lang = defaultLang
        select(lang)
        show(nameOf(lang), "", "wait")
        chatCtl.translate(msg.id, lang)
    }
    // The select shows code: "" is its first option, Original (the select can't be left empty with certainty on 10.3).
    function option(text, value) {
        var o = optionDef.createObject()
        o.text = text
        o.value = value
        return o
    }
    function select(code) {
        setting = true
        for (var i = 0; i < langs.count(); i++) {
            if (langs.at(i).value == code) {
                langs.selectedIndex = i
            }
        }
        setting = false
    }
    // The user picked a language: the same message again, in it. Original is the text as sent, with no request
    // (lang "" also drops a reply still on its way).
    function pick(code) {
        lang = code
        if (code == "") {
            heading = qsTr("Original")
            body = original
            mode = "text"
            return
        }
        heading = nameOf(code)
        body = ""
        mode = "wait"
        chatCtl.translate(msgId, code)
    }
    // A reply for a language picked before this one is not the dialog's.
    function got(id, text, from, to) {
        if (id != msgId || mode != "wait" || to != lang) {
            return
        }
        var name = nameOf(to)
        heading = from ? nameOf(from) + " → " + name : name
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
    attachedObjects: ComponentDefinition {
        id: optionDef
        Option {}
    }
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
            // The select is the scrolling content's first child: its open list (nearly fifty options, taller than the card on
            // a Q10) scrolls with the text, and is never outside the ScrollView.
            ScrollView {
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                Container {
                    DropDown {
                        id: langs
                        title: qsTr("Translate to")
                        // Made once. The phone's language comes first when the list has no such language.
                        onCreationCompleted: {
                            var all = Languages.list()
                            var found = false
                            for (var i = 0; i < all.length; i++) {
                                found = found || all[i][0] == dlg.defaultLang
                            }
                            add(dlg.option(qsTr("Original"), ""))
                            if (dlg.defaultLang != "" && !found) {
                                add(dlg.option(dlg.nameOf(dlg.defaultLang), dlg.defaultLang))
                            }
                            for (var j = 0; j < all.length; j++) {
                                add(dlg.option(all[j][1], all[j][0]))
                            }
                        }
                        onSelectedIndexChanged: {
                            if (dlg.setting || selectedIndex < 0) {
                                return
                            }
                            dlg.pick(at(selectedIndex).value)
                        }
                    }
                    Busy {
                        visible: dlg.mode == "wait"
                        running: dlg.mode == "wait"
                        topMargin: zoom * ui.du(4)
                        bottomMargin: zoom * ui.du(4)
                    }
                    Label {
                        visible: dlg.mode != "wait"
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
