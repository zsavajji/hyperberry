import bb.cascades 1.4
import hyperberry 1.0

// A developer test (Settings, overflow menu): decodes the bundled 640x480@30 H.264 clip in the
// phone's hardware, through OpenMAX IL, and draws it in a native window under this page, again and
// again until the page closes. The numbers on it are the answer; each pass also writes a
// `video test:` line to the UI's log. See VideoTest.h.
Page {
    id: testPage
    titleBar: TitleBar {
        title: qsTr("Video decoder test")
    }
    // Starts when the page is built: the window is made after the control below exists, which
    // it does by now. The page's destruction (main.qml destroys a popped page) deletes `test`,
    // which stops it and frees the window.
    onCreationCompleted: test.start(video.windowId)
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
                    test.start(video.windowId)
                }
            }
        },
        // The window's switches, for when the picture doesn't show: each restarts the test.
        ActionItem {
            title: test.above ? qsTr("Window behind") : qsTr("Window in front")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.above = !test.above
        },
        ActionItem {
            title: test.rgb ? qsTr("Draw as NV12") : qsTr("Draw as RGB")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.rgb = !test.rgb
        }
    ]
    // No background of our own: the theme's is black here, and one of ours painted over the hole
    // the native window shows through (0.0.1.190: only the numbers showed).
    Container {
        layout: DockLayout {
        }
        Container {
            id: stage
            // The width the page gives, for the video's height: 4:3, so 720x540 on the Q10.
            property real w: 0
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Center
            layout: DockLayout {
            }
            attachedObjects: LayoutUpdateHandler {
                onLayoutFrameChanged: stage.w = layoutFrame.width
            }
            // Under the page's own drawing (the window sits at z -1), so the report below shows
            // over the picture and the action bar over its foot.
            ForeignWindowControl {
                id: video
                windowId: "hyperberryVideoTest"
                updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                horizontalAlignment: HorizontalAlignment.Fill
                preferredHeight: stage.w * 3 / 4
            }
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Top
                background: Color.create("#99000000")
                leftPadding: ui.du(1)
                rightPadding: ui.du(1)
                Label {
                    text: test.error != "" ? test.error
                        : qsTr("Pass %1: in %2, out %3, %4 fps").arg(test.pass + 1).arg(test.framesIn).arg(test.framesOut).arg(test.fps.toFixed(1))
                    multiline: true
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.color: Color.White
                }
                Label {
                    text: qsTr("Latency %1 / max %2 ms, first frame %3 ms").arg(test.latencyMean.toFixed(1)).arg(test.latencyMax.toFixed(1)).arg(test.firstFrameMs.toFixed(0))
                    multiline: true
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.color: Color.White
                }
                Label {
                    text: qsTr("CPU %1 % of a core, de-tile %2 ms").arg(test.cpuPercent.toFixed(0)).arg(test.detileMs.toFixed(2))
                    multiline: true
                    textStyle.base: SystemDefaults.TextStyles.SmallText
                    textStyle.color: Color.White
                }
            }
        }
    }
}
