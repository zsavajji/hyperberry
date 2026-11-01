import bb.cascades 1.4

// A group's info (PLAN.md §7, step 3.5), read-only but for a tap on a participant, which opens
// their contact page (step 5.8). ctl is the chat page's ChatController, which
// asked the engine for group.info; the chat page stays under this one, so ctl outlives it.
Page {
    id: page
    property variant chat
    property variant ctl
    property variant info: ctl ? ctl.groupInfo : {}
    property bool loaded: info.participants !== undefined
    property bool failed: !loaded && engine.failure.cmd == "group.info"
    // The full-size picture the engine fetched with the info; its file name has the picture's id.
    property bool hasPicture: !!info.picture
    onInfoChanged: fill()
    onCtlChanged: fill()
    titleBar: TitleBar {
        title: qsTr("Group info")
    }
    attachedObjects: [
        ArrayDataModel {
            id: people
        },
        ComponentDefinition {
            id: contactDef
            source: "ContactInfoPage.qml"
        }
    ]
    // Not our own row. The chat's row when there is a one-to-one chat (participants are keyed LID
    // first, as chats are); else a row built like ContactPicker's, so the page and its Message
    // work for a person with no chat yet. The participant's colour is the sender colour: fine
    // for a tile here.
    function openPerson(p) {
        if (p.me) {
            return
        }
        var c = contactDef.createObject()
        var row = chats.chat(p.jid)
        c.chat = row.jid ? row : {"kind": 0, "jid": p.jid, "name": p.name || p.jid.split("@")[0],
                                  "initials": p.initials, "color": p.color}
        nav.push(c)
    }
    function fill() {
        people.clear()
        if (info && info.participants) {
            people.append(info.participants)
        }
    }
    Container {
        layout: DockLayout {}
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        Container {
            visible: page.loaded || !page.failed
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            // The group's tile and subject; the chat's name until the engine answers.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                topPadding: ui.du(1.5)
                bottomPadding: ui.du(1.5)
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    verticalAlignment: VerticalAlignment.Center
                    preferredWidth: ui.du(7)
                    preferredHeight: ui.du(7)
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
                        textStyle.fontSizeValue: 118
                        textStyle.color: Color.White
                    }
                    ImageView {
                        visible: page.hasPicture
                        horizontalAlignment: HorizontalAlignment.Fill
                        verticalAlignment: VerticalAlignment.Fill
                        imageSource: page.hasPicture ? "file://" + page.info.picture : ""
                        scalingMethod: ScalingMethod.AspectFill
                        loadEffect: ImageViewLoadEffect.None
                    }
                }
                Label {
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: ui.du(1.5)
                    topMargin: 0
                    bottomMargin: 0
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    multiline: true
                    text: page.info.subject || (page.chat ? page.chat.name : "")
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize
                }
            }
            // Padding, not margins: margins only space siblings, never from the container's edge.
            Container {
                visible: !!page.info.description
                horizontalAlignment: HorizontalAlignment.Fill
                leftPadding: ui.du(1.5)
                rightPadding: ui.du(1.5)
                bottomPadding: ui.du(1.5)
                Label {
                    horizontalAlignment: HorizontalAlignment.Fill
                    multiline: true
                    text: page.info.description || ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            // From info, not people.size(): a method call in a binding isn't re-read when the model fills.
            Header {
                visible: page.loaded
                title: !page.loaded ? "" : page.info.participants.length == 1 ? qsTr("1 participant") : qsTr("%1 participants").arg(page.info.participants.length)
            }
            ListView {
                // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
                property variant emojiStyle: nav.emojiStyle
                property int bodySize: nav.bodySize
                property int smallSize: nav.smallSize
                dataModel: people
                onTriggered: page.openPerson(people.data(indexPath))
                listItemComponents: ListItemComponent {
                    type: ""
                    Container {
                        id: row
                        horizontalAlignment: HorizontalAlignment.Fill
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
                                background: Color.create(ListItemData.color)
                                layout: DockLayout {}
                                Label {
                                    horizontalAlignment: HorizontalAlignment.Center
                                    verticalAlignment: VerticalAlignment.Center
                                    text: ListItemData.initials
                                    textFormat: TextFormat.Plain
                                    textStyle.base: row.ListItem.view.emojiStyle
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
                                text: ListItemData.me ? qsTr("You") : ListItemData.name || qsTr("Unknown")
                                textFormat: TextFormat.Plain
                                textStyle.base: row.ListItem.view.emojiStyle
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: row.ListItem.view.bodySize
                            }
                            Label {
                                visible: ListItemData.admin
                                verticalAlignment: VerticalAlignment.Center
                                leftMargin: ui.du(1.5)
                                text: qsTr("Admin")
                                textStyle.base: row.ListItem.view.emojiStyle
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: row.ListItem.view.smallSize
                                textStyle.color: ui.palette.primary
                            }
                        }
                        Container {
                            horizontalAlignment: HorizontalAlignment.Fill
                            preferredHeight: ui.px(2)
                            background: ui.palette.plain
                        }
                    }
                }
            }
        }
        Busy {
            running: !page.loaded && !page.failed
            text: qsTr("Loading…")
        }
        Label {
            visible: page.failed
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            multiline: true
            textStyle.textAlign: TextAlign.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: qsTr("Couldn't load the group's info. Check the connection and try again.")
        }
    }
}
