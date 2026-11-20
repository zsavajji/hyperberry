import bb.cascades 1.4
import hyperberry 1.0

// Settings > Translation: the service that translates messages, and what it needs. Saved as they change; a text
// is saved 300 ms after the last key (start() on a running QTimer starts it over), and at once when its field loses
// the focus or Enter is pressed, so the engine isn't told once per letter.
Page {
    id: page
    property bool deepl: settings.translateProvider == "deepl"
    property bool libre: settings.translateProvider == "libre"
    titleBar: TitleBar {
        title: qsTr("Translation")
    }
    function save() {
        settings.deeplKey = deeplField.text.trim()
        settings.libreURL = urlField.text.trim()
        settings.libreKey = libreKeyField.text.trim()
    }
    attachedObjects: [
        Timer {
            id: saveTimer
            interval: 300
            singleShot: true
            onTimeout: page.save()
        }
    ]
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ScrollView {
            Container {
                topPadding: zoom * ui.du(2)
                leftPadding: zoom * ui.du(2)
                rightPadding: zoom * ui.du(2)
                bottomPadding: zoom * ui.du(2)
                DropDown {
                    title: qsTr("Service")
                    options: [
                        Option { text: "Google"; selected: !page.deepl && !page.libre },
                        Option { text: "DeepL"; selected: page.deepl },
                        Option { text: "LibreTranslate"; selected: page.libre }
                    ]
                    onSelectedIndexChanged: {
                        if (selectedIndex >= 0) {
                            settings.translateProvider = selectedIndex == 1 ? "deepl" : selectedIndex == 2 ? "libre" : "google"
                        }
                    }
                }
                TextField {
                    id: deeplField
                    visible: page.deepl
                    topMargin: zoom * ui.du(2)
                    hintText: qsTr("DeepL API key")
                    text: settings.deeplKey
                    input.flags: TextInputFlag.AutoCapitalizationOff | TextInputFlag.AutoCorrectionOff | TextInputFlag.PredictionOff | TextInputFlag.SpellCheckOff
                    input.onSubmitted: page.save()
                    onTextChanging: saveTimer.start()
                    onFocusedChanged: {
                        if (!focused) {
                            page.save()
                        }
                    }
                }
                TextField {
                    id: urlField
                    visible: page.libre
                    topMargin: zoom * ui.du(2)
                    hintText: qsTr("Server address")
                    inputMode: TextFieldInputMode.Url
                    text: settings.libreURL
                    input.flags: TextInputFlag.AutoCapitalizationOff | TextInputFlag.AutoCorrectionOff | TextInputFlag.PredictionOff | TextInputFlag.SpellCheckOff
                    input.onSubmitted: page.save()
                    onTextChanging: saveTimer.start()
                    onFocusedChanged: {
                        if (!focused) {
                            page.save()
                        }
                    }
                }
                TextField {
                    id: libreKeyField
                    visible: page.libre
                    topMargin: zoom * ui.du(2)
                    hintText: qsTr("API key (optional)")
                    text: settings.libreKey
                    input.flags: TextInputFlag.AutoCapitalizationOff | TextInputFlag.AutoCorrectionOff | TextInputFlag.PredictionOff | TextInputFlag.SpellCheckOff
                    input.onSubmitted: page.save()
                    onTextChanging: saveTimer.start()
                    onFocusedChanged: {
                        if (!focused) {
                            page.save()
                        }
                    }
                }
                Label {
                    topMargin: zoom * ui.du(2)
                    multiline: true
                    text: qsTr("The messages you translate are sent to this service.")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
        }
    }
}
