import bb.cascades 1.4
import "errors.js" as Errors

// Linking by pairing code (PLAN.md §7): type the phone's number, the engine returns a code, and
// the linking page below shows it in large type.
Page {
    id: page
    property bool asking: false
    property string failedCmd: engine.failure.cmd || ""
    property string linkState: engine.link.state || ""

    titleBar: TitleBar {
        title: qsTr("Link with phone number")
    }
    actions: [
        ActionItem {
            title: qsTr("Get code")
            ActionBar.placement: ActionBarPlacement.Signature
            enabled: !page.asking && number.text != ""
            onTriggered: page.submit()
        }
    ]

    function submit() {
        if (number.text == "" || asking)
            return
        asking = true
        engine.request("link.code", {"phone": number.text})
    }
    // The request ended in failure; on success the state change pops this page.
    onFailedCmdChanged: {
        if (failedCmd == "link.code")
            asking = false
    }
    onLinkStateChanged: {
        if (linkState == "code")
            nav.pop()
    }
    onCreationCompleted: number.requestFocus()

    ScrollView {
        Container {
            topPadding: ui.du(2)
            bottomPadding: ui.du(2)
            leftPadding: ui.du(2)
            rightPadding: ui.du(2)
            horizontalAlignment: HorizontalAlignment.Fill

            Label {
                multiline: true
                text: qsTr("Type the number of the phone that has WhatsApp, with the country code.")
            }
            TextField {
                id: number
                inputMode: TextFieldInputMode.PhoneNumber
                hintText: qsTr("+39 333 123 4567")
                input.submitKey: SubmitKey.Submit
                input.onSubmitted: page.submit()
            }
            Container {
                visible: page.asking
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                ActivityIndicator {
                    running: page.asking
                    preferredWidth: ui.du(6)
                    preferredHeight: ui.du(6)
                }
                Label {
                    text: qsTr("Asking WhatsApp for a code…")
                }
            }
            Label {
                visible: engine.failure.cmd == "link.code"
                multiline: true
                text: Errors.errorText(engine.failure)
            }
        }
    }
}
