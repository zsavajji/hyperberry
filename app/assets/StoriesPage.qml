import bb.cascades 1.4

// The Stories tab: a grid of two tiles across, one per poster with a story under 24 h old, from storyList
// (StoriesModel), unseen first. The cell is the stock Active Frame's size on the Q10, 310 x 211 px: 720 px less
// 40 px each side and a 20 px gap leaves 310. The list reloads only while this tab is shown (main.qml sets it).
Page {
    id: page
    // The viewer, a Sheet made for each poster opened (it destroys itself on close).
    attachedObjects: ComponentDefinition {
        id: viewerDef
        source: "StoryViewer.qml"
    }
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
                        viewerDef.createObject().start(dataModel.data(indexPath))
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
