import bb.cascades 1.4

// Settings > Chats and Calls: saved as they change. Each lives in `settings`.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Chats and Calls")
    }
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ScrollView {
            Container {
                Header {
                    title: qsTr("Chats")
                }
                Container {
                    topPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(2)
                    rightPadding: zoom * ui.du(2)
                    bottomPadding: zoom * ui.du(2)
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
                        topMargin: zoom * ui.du(3)
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            verticalAlignment: VerticalAlignment.Center
                            Label {
                                text: qsTr("Proximity sensor")
                                bottomMargin: 0
                            }
                            Label {
                                text: qsTr("Off, voice notes stay on the speaker and the screen stays on in calls. For a faulty sensor.")
                                multiline: true
                                topMargin: 0
                                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                                textStyle.color: ui.palette.secondaryTextOnPlain
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.earSensor
                            onCheckedChanged: settings.earSensor = checked
                        }
                    }
                }
                Header {
                    title: qsTr("Calls")
                }
                Container {
                    topPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(2)
                    rightPadding: zoom * ui.du(2)
                    bottomPadding: zoom * ui.du(2)
                    // What our camera sends in a video call; CallVideo reopens the camera when either changes.
                    DropDown {
                        title: qsTr("Max camera resolution")
                        options: [
                            Option { text: "1280x720"; selected: settings.videoQuality == 0 },
                            Option { text: "640x480"; selected: settings.videoQuality == 1 },
                            Option { text: "320x240"; selected: settings.videoQuality == 2 }
                        ]
                        onSelectedIndexChanged: {
                            if (selectedIndex >= 0) {
                                settings.videoQuality = selectedIndex
                            }
                        }
                    }
                    DropDown {
                        topMargin: zoom * ui.du(2)
                        title: qsTr("Camera aspect ratio")
                        options: [
                            Option { text: qsTr("Native"); selected: !settings.cameraCrop },
                            Option { text: qsTr("9:16 (cropped)"); selected: settings.cameraCrop }
                        ]
                        onSelectedIndexChanged: {
                            if (selectedIndex >= 0) {
                                settings.cameraCrop = selectedIndex == 1
                            }
                        }
                    }
                    Label {
                        topMargin: zoom * ui.du(1)
                        multiline: true
                        text: qsTr("The largest picture your camera sends: it steps down to 640x480 or 320x240, and back up, as the connection allows. 9:16 sends the middle of the picture, upright, to fill the other phone's screen. A change applies at once, also during a call.")
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
            }
        }
    }
}
