import bb.cascades 1.4
import bb.cascades.pickers 1.0
import bb.system 1.2
import hyperberry 1.0

// Settings (step 5.4), saved as they change, so there's no Save button. Each lives in `settings`.
// My profile (step 5.9) is at the top, from the engine through this page's own Profile.
Page {
    id: page
    property string cacheText: ""
    property variant info: profile.info
    property bool loaded: info.phone !== undefined
    property bool failed: !loaded && engine.failure.cmd == "profile.get"
    property bool hasPicture: !!info.picture
    // Bumped on each reply: the picture's path is always the same, and the ImageView would
    // otherwise show the first one it loaded.
    property int rev: 0
    onInfoChanged: rev++
    titleBar: TitleBar {
        title: qsTr("Settings")
    }
    // Read when the page opens and after a clear; the walk of data/cache/ is a few files.
    onCreationCompleted: {
        cacheText = settings.cacheSize()
        // Not a Connections: Cascades' QML has none. The signal lives and dies with the page.
        profile.failed.connect(profileFailedToast.show)
        if (engine.linked) {
            profile.ask()
        }
    }
    function changePhoto() {
        if (!loaded) {
            return
        }
        photoDialog.clearList()
        photoDialog.appendItem(qsTr("Change photo"), true, true)
        if (hasPicture) {
            photoDialog.appendItem(qsTr("Remove photo"))
        }
        photoDialog.show()
    }
    function editName() {
        namePrompt.inputField.defaultText = info.name || ""
        namePrompt.show()
    }
    function editAbout() {
        aboutPrompt.inputField.defaultText = info.about || ""
        aboutPrompt.show()
    }
    attachedObjects: [
        Profile {
            id: profile
            link: engine
        },
        // The system's picker with its crop tool; Profile makes the 640x640 JPEG from the crop.
        FilePicker {
            id: photoPicker
            type: FileType.Picture
            mode: FilePickerMode.Picker
            imageCropEnabled: true
            title: qsTr("Change photo")
            onFileSelected: {
                if (!profile.setPhoto(selectedFiles[0])) {
                    unreadableToast.show()
                }
            }
        },
        SystemListDialog {
            id: photoDialog
            title: qsTr("Profile photo")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value != SystemUiResult.ConfirmButtonSelection) {
                    return
                }
                if (selectedIndices[0] == 0) {
                    photoPicker.open()
                } else {
                    profile.removePhoto()
                }
            }
        },
        SystemPrompt {
            id: namePrompt
            title: qsTr("Your name")
            inputField.maximumLength: 25
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    profile.setName(inputFieldTextEntry())
                }
            }
        },
        SystemPrompt {
            id: aboutPrompt
            title: qsTr("About")
            inputField.maximumLength: 139
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    profile.setAbout(inputFieldTextEntry())
                }
            }
        },
        SystemToast {
            id: profileFailedToast
            body: qsTr("Couldn't update your profile. Check the connection and try again.")
        },
        SystemToast {
            id: unreadableToast
            body: qsTr("Couldn't read that picture.")
        },
        SystemDialog {
            id: unlinkDialog
            title: qsTr("Unlink this device?")
            body: qsTr("This device is removed from your linked devices and its chats are deleted here.")
            confirmButton.label: qsTr("Unlink")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection)
                    engine.request("link.logout", {})
            }
        },
        SystemToast {
            id: clearedToast
        }
    ]
    ScrollView {
        Container {
            topPadding: ui.du(2)
            leftPadding: ui.du(2)
            rightPadding: ui.du(2)
            bottomPadding: ui.du(2)
            // My profile (step 5.9): not shown while unlinked, when there is none.
            Container {
                visible: engine.linked
                horizontalAlignment: HorizontalAlignment.Fill
                bottomPadding: ui.du(1)
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    // The picture, or the tile with my initials; a tap offers Change and Remove.
                    Container {
                        preferredWidth: ui.du(20)
                        preferredHeight: ui.du(20)
                        background: page.loaded ? Color.create(page.info.color) : ui.palette.plain
                        layout: DockLayout {}
                        Label {
                            visible: !page.hasPicture
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            text: page.loaded ? page.info.initials : ""
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 300
                            textStyle.color: Color.White
                        }
                        ImageView {
                            visible: page.hasPicture
                            horizontalAlignment: HorizontalAlignment.Fill
                            verticalAlignment: VerticalAlignment.Fill
                            imageSource: page.hasPicture ? "file://" + page.info.picture + "?" + page.rev : ""
                            scalingMethod: ScalingMethod.AspectFill
                            loadEffect: ImageViewLoadEffect.None
                        }
                        gestureHandlers: TapHandler {
                            onTapped: page.changePhoto()
                        }
                    }
                    Container {
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: 1
                        }
                        leftMargin: ui.du(2)
                        verticalAlignment: VerticalAlignment.Center
                        Busy {
                            running: !page.loaded && !page.failed
                            text: qsTr("Loading…")
                        }
                        Label {
                            visible: page.failed
                            multiline: true
                            textStyle.color: ui.palette.secondaryTextOnPlain
                            text: qsTr("Couldn't load your profile. Check the connection and try again.")
                        }
                        Label {
                            visible: page.loaded
                            multiline: true
                            text: page.info.name || ""
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                            gestureHandlers: TapHandler {
                                onTapped: page.editName()
                            }
                        }
                        // Read-only: the number is the account's.
                        Label {
                            visible: page.loaded
                            text: page.info.phone || ""
                            textFormat: TextFormat.Plain
                            textStyle.base: SystemDefaults.TextStyles.SubtitleText
                            textStyle.color: ui.palette.secondaryTextOnPlain
                        }
                    }
                }
                // The about; a tap edits it. Padded so an empty one is still a target.
                Container {
                    visible: page.loaded
                    horizontalAlignment: HorizontalAlignment.Fill
                    topPadding: ui.du(2)
                    minHeight: ui.du(8)
                    Label {
                        text: qsTr("About")
                        bottomMargin: 0
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                    Label {
                        multiline: true
                        topMargin: 0
                        text: page.info.about || ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                    gestureHandlers: TapHandler {
                        onTapped: page.editAbout()
                    }
                }
                Divider {
                    topMargin: ui.du(2)
                }
            }
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
            Header {
                topMargin: ui.du(3)
                title: qsTr("Storage")
            }
            Container {
                topMargin: ui.du(2)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Label {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    verticalAlignment: VerticalAlignment.Center
                    text: qsTr("Cache: %1").arg(page.cacheText)
                }
                Button {
                    verticalAlignment: VerticalAlignment.Center
                    text: qsTr("Clear cache")
                    onClicked: {
                        // Nothing in the cache is lost, so no confirmation.
                        var freed = settings.clearCache()
                        page.cacheText = settings.cacheSize()
                        clearedToast.body = qsTr("Cache cleared: %1 freed").arg(freed)
                        clearedToast.show()
                    }
                }
            }
            Label {
                topMargin: ui.du(1)
                multiline: true
                text: qsTr("Voice notes' decoded audio and photo previews; they're made again when needed.")
                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                textStyle.color: ui.palette.secondaryTextOnPlain
            }
            // Last: the one action that ends the link.
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
