import bb.cascades 1.4
import hyperberry 1.0

// A developer test (Settings, overflow menu), step V0 of docs/plan/phase7e.md, the gate of the
// group video grid: plays the bundled 640x480@30 H.264 clip through 1, 2, 3 or 4 hardware
// decoders at once (OpenMAX IL), each into its own native window in a 2x2 grid under this page,
// again and again until the page closes. The overlay says per decoder whether it opened, its
// frames, fps and latency, and the process's CPU; each pass also writes a `video test:` line to
// the UI's log. The overflow menu picks how many decoders. See VideoTest.h.
Page {
    id: testPage
    titleBar: TitleBar {
        title: qsTr("Video decoders test")
    }
    // Starts when the page is built: the windows are made after the controls below exist, which
    // they do by now. The page's destruction (main.qml destroys a popped page) deletes `test`,
    // which stops it and frees the windows.
    onCreationCompleted: test.start(["hyperberryVideoTest0", "hyperberryVideoTest1", "hyperberryVideoTest2", "hyperberryVideoTest3"])
    attachedObjects: VideoTest {
        id: test
    }
    actions: [
        ActionItem {
            title: test.running ? qsTr("Stop") : qsTr("Start")
            imageSource: test.running ? "asset:///images/pause.png" : "asset:///images/play.png"
            ActionBar.placement: ActionBarPlacement.Signature
            onTriggered: {
                if (test.running) {
                    test.stop()
                } else {
                    test.start(["hyperberryVideoTest0", "hyperberryVideoTest1", "hyperberryVideoTest2", "hyperberryVideoTest3"])
                }
            }
        },
        // How many decoders; the current one is ticked. Each restarts the test.
        ActionItem {
            title: (test.count == 1 ? "✓ " : "") + qsTr("1 decoder")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.count = 1
        },
        ActionItem {
            title: (test.count == 2 ? "✓ " : "") + qsTr("2 decoders")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.count = 2
        },
        ActionItem {
            title: (test.count == 3 ? "✓ " : "") + qsTr("3 decoders")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.count = 3
        },
        ActionItem {
            title: (test.count == 4 ? "✓ " : "") + qsTr("4 decoders")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.count = 4
        }
    ]
    // No background of our own: the theme's is black here, and one of ours painted over the holes
    // the native windows show through (0.0.1.190: only the numbers showed).
    Container {
        layout: DockLayout {
        }
        Container {
            id: stage
            // The width the page gives; each cell is half of it, 4:3 like the clip.
            property real w: 0
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Center
            layout: DockLayout {
            }
            attachedObjects: LayoutUpdateHandler {
                onLayoutFrameChanged: stage.w = layoutFrame.width
            }
            // Under the page's own drawing (the windows sit at z -1), so the report below shows
            // over the pictures and the action bar over their foot.
            Container {
                layout: StackLayout {
                    orientation: LayoutOrientation.TopToBottom
                }
                Container {
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    ForeignWindowControl {
                        windowId: "hyperberryVideoTest0"
                        updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                        preferredWidth: stage.w / 2
                        preferredHeight: stage.w / 2 * 3 / 4
                    }
                    ForeignWindowControl {
                        windowId: "hyperberryVideoTest1"
                        updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                        preferredWidth: stage.w / 2
                        preferredHeight: stage.w / 2 * 3 / 4
                    }
                }
                Container {
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    ForeignWindowControl {
                        windowId: "hyperberryVideoTest2"
                        updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                        preferredWidth: stage.w / 2
                        preferredHeight: stage.w / 2 * 3 / 4
                    }
                    ForeignWindowControl {
                        windowId: "hyperberryVideoTest3"
                        updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                        preferredWidth: stage.w / 2
                        preferredHeight: stage.w / 2 * 3 / 4
                    }
                }
            }
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                background: Color.create("#99000000")
                leftPadding: ui.du(1)
                rightPadding: ui.du(1)
                Label {
                    text: test.error != "" ? test.error : test.report
                    multiline: true
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.color: Color.White
                }
            }
        }
    }
}
