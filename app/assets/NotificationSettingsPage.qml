import bb.cascades 1.4

// Settings > Notifications: saved as they change. Each lives in `settings`.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Notifications")
    }
    ScrollView {
        Container {
            topPadding: ui.du(2)
            leftPadding: ui.du(2)
            rightPadding: ui.du(2)
            bottomPadding: ui.du(2)
            Container {
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
                        text: qsTr("Start at boot")
                        bottomMargin: 0
                    }
                    Label {
                        text: qsTr("Brings HyperBerry back within 6 minutes of a reboot, or if it stops, with the app closed.")
                        multiline: true
                        topMargin: 0
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
                ToggleButton {
                    verticalAlignment: VerticalAlignment.Center
                    checked: settings.startAtBoot
                    onCheckedChanged: settings.startAtBoot = checked
                }
            }
        }
    }
}
