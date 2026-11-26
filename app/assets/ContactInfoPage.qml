import bb.cascades 1.4
import bb.system 1.2
import hyperberry 1.0

// A person's info (step 5.8), read-only but for the mute toggle and, in the action menu, the
// account actions (step 5.8b). chat is a chat list row, set right after createObject(), or a row
// built like the contact picker's for a person with no chat yet (a group's participant). The page
// asks the engine itself (contact.info) through its own ContactInfo, so the answer goes with the
// page.
Page {
    id: page
    property variant chat
    property variant info: contact.info
    property bool loaded: info.groups !== undefined
    property bool failed: !loaded && engine.failure.cmd == "contact.info"
    // The full-size picture the engine fetched with the info; its file name has the picture's id.
    property bool hasPicture: !!info.picture
    // From info, not a method call: a binding isn't re-read when a method's result changes.
    property int groupCount: loaded ? info.groups.length : 0
    property bool muted: false
    // A WhatsApp Business account: contact.info's business block (engine/business.go).
    property bool business: loaded && !!info.business
    property bool hasHours: business && info.business.hours.length > 0
    property bool weekShown: false
    // The chat exists in chats.db. A person from a group, or the picker, may have none yet: then
    // there is nothing to mute, export, clear or delete.
    property bool hasChat: false
    // As ChatPage: a failure of the page's own requests, once per failure.
    property string failedCmd: engine.failure.cmd || ""
    onFailedCmdChanged: {
        if (failedCmd == "contact.block" || failedCmd == "chat.export") {
            failedToast.show()
        }
    }
    onChatChanged: {
        // !!: a contact picked with no chat yet has no muted, and undefined fails a bool.
        muted = !!chat.muted
        hasChat = !!chats.chat(chat.jid).jid
        contact.ask(chat.jid)
    }
    onInfoChanged: fill()
    // Not a Connections: Cascades' QML has none. Both live and die with the page.
    onCreationCompleted: contact.exported.connect(page.exported)
    titleBar: TitleBar {
        title: qsTr("Contact info")
    }
    actions: [
        // Opens the person's chat, which may not exist yet (nav.openRow).
        ActionItem {
            title: qsTr("Message")
            imageSource: "file:///usr/share/icons/bb_action_sendtextmessage.png"
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: nav.openRow(page.chat)
        },
        // call.start's failure is worded by main.qml's toast. Not for ourselves (the engine says bad_chat).
        ActionItem {
            title: qsTr("Voice call")
            imageSource: "file:///usr/share/icons/ic_phone.png"
            enabled: engine.linked
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: engine.request("call.start", {"chat": page.chat.jid})
        },
        ActionItem {
            title: qsTr("Video call")
            imageSource: "file:///usr/share/icons/ic_chat_video.png"
            enabled: engine.linked
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: engine.request("call.start", {"chat": page.chat.jid, "video": true})
        },
        ActionItem {
            title: qsTr("Export chat")
            imageSource: "asset:///images/save_action.png"
            enabled: page.hasChat
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: {
                contact.exportChat()
            }
        },
        ActionItem {
            title: qsTr("Clear chat")
            imageSource: "file:///usr/share/icons/bb_action_delete.png"
            enabled: page.hasChat
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: {
                clearDialog.show()
            }
        },
        // Unblock asks nothing: it undoes the block, which is the confirmation.
        ActionItem {
            title: page.info.blocked ? qsTr("Unblock") : qsTr("Block")
            imageSource: "asset:///images/block_action.png"
            enabled: page.loaded
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: {
                if (page.info.blocked) {
                    contact.block(false)
                } else {
                    blockDialog.show()
                }
            }
        },
        DeleteActionItem {
            title: qsTr("Delete chat")
            enabled: page.hasChat
            ActionBar.placement: ActionBarPlacement.InOverflow
            onTriggered: {
                deleteDialog.show()
            }
        }
    ]
    attachedObjects: [
        ContactInfo {
            id: contact
            link: engine
        },
        // A group in common: the same row as a participant on the group info page.
        ComponentDefinition {
            id: groupDef
            Container {
                id: row
                property variant group
                property bool pressed: false
                horizontalAlignment: HorizontalAlignment.Fill
                // The Classic's trackpad reaches the row: it takes the focus, lit as while pressed, and a click (Enter) opens the group (the user, 2026-11-20).
                focusPolicy: FocusPolicy.KeyAndTouch
                background: pressed || focused ? Color.create(nav.pressColor) : Color.Transparent
                gestureHandlers: TapHandler {
                    onTapped: nav.openRequested(row.group.jid)
                }
                keyListeners: KeyListener {
                    onKeyReleased: {
                        if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                            nav.openRequested(row.group.jid)
                        }
                    }
                }
                onTouch: row.pressed = event.isDown() || event.isMove()
                onTouchExit: row.pressed = false
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    minHeight: zoom * ui.du(9.4)
                    leftPadding: zoom * ui.du(1.5)
                    rightPadding: zoom * ui.du(1.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: zoom * ui.du(7)
                        preferredHeight: zoom * ui.du(7)
                        background: row.group ? Color.create(row.group.color) : Color.Transparent
                        layout: DockLayout {}
                        Label {
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            text: row.group ? row.group.initials : ""
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 118 * zoom
                            textStyle.color: Color.White
                        }
                    }
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        leftMargin: zoom * ui.du(1.5)
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: 1
                        }
                        text: row.group ? row.group.name : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: ui.px(2)
                    background: nav.lineColor
                }
            }
        },
        SystemDialog {
            id: clearDialog
            title: qsTr("Clear this chat?")
            body: qsTr("Its messages will be deleted from this phone.")
            confirmButton.label: qsTr("Clear")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    engine.request("chat.clear", {"chat": page.chat.jid})
                }
            }
        },
        SystemDialog {
            id: blockDialog
            title: qsTr("Block %1?").arg(page.chat ? page.chat.name : "")
            body: qsTr("Blocked contacts can't call you or send you messages.")
            confirmButton.label: qsTr("Block")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    contact.block(true)
                }
            }
        },
        SystemDialog {
            id: deleteDialog
            title: qsTr("Delete this chat?")
            body: qsTr("Its messages will be deleted from this phone.")
            confirmButton.label: qsTr("Delete")
            cancelButton.label: qsTr("Cancel")
            onFinished: {
                if (value == SystemUiResult.ConfirmButtonSelection) {
                    engine.request("chat.delete", {"chat": page.chat.jid})
                    // The chat page under this one goes too: the chat is gone.
                    nav.backToList()
                }
            }
        },
        SystemToast {
            id: exportedToast
        },
        SystemToast {
            id: failedToast
            body: qsTr("Couldn't do that. Check the connection and try again.")
        }
    ]
    // ponytail: on mutes always; the list's menu offers 8 hours and a week too.
    function setMuted(on) {
        if (on == muted) {
            return
        }
        muted = on
        engine.request("chat.set", {"chat": chat.jid, "mute": on ? -1 : 0})
    }
    // The body is set when the export is done: it names the file.
    function exported(path) {
        exportedToast.body = qsTr("Saved to Downloads: %1").arg(path.substring(path.lastIndexOf("/") + 1))
        exportedToast.show()
    }
    // ponytail: hours read in the phone's time zone, not the business's; a business abroad shows the wrong "open now".
    function hoursOf(d) {
        var out = []
        var h = info.business.hours
        for (var i = 0; i < h.length; i++) {
            if (h[i].day == d) {
                out.push(h[i])
            }
        }
        return out
    }
    function clock(min) {
        return Qt.formatTime(new Date(2000, 0, 1, Math.floor(min / 60), min % 60))
    }
    // A day's hours as the phone writes them: "Open 24 hours", "09:00 – 12:00, 15:00 – 19:00", "Closed".
    function dayText(d) {
        var h = hoursOf(d)
        var spans = []
        for (var i = 0; i < h.length; i++) {
            if (h[i].mode == "open_24h") {
                return qsTr("Open 24 hours")
            }
            if (h[i].mode == "appointment_only") {
                return qsTr("By appointment only")
            }
            spans.push(clock(h[i].open) + " – " + clock(h[i].close))
        }
        return spans.length ? spans.join(", ") : qsTr("Closed")
    }
    // "Open now" at this minute (today's hours, or last night's that run past midnight), "By appointment" or "Closed now".
    function status() {
        var now = new Date()
        var m = now.getHours() * 60 + now.getMinutes()
        var today = hoursOf(now.getDay())
        for (var i = 0; i < today.length; i++) {
            var h = today[i]
            if (h.mode == "appointment_only") {
                return qsTr("By appointment")
            }
            if (h.mode == "open_24h" || (h.close > h.open ? m >= h.open && m < h.close : m >= h.open)) {
                return qsTr("Open now")
            }
        }
        var night = hoursOf((now.getDay() + 6) % 7)
        for (var j = 0; j < night.length; j++) {
            if (night[j].mode == "specific_hours" && night[j].close <= night[j].open && m < night[j].close) {
                return qsTr("Open now")
            }
        }
        return qsTr("Closed now")
    }
    // The week from Monday, day names and hours as two columns of lines.
    function weekText(names) {
        var out = []
        for (var i = 1; i <= 7; i++) {
            var d = i % 7
            out.push(names ? Qt.formatDate(new Date(2026, 10, 22 + d), "dddd") : dayText(d)) // 22 November 2026 is a Sunday
        }
        return out.join("\n")
    }
    function fill() {
        groupsBox.removeAll()
        if (info && info.groups) {
            for (var i = 0; i < info.groups.length; i++) {
                var row = groupDef.createObject()
                row.group = info.groups[i]
                groupsBox.add(row)
            }
        }
    }
    // One scrolling column, groups included: a ListView inside a ScrollView misbehaves.
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ScrollView {
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                // The tile, name and number; the chat's own until the engine answers.
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    topPadding: zoom * ui.du(3)
                    // Air under the number, which sat on the next header (the user's ask, 2026-11-19, as Tengram's).
                    bottomPadding: zoom * ui.du(2)
                    leftPadding: zoom * ui.du(1.5)
                    rightPadding: zoom * ui.du(1.5)
                    Container {
                        horizontalAlignment: HorizontalAlignment.Center
                        preferredWidth: zoom * ui.du(20)
                        preferredHeight: zoom * ui.du(20)
                        background: page.chat ? Color.create(page.chat.color) : Color.Transparent
                        layout: DockLayout {}
                        Label {
                            visible: !page.hasPicture
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            text: page.chat ? page.chat.initials : ""
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 300 * zoom
                            textStyle.color: Color.White
                        }
                        // The picture opens full size on a tap, and on the Classic's trackpad: it takes the focus, dimmed (the user, 2026-11-20).
                        ImageView {
                            id: bigPhoto
                            visible: page.hasPicture
                            horizontalAlignment: HorizontalAlignment.Fill
                            verticalAlignment: VerticalAlignment.Fill
                            imageSource: page.hasPicture ? "file://" + page.info.picture : ""
                            scalingMethod: ScalingMethod.AspectFill
                            loadEffect: ImageViewLoadEffect.None
                            focusPolicy: FocusPolicy.KeyAndTouch
                            opacity: bigPhoto.focused ? 0.6 : 1
                            gestureHandlers: TapHandler {
                                onTapped: nav.showPhoto(page.info.picture)
                            }
                            keyListeners: KeyListener {
                                onKeyReleased: {
                                    if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                        nav.showPhoto(page.info.picture)
                                    }
                                }
                            }
                        }
                    }
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        multiline: true
                        textStyle.textAlign: TextAlign.Center
                        // Saved in the address book while open: the chat keeps the row it opened with.
                        text: page.chat ? (chats.revision >= 0 && chats.chat(page.chat.jid).name) || page.chat.name : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize * 1.3
                    }
                    // A business's verified name, when it isn't the name above (the phone shows it so).
                    Label {
                        visible: page.business && !!page.info.business.name && page.info.business.name != page.chat.name
                        horizontalAlignment: HorizontalAlignment.Center
                        text: page.business ? "~" + page.info.business.name : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: ui.palette.secondaryTextOnPlain
                    }
                    // In the primary colour, as a link: a tap saves the number in the phone's contacts.
                    // The Classic's trackpad reaches the number too: it takes the focus, dimmed, and a click (Enter) saves it (the user, 2026-11-20).
                    Label {
                        id: phoneLink
                        visible: !!page.info.phone
                        horizontalAlignment: HorizontalAlignment.Center
                        focusPolicy: FocusPolicy.KeyAndTouch
                        opacity: phoneLink.focused ? 0.5 : 1
                        text: page.info.phone || ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: ui.palette.primary
                        gestureHandlers: TapHandler {
                            onTapped: contact.saveNumber(page.info.phone)
                        }
                        keyListeners: KeyListener {
                            onKeyReleased: {
                                if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                    contact.saveNumber(page.info.phone)
                                }
                            }
                        }
                    }
                }
                Busy {
                    running: !page.loaded && !page.failed
                    topPadding: zoom * ui.du(4)
                    text: qsTr("Loading…")
                }
                Label {
                    visible: page.failed
                    horizontalAlignment: HorizontalAlignment.Center
                    topMargin: zoom * ui.du(3)
                    multiline: true
                    textStyle.textAlign: TextAlign.Center
                    textStyle.color: ui.palette.secondaryTextOnPlain
                    text: qsTr("Couldn't load the contact's info. Check the connection and try again.")
                }
                Header {
                    visible: page.business
                    title: qsTr("Business account")
                    subtitle: qsTr("Uses WhatsApp Business")
                }
                // Padding, not margins: margins only space siblings, never from the container's edge.
                Container {
                    visible: page.business
                    horizontalAlignment: HorizontalAlignment.Fill
                    topPadding: zoom * ui.du(1.5)
                    leftPadding: zoom * ui.du(1.5)
                    rightPadding: zoom * ui.du(1.5)
                    bottomPadding: zoom * ui.du(1.5)
                    // Open now or closed, and today's hours; a tap (or the trackpad's click) shows the week.
                    Container {
                        id: hoursRow
                        visible: page.hasHours
                        horizontalAlignment: HorizontalAlignment.Fill
                        focusPolicy: FocusPolicy.KeyAndTouch
                        opacity: hoursRow.focused ? 0.6 : 1
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        gestureHandlers: TapHandler {
                            onTapped: page.weekShown = !page.weekShown
                        }
                        keyListeners: KeyListener {
                            onKeyReleased: {
                                if (event.key == 13 || event.key == 10) { // Enter: the trackpad's click
                                    page.weekShown = !page.weekShown
                                }
                            }
                        }
                        Label {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            text: page.hasHours ? page.status() : ""
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                            textStyle.color: text == qsTr("Open now") ? Color.create("#1eaa61")
                                : text == qsTr("Closed now") ? Color.create("#e0453a") : ui.palette.secondaryTextOnPlain
                        }
                        Label {
                            text: page.hasHours ? page.dayText(new Date().getDay()) + (page.weekShown ? " ▴" : " ▾") : ""
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                        }
                    }
                    Container {
                        visible: page.hasHours && page.weekShown
                        horizontalAlignment: HorizontalAlignment.Fill
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        Label {
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            multiline: true
                            text: page.hasHours ? page.weekText(true) : ""
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                            textStyle.color: ui.palette.secondaryTextOnPlain
                        }
                        Label {
                            multiline: true
                            text: page.hasHours ? page.weekText(false) : ""
                            textStyle.textAlign: TextAlign.Right
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: nav.bodySize
                            textStyle.color: ui.palette.secondaryTextOnPlain
                        }
                    }
                    // Category, description, address, email and websites, each on its own lines; links active.
                    Label {
                        visible: page.business && !!page.info.business.category
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        text: page.business ? page.info.business.category : ""
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                    Label {
                        visible: page.business && !!page.info.business.description
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        text: page.business ? page.info.business.description : ""
                        content.flags: TextContentFlag.ActiveText
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                    Label {
                        visible: page.business && !!(page.info.business.address || page.info.business.email || page.info.business.websites.length)
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        text: page.business ? [page.info.business.address, page.info.business.email].concat(page.info.business.websites)
                            .filter(function (x) { return !!x }).join("\n") : ""
                        content.flags: TextContentFlag.ActiveText
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                }
                Header {
                    visible: page.loaded && !!page.info.about
                    title: qsTr("About")
                }
                // Padding, not margins: margins only space siblings, never from the container's edge.
                Container {
                    visible: page.loaded && !!page.info.about
                    horizontalAlignment: HorizontalAlignment.Fill
                    topPadding: zoom * ui.du(1.5)
                    leftPadding: zoom * ui.du(1.5)
                    rightPadding: zoom * ui.du(1.5)
                    bottomPadding: zoom * ui.du(1.5)
                    Label {
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        text: page.info.about || ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                }
                Container {
                    visible: page.hasChat
                    horizontalAlignment: HorizontalAlignment.Fill
                    topPadding: zoom * ui.du(1.5)
                    leftPadding: zoom * ui.du(1.5)
                    rightPadding: zoom * ui.du(1.5)
                    bottomPadding: zoom * ui.du(1.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: 1
                        }
                        text: qsTr("Mute notifications")
                    }
                    ToggleButton {
                        verticalAlignment: VerticalAlignment.Center
                        checked: page.muted
                        onCheckedChanged: page.setMuted(checked)
                    }
                }
                Header {
                    visible: page.groupCount > 0
                    title: page.groupCount == 1 ? qsTr("1 group in common") : qsTr("%1 groups in common").arg(page.groupCount)
                }
                Container {
                    id: groupsBox
                    horizontalAlignment: HorizontalAlignment.Fill
                }
            }
        }
    }
}
