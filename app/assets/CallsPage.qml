import bb.cascades 1.4

// The Calls tab (step 7.4): the call rows of every chat, newest first, from callList (CallsModel),
// which reads chats.db only while active (main.qml sets that when the tab is shown). A tap calls back; the long-press menu has Call and Open
// chat. A failed call.start is worded by main.qml's toast, whoever sent it.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Calls")
        scrollBehavior: TitleBarScrollBehavior.Sticky
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            id: list
            dataModel: callList
            property variant emojiStyle: nav.emojiStyle
            property int bodySize: nav.bodySize
            property int smallSize: nav.smallSize
            property real textScale: settings.textScale
            property string pressColor: nav.pressColor
            function callBack(row) {
                engine.request("call.start", {"chat": row.jid})
            }
            function openChat(row) {
                nav.openRequested(row.jid)
            }
            onTriggered: {
                if (indexPath.length == 1) {
                    callBack(dataModel.data(indexPath))
                }
            }
            listItemComponents: [
                ListItemComponent {
                    type: "item"
                    CallRow {
                        call: ListItemData
                    }
                }
            ]
        }
        Label {
            visible: callList.empty
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("No calls yet")
        }
    }
}
