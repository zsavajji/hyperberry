import bb.cascades 1.4
import bb.cascades.pickers 1.0
import bb.system 1.2

// The Stories tab: a grid of two tiles across, one per poster with a story under 24 h old, from storyList
// (StoriesModel), unseen first, our own stories ("My story") before them. The cell is the stock Active Frame's size on the Q10, 310 x 211 px: 720 px less
// 40 px each side and a 20 px gap leaves 310. The list reloads only while this tab is shown (main.qml sets it).
// New story (the Signature action) picks a photo or a video and Text story writes one; StoryComposer posts it.
Page {
    id: page
    // A file the engine can send as a story: its extension, as the engine tells a photo from a video.
    function modeOf(path) {
        var ext = path.substring(path.lastIndexOf(".") + 1).toLowerCase()
        return ext == "jpg" || ext == "jpeg" ? "photo" : ext == "mp4" || ext == "3gp" || ext == "mov" ? "video" : ""
    }
    onCreationCompleted: {
        storyList.postFailed.connect(postFailed.show)
        storyList.deleteFailed.connect(deleteFailed.show)
    }
    attachedObjects: [
        // The viewer, one Sheet for every poster opened: it is set up by start() and emptied when it closes.
        StoryViewer {
            id: viewer
        },
        StoryComposer {
            id: composer
        },
        FilePicker {
            id: picker
            type: FileType.Picture | FileType.Video
            mode: FilePickerMode.Picker
            title: qsTr("New story")
            onFileSelected: {
                var m = page.modeOf(selectedFiles[0])
                if (m == "") {
                    wrongFile.show()
                } else {
                    composer.compose(m, selectedFiles[0])
                }
            }
        },
        SystemToast {
            id: postFailed
            body: qsTr("Couldn't post the story")
        },
        SystemToast {
            id: deleteFailed
            body: qsTr("Couldn't delete the story")
        },
        SystemToast {
            id: wrongFile
            body: qsTr("Pick a JPEG photo or an MP4 video")
        }
    ]
    actions: [
        ActionItem {
            title: qsTr("New story")
            imageSource: "asset:///images/plus_action.png"
            ActionBar.placement: ActionBarPlacement.Signature
            shortcuts: SystemShortcut {
                type: SystemShortcuts.CreateNew
            }
            onTriggered: picker.open()
        },
        ActionItem {
            title: qsTr("Text story")
            imageSource: "file:///usr/share/icons/bb_action_sendtextmessage.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: composer.compose("text", "")
        }
    ]
    titleBar: TitleBar {
        title: qsTr("Stories")
        scrollBehavior: TitleBarScrollBehavior.Sticky
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            leftPadding: ui.px(40)
            rightPadding: ui.px(40)
            topPadding: ui.px(20)
            ListView {
                id: grid
                dataModel: storyList
                property variant emojiStyle: nav.emojiStyle
                property int bodySize: nav.bodySize
                property int smallSize: nav.smallSize
                property string pressColor: nav.pressColor
                layout: GridListLayout {
                    columnCount: 2
                    cellAspectRatio: 310 / 211
                    horizontalCellSpacing: ui.px(20)
                    verticalCellSpacing: ui.px(20)
                }
                onTriggered: {
                    if (indexPath.length == 1) {
                        viewer.start(dataModel.data(indexPath))
                    }
                }
                listItemComponents: [
                    ListItemComponent {
                        type: "item"
                        StoryTile {
                            story: ListItemData
                        }
                    }
                ]
            }
        }
        Label {
            visible: storyList.empty
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("No stories yet")
        }
    }
}
