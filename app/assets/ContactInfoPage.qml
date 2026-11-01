import bb.cascades 1.4
import hyperberry 1.0

// A person's info (step 5.8), read-only but for the mute toggle. chat is a chat list row, set
// right after createObject(). The page asks the engine itself (contact.info) through its own
// ContactInfo, so the answer goes with the page.
Page {
    id: page
    property variant chat
    property variant info: contact.info
    property bool loaded: info.groups !== undefined
    property bool failed: !loaded && engine.failure.cmd == "contact.info"
    // From info, not a method call: a binding isn't re-read when a method's result changes.
    property int groupCount: loaded ? info.groups.length : 0
    property bool muted: false
    onChatChanged: {
        // !!: a contact picked with no chat yet has no muted, and undefined fails a bool.
        muted = !!chat.muted
        contact.ask(chat.jid)
    }
    onInfoChanged: fill()
    titleBar: TitleBar {
        title: qsTr("Contact info")
    }
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
                horizontalAlignment: HorizontalAlignment.Fill
                gestureHandlers: TapHandler {
                    onTapped: nav.openRequested(row.group.jid)
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    minHeight: ui.du(9.4)
                    leftPadding: ui.du(1.5)
                    rightPadding: ui.du(1.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(7)
                        preferredHeight: ui.du(7)
                        background: row.group ? Color.create(row.group.color) : Color.Transparent
                        layout: DockLayout {}
                        Label {
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            text: row.group ? row.group.initials : ""
                            textFormat: TextFormat.Plain
                            textStyle.base: nav.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 118
                            textStyle.color: Color.White
                        }
                    }
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        leftMargin: ui.du(1.5)
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
                    background: ui.palette.plain
                }
            }
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
    ScrollView {
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            // The tile, name and number; the chat's own until the engine answers.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                topPadding: ui.du(3)
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                Container {
                    horizontalAlignment: HorizontalAlignment.Center
                    preferredWidth: ui.du(20)
                    preferredHeight: ui.du(20)
                    background: page.chat ? Color.create(page.chat.color) : Color.Transparent
                    layout: DockLayout {}
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        verticalAlignment: VerticalAlignment.Center
                        text: page.chat ? page.chat.initials : ""
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 300
                        textStyle.color: Color.White
                    }
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    multiline: true
                    textStyle.textAlign: TextAlign.Center
                    text: page.chat ? page.chat.name : ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize * 1.3
                }
                Label {
                    visible: !!page.info.phone
                    horizontalAlignment: HorizontalAlignment.Center
                    text: page.info.phone || ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            Busy {
                running: !page.loaded && !page.failed
                topPadding: ui.du(4)
                text: qsTr("Loading…")
            }
            Label {
                visible: page.failed
                horizontalAlignment: HorizontalAlignment.Center
                topMargin: ui.du(3)
                multiline: true
                textStyle.textAlign: TextAlign.Center
                textStyle.color: ui.palette.secondaryTextOnPlain
                text: qsTr("Couldn't load the contact's info. Check the connection and try again.")
            }
            Header {
                visible: page.loaded && !!page.info.about
                title: qsTr("About")
            }
            // Padding, not margins: margins only space siblings, never from the container's edge.
            Container {
                visible: page.loaded && !!page.info.about
                horizontalAlignment: HorizontalAlignment.Fill
                topPadding: ui.du(1.5)
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                bottomPadding: ui.du(1.5)
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
                horizontalAlignment: HorizontalAlignment.Fill
                topPadding: ui.du(1.5)
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                bottomPadding: ui.du(1.5)
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
