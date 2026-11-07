import bb.cascades 1.4
import hyperberry 1.0

// A developer test (Settings, overflow menu): the camera encodes H.264 in the phone's hardware,
// each access unit goes straight into the hardware decoder and a native window (the larger
// picture, a fraction of a second behind), and the camera's own viewfinder shows beside it (the
// smaller). No network. The numbers under them are the answer; every 10 s they also go to the
// UI's log as a `camera test:` line. See CameraTest.h.
Page {
    id: testPage
    titleBar: TitleBar {
        title: qsTr("Camera test")
    }
    // Starts when the page is built: both windows are made after the controls below exist, which
    // they do by now. The page's destruction (main.qml destroys a popped page) deletes `test`,
    // which stops the camera and the decoder and frees the window.
    onCreationCompleted: test.start(loopback.windowId, finder.windowId)
    attachedObjects: CameraTest {
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
                    test.start(loopback.windowId, finder.windowId)
                }
            }
        },
        // Each restarts a running test.
        ActionItem {
            title: test.front ? qsTr("Use the rear camera") : qsTr("Use the front camera")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.front = !test.front
        },
        ActionItem {
            title: test.small ? qsTr("Size 640x480") : qsTr("Size 320x240")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.small = !test.small
        },
        ActionItem {
            title: test.fps == 30 ? qsTr("15 frames a second") : qsTr("30 frames a second")
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: test.fps = test.fps == 30 ? 15 : 30
        }
    ]
    // No background of our own: the native windows show through the page's holes (see
    // VideoTestPage.qml).
    Container {
        id: root
        // The width the page gives; the pictures are 4:3, a third and two thirds of it.
        property real w: 0
        attachedObjects: LayoutUpdateHandler {
            onLayoutFrameChanged: root.w = layoutFrame.width
        }
        Container {
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            verticalAlignment: VerticalAlignment.Top
            // The camera's viewfinder: the library makes the window, in our group, with this id.
            ForeignWindowControl {
                id: finder
                windowId: "hyperberryCameraViewfinder"
                updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                onBoundToWindowChanged: console.log("camera control: viewfinder bound " + boundToWindow + " in " + finder.windowGroup)
                preferredWidth: root.w / 3
                preferredHeight: root.w / 4
                verticalAlignment: VerticalAlignment.Top
            }
            // The decoded loopback: our own window, made by VideoWindow.
            ForeignWindowControl {
                id: loopback
                windowId: "hyperberryCameraLoopback"
                updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
                onBoundToWindowChanged: console.log("camera control: loopback bound " + boundToWindow + " in " + loopback.windowGroup)
                preferredWidth: root.w * 2 / 3
                preferredHeight: root.w / 2
                verticalAlignment: VerticalAlignment.Top
            }
        }
        Container {
            leftPadding: ui.du(1)
            rightPadding: ui.du(1)
            Label {
                text: test.error != "" ? test.error : test.modeText
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
            }
            Label {
                text: qsTr("Encoder %1 fps, %2 kbit/s, %3 keyframes").arg(test.encFps.toFixed(1)).arg(test.encKbps.toFixed(0)).arg(test.keyframes)
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
            }
            Label {
                text: qsTr("Frame mean %1 / max %2 bytes, gap %3 / max %4 ms").arg(test.meanFrameBytes).arg(test.maxFrameBytes).arg(test.intervalMean.toFixed(1)).arg(test.intervalMax.toFixed(1))
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
            }
            Label {
                text: qsTr("Decoder in %1, out %2, latency %3 / max %4 ms").arg(test.framesIn).arg(test.framesOut).arg(test.latencyMean.toFixed(1)).arg(test.latencyMax.toFixed(1))
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
            }
            Label {
                text: qsTr("CPU %1 % of a core (whole app)").arg(test.cpuPercent.toFixed(0))
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
            }
        }
    }
}
