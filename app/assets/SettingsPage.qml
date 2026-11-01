import bb.cascades 1.4
import bb.system 1.2

// Settings (step 5.4), saved as they change, so there's no Save button. Each lives in `settings`.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Settings")
    }
    attachedObjects: SystemDialog {
        id: unlinkDialog
        title: qsTr("Unlink this device?")
        body: qsTr("This device is removed from your linked devices and its chats are deleted here.")
        confirmButton.label: qsTr("Unlink")
        cancelButton.label: qsTr("Cancel")
        onFinished: {
            if (value == SystemUiResult.ConfirmButtonSelection)
                engine.request("link.logout", {})
        }
    }
    ScrollView {
        Container {
            topPadding: ui.du(2)
            leftPadding: ui.du(2)
            rightPadding: ui.du(2)
            bottomPadding: ui.du(2)
            DropDown {
                title: qsTr("Theme")
                options: [
                    Option { text: qsTr("Dark"); selected: settings.theme == 0 },
                    Option { text: qsTr("Bright"); selected: settings.theme == 1 }
                ]
                onSelectedIndexChanged: settings.theme = selectedIndex
            }
            DropDown {
                title: qsTr("Text size")
                options: [
                    Option { text: qsTr("Small"); selected: settings.textSize == 0 },
                    Option { text: qsTr("Normal"); selected: settings.textSize == 1 },
                    Option { text: qsTr("Large"); selected: settings.textSize == 2 }
                ]
                onSelectedIndexChanged: settings.textSize = selectedIndex
            }
            Container {
                topMargin: ui.du(3)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    verticalAlignment: VerticalAlignment.Center
                    Label {
                        text: qsTr("Enter sends")
                        bottomMargin: 0
                    }
                    Label {
                        text: qsTr("Off, Enter starts a new line; the Send action still sends.")
                        multiline: true
                        topMargin: 0
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
                ToggleButton {
                    verticalAlignment: VerticalAlignment.Center
                    checked: settings.enterSends
                    onCheckedChanged: settings.enterSends = checked
                }
            }
            Container {
                topMargin: ui.du(3)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    verticalAlignment: VerticalAlignment.Center
                    Label {
                        text: qsTr("Notification preview")
                        bottomMargin: 0
                    }
                    Label {
                        text: qsTr("Off, a notification says \"New message\" instead of the text.")
                        multiline: true
                        topMargin: 0
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
                ToggleButton {
                    verticalAlignment: VerticalAlignment.Center
                    checked: settings.notifyPreview
                    onCheckedChanged: settings.notifyPreview = checked
                }
            }
            Button {
                topMargin: ui.du(4)
                horizontalAlignment: HorizontalAlignment.Fill
                text: qsTr("Unlink this device")
                enabled: engine.linked
                onClicked: unlinkDialog.show()
            }
        }
    }
}
