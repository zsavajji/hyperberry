import bb.cascades 1.4
import bb.system 1.2

// Settings > Data and storage: saved as they change. Each lives in `settings`.
Page {
    id: page
    property string cacheText: ""
    titleBar: TitleBar {
        title: qsTr("Data and storage")
    }
    // Read when the page opens and after a clear; the walk of data/cache/ is a few files.
    onCreationCompleted: cacheText = settings.cacheSize()
    attachedObjects: [
        SystemToast {
            id: clearedToast
        }
    ]
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ScrollView {
            Container {
                Header {
                    title: qsTr("Download automatically")
                }
                Container {
                    topPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(2)
                    rightPadding: zoom * ui.du(2)
                    bottomPadding: zoom * ui.du(2)
                    // What downloads when it arrives; the rest waits for a tap. Mobile data, below, holds them all.
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
                                text: qsTr("Photos")
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.autoPhoto
                            onCheckedChanged: settings.autoPhoto = checked
                        }
                    }
                    Container {
                        topMargin: zoom * ui.du(2)
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            verticalAlignment: VerticalAlignment.Center
                            Label {
                                text: qsTr("Audio")
                                bottomMargin: 0
                            }
                            Label {
                                text: qsTr("Voice notes and audio files")
                                multiline: true
                                topMargin: 0
                                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                                textStyle.color: ui.palette.secondaryTextOnPlain
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.autoAudio
                            onCheckedChanged: settings.autoAudio = checked
                        }
                    }
                    Container {
                        topMargin: zoom * ui.du(2)
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            verticalAlignment: VerticalAlignment.Center
                            Label {
                                text: qsTr("Videos")
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.autoVideo
                            onCheckedChanged: settings.autoVideo = checked
                        }
                    }
                    Container {
                        topMargin: zoom * ui.du(2)
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Container {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            verticalAlignment: VerticalAlignment.Center
                            Label {
                                text: qsTr("Documents")
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.autoDoc
                            onCheckedChanged: settings.autoDoc = checked
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
                                text: qsTr("Download on mobile data")
                                bottomMargin: 0
                            }
                            Label {
                                text: qsTr("Off, nothing downloads by itself on mobile data; it waits for Wi-Fi or a tap.")
                                multiline: true
                                topMargin: 0
                                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                                textStyle.color: ui.palette.secondaryTextOnPlain
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.mediaOnMobile
                            onCheckedChanged: settings.mediaOnMobile = checked
                        }
                    }
                }
                Header {
                    title: qsTr("Network")
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
                                text: qsTr("Cloudflare DNS")
                                bottomMargin: 0
                            }
                            Label {
                                text: qsTr("Finds WhatsApp's servers through 1.1.1.1 and 1.0.0.1 instead of your network's DNS. Try it if messages or media don't go through.")
                                multiline: true
                                topMargin: 0
                                textStyle.base: SystemDefaults.TextStyles.SubtitleText
                                textStyle.color: ui.palette.secondaryTextOnPlain
                            }
                        }
                        ToggleButton {
                            verticalAlignment: VerticalAlignment.Center
                            checked: settings.cloudflareDns
                            onCheckedChanged: settings.cloudflareDns = checked
                        }
                    }
                }
                Header {
                    title: qsTr("Storage")
                }
                Container {
                    topPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(2)
                    rightPadding: zoom * ui.du(2)
                    bottomPadding: zoom * ui.du(2)
                    // What data/cache/ may hold. The engine prunes it, the oldest first (step 8.3).
                    DropDown {
                        title: qsTr("Cache limit")
                        options: [
                            Option { text: "64 MB"; selected: settings.cacheLimit == 64 },
                            Option { text: "128 MB"; selected: settings.cacheLimit == 128 },
                            Option { text: "256 MB"; selected: settings.cacheLimit == 256 },
                            Option { text: "512 MB"; selected: settings.cacheLimit == 512 }
                        ]
                        onSelectedIndexChanged: {
                            if (selectedIndex >= 0) {
                                settings.cacheLimit = [64, 128, 256, 512][selectedIndex]
                            }
                        }
                    }
                    Container {
                        topMargin: zoom * ui.du(2)
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
                        topMargin: zoom * ui.du(1)
                        multiline: true
                        text: qsTr("Voice notes' decoded audio and photo previews; they're made again when needed.")
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
                // The engine's memory cap (memMB in the link body; MemoryPatch.cpp). Hidden when unknown.
                Header {
                    visible: engine.link.memMB > 0
                    title: qsTr("Background memory")
                }
                Container {
                    visible: engine.link.memMB > 0
                    topPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(2)
                    rightPadding: zoom * ui.du(2)
                    bottomPadding: zoom * ui.du(2)
                    Label {
                        multiline: true
                        text: qsTr("HyperBerry's background service has %1 MB").arg(engine.link.memMB)
                    }
                    Button {
                        visible: engine.link.memMB < 128 && !memPatch.installed
                        topMargin: zoom * ui.du(1)
                        horizontalAlignment: HorizontalAlignment.Fill
                        enabled: !memPatch.busy
                        text: qsTr("Install the memory patch")
                        onClicked: nav.askMemory()
                    }
                    Label {
                        visible: engine.link.memMB < 128 && memPatch.installed
                        topMargin: zoom * ui.du(1)
                        multiline: true
                        text: qsTr("The patch is installed. It takes effect a few minutes after the phone starts: close HyperBerry and open it again then.")
                        textStyle.base: SystemDefaults.TextStyles.SubtitleText
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                }
            }
        }
    }
}
