import bb.cascades 1.4
import hyperberry 1.0

// The Calls tab (step 7.4): the call rows of every chat, newest first, from callList (CallsModel),
// which reads chats.db only while active (main.qml sets that when the tab is shown). A tap calls back; the long-press menu has Call and Open
// chat. A failed call.start is worded by main.qml's toast, whoever sent it.
Page {
    id: page
    // New call (the user's ask, 2026-11-07): New chat's contact picker, titled for a call; a pick
    // places the call and the picker goes, so the sheet comes up over the Calls list.
    attachedObjects: ComponentDefinition {
        id: pickerDef
        source: "NewChatPage.qml"
    }
    actions: ActionItem {
        title: qsTr("New call")
        imageSource: "file:///usr/share/icons/ic_phone.png"
        ActionBar.placement: ActionBarPlacement.Signature
        shortcuts: SystemShortcut {
            type: SystemShortcuts.CreateNew
        }
        onTriggered: page.newCall()
    }
    function newCall() {
        var picker = pickerDef.createObject()
        picker.title = qsTr("New call")
        picker.picked.connect(page.callPicked)
        callsNav.push(picker)
        picker.focusSearch()
    }
    function callPicked(contact) {
        engine.request("call.start", {"chat": contact.jid})
        callsNav.pop()
    }
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
            // The rows are the chat list's (ChatRows.cpp), as a call's: the call in the preview's place.
            property bool callList: true
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
            listItemProvider: ChatRows {}
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
