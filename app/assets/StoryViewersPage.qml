import bb.cascades 1.4

// Who viewed one of our stories (docs/plan/stories.md), pushed over the story viewer by its eye, so it slides in
// as Group info does and Back goes back to the story. The rows are storyList.viewersOf, newest first, filled
// again whenever a view arrives.
Page {
    id: page
    property string storyId: ""
    property int count: 0
    // The pusher calls this before the push.
    function load(id) {
        page.storyId = id
        page.fill()
    }
    // ponytail: every new view clears and fills the list, which takes it back to the top; replace rows in place
    // (as GroupInfoPage.fill does) if a long list jumps.
    function fill() {
        var list = storyList.viewersOf(page.storyId)
        people.clear()
        if (list.length > 0) {
            people.append(list)
        }
        page.count = list.length
    }
    onCreationCompleted: {
        // Not a Connections: Cascades' QML has none. It lives and dies with the page.
        storyList.viewsChanged.connect(page.fill)
    }
    titleBar: TitleBar {
        title: qsTr("Viewed by")
    }
    attachedObjects: [
        ArrayDataModel {
            id: people
        }
    ]
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
            property variant emojiStyle: nav.emojiStyle
            property int bodySize: nav.bodySize
            property int smallSize: nav.smallSize
            property string pressColor: nav.pressColor
            property variant lineColor: nav.lineColor
            dataModel: people
            listItemComponents: ListItemComponent {
                type: ""
                ContactRow {
                    contact: ListItemData
                }
            }
        }
        Label {
            visible: page.count == 0
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("No views yet")
        }
    }
}
