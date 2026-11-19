import bb.cascades 1.4
import bb.cascades.pickers 1.0
import bb.system 1.2
import hyperberry 1.0

// The Stories tab: a grid of two tiles across, one per poster with a story under 24 h old, from storyList
// (StoriesModel), unseen first, our own stories ("My story") before them. The cell is the stock Active Frame's size on the Q10, 310 x 211 px: 720 px less
// 40 px each side and a 20 px gap leaves 310. The list reloads only while this tab is shown (main.qml sets it).
// New story (the Signature action) picks a photo (a JPEG, or a PNG made into one) or a video and Text story writes
// one; StoryComposer posts it.
Page {
    id: page
    // A file the engine can send as a story: its extension, as the engine tells a photo from a video.
    function modeOf(path) {
        var ext = path.substring(path.lastIndexOf(".") + 1).toLowerCase()
        return ext == "jpg" || ext == "jpeg" || ext == "png" ? "photo" : ext == "mp4" || ext == "3gp" || ext == "mov" ? "video" : ""
    }
    // The viewer on a story quoted in a chat; a story older than a day is gone, and says so.
    function openStory(poster, id) {
        var row = storyList.posterOf(poster)
        if (!row.poster || storyList.storiesOf(poster).length == 0) {
            storyGone.show()
            return
        }
        page.view(row, id)
    }
    // The viewer, a page of its own on the tab's pane (StoryViewer.qml): the system's Back pops it, and so does its end.
    function view(row, id) {
        var v = viewerDef.createObject()
        v.finished.connect(page.viewed)
        storiesNav.push(v)
        v.start(row, id)
    }
    function viewed() {
        storiesNav.pop()
    }
    onCreationCompleted: {
        storyList.postFailed.connect(postFailed.show)
        storyList.deleteFailed.connect(deleteFailed.show)
    }
    attachedObjects: [
        ComponentDefinition {
            id: viewerDef
            source: "StoryViewer.qml"
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
                var file = selectedFiles[0]
                var m = page.modeOf(file)
                if (m == "photo") {
                    // A PNG becomes a JPEG here, since the engine sends JPEGs and decodes no picture.
                    file = media.asJpeg(file)
                    if (file == "") {
                        m = ""
                    }
                }
                if (m == "") {
                    wrongFile.show()
                } else {
                    composer.compose(m, file)
                }
            }
        },
        MediaOpener {
            id: media
            link: engine
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
            id: storyGone
            body: qsTr("This story is no longer available")
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
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            leftPadding: pxZoom * ui.px(40)
            rightPadding: pxZoom * ui.px(40)
            topPadding: pxZoom * ui.px(20)
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
                    horizontalCellSpacing: pxZoom * ui.px(20)
                    verticalCellSpacing: pxZoom * ui.px(20)
                }
                onTriggered: {
                    if (indexPath.length == 1) {
                        page.view(dataModel.data(indexPath))
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
