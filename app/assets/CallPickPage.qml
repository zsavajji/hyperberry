import bb.cascades 1.4
import bb.system 1.2
import hyperberry 1.0

// Who to call in a group (step 7d.G4): its members, ours left out, ticked as NewGroupPage ticks
// contacts, for a voice call or a video one (step 7e). Both stay off below two ticked; they ask first
// when everyone or more than 5 are ticked. done fires once call.start is sent (a failure is worded by
// main.qml's toast); the pusher pops.
Page {
    id: page
    property string chat
    // Which call the confirm dialog is for.
    property bool pendingVideo: false
    signal done
    // The pusher calls this before the push: chat is set first, then the members are asked for.
    function load(jid) {
        chat = jid
        picker.loadGroup(jid)
    }
    function confirmCall(video) {
        pendingVideo = video
        if (picker.chosenCount == picker.total || picker.chosenCount > 5) {
            sure.show()
        } else {
            place()
        }
    }
    function place() {
        if (picker.startGroupCall(pendingVideo) == "") {
            notSent.show()
            return
        }
        done()
    }
    titleBar: TitleBar {
        title: qsTr("Call")
    }
    actions: [
        ActionItem {
            title: qsTr("Voice call")
            imageSource: "file:///usr/share/icons/ic_phone.png"
            ActionBar.placement: ActionBarPlacement.Signature
            enabled: picker.chosenCount >= 2
            onTriggered: page.confirmCall(false)
        },
        ActionItem {
            title: qsTr("Video call")
            imageSource: "file:///usr/share/icons/ic_chat_video.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            enabled: picker.chosenCount >= 2
            onTriggered: page.confirmCall(true)
        }
    ]
    attachedObjects: [
        ContactPicker {
            id: picker
            link: engine
        },
        SystemToast {
            id: tooMany
            body: qsTr("At most 31 people in a call")
        },
        SystemToast {
            id: notSent
            body: qsTr("Not sent: HyperBerry is reconnecting. Try again in a moment.")
        },
        SystemDialog {
            id: sure
            title: qsTr("Call %1 people?").arg(picker.chosenCount)
            confirmButton.label: qsTr("Call")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    page.place()
                }
            }
        }
    ]
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            // Padding, not margins: margins only space siblings, never from the container's edge.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                topPadding: ui.du(1.5)
                bottomPadding: ui.du(1.5)
                TextField {
                    hintText: qsTr("Search members")
                    input.submitKey: SubmitKey.Search
                    onTextChanging: picker.filter(text)
                }
                Label {
                    visible: picker.chosenCount > 0
                    textStyle.color: ui.palette.secondaryTextOnPlain
                    text: qsTr("%1 chosen").arg(picker.chosenCount)
                }
            }
            ListView {
                // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
                property variant emojiStyle: nav.emojiStyle
                property int bodySize: nav.bodySize
                property int smallSize: nav.smallSize
                property string pressColor: nav.pressColor
                dataModel: picker.model
                onTriggered: {
                    if (!picker.toggle(indexPath)) {
                        tooMany.show()
                    }
                }
                listItemComponents: ListItemComponent {
                    type: ""
                    ContactRow {
                        contact: ListItemData
                    }
                }
            }
        }
        Busy {
            running: !picker.loaded
            text: qsTr("Loading…")
        }
    }
}
