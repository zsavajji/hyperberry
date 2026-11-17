import bb.cascades 1.4
import hyperberry 1.0

// Who sees a new story (the user's ask, 2026-11-12): the address book ticked as NewGroupPage ticks it, as the group
// call's picker lists its members, with "Everyone" on top. Everyone is the account's own status privacy settings
// (WhatsApp's default) and is ticked while nobody else is; ticking a contact unticks it, and a tap on it unticks the
// rest. Done emits chosen(jids): an empty list for Everyone. StoryComposer owns it; reset() before each story.
Sheet {
    id: sheet
    signal chosen(variant jids)
    function reset() {
        picker.clearChosen()
        search.text = ""
        picker.filter("")
    }
    attachedObjects: ContactPicker {
        id: picker
        link: engine
    }
    onCreationCompleted: picker.load()
    content: Page {
        titleBar: TitleBar {
            title: qsTr("Share with")
            dismissAction: ActionItem {
                title: qsTr("Cancel")
                onTriggered: sheet.close()
            }
            acceptAction: ActionItem {
                title: qsTr("Done")
                onTriggered: {
                    sheet.chosen(picker.chosen())
                    sheet.close()
                }
            }
        }
        Container {
            layout: DockLayout {}
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                // Padding, not margins: margins only space siblings, never from the container's edge.
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    leftPadding: ui.du(1.5)
                    rightPadding: ui.du(1.5)
                    topPadding: ui.du(1.5)
                    bottomPadding: ui.du(1.5)
                    // The wrapper carries Bright's field fill (nav.fieldColor); Dark keeps the system box.
                    Container {
                        horizontalAlignment: HorizontalAlignment.Fill
                        background: nav.fieldColor
                        leftPadding: settings.theme == 1 ? ui.du(1) : 0
                        TextField {
                            id: search
                            horizontalAlignment: HorizontalAlignment.Fill
                            backgroundVisible: settings.theme != 1
                            hintText: qsTr("Search contacts")
                            input.submitKey: SubmitKey.Search
                            onTextChanging: picker.filter(text)
                        }
                    }
                    Label {
                        visible: picker.chosenCount > 0
                        textStyle.color: ui.palette.secondaryTextOnPlain
                        text: qsTr("%1 chosen").arg(picker.chosenCount)
                    }
                }
                // Everyone: a row of the contact rows' size and look, ticked while nobody else is.
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    minHeight: ui.du(9.4)
                    leftPadding: ui.du(1.5)
                    rightPadding: ui.du(1.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    gestureHandlers: TapHandler {
                        onTapped: picker.clearChosen()
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(7)
                        preferredHeight: ui.du(7)
                        background: ui.palette.primary
                        layout: DockLayout {}
                        ImageView {
                            horizontalAlignment: HorizontalAlignment.Center
                            verticalAlignment: VerticalAlignment.Center
                            preferredWidth: ui.du(5)
                            preferredHeight: ui.du(5)
                            imageSource: "asset:///images/contacts_action.png"
                            filterColor: Color.White
                        }
                    }
                    Label {
                        verticalAlignment: VerticalAlignment.Center
                        leftMargin: ui.du(1.5)
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: 1
                        }
                        text: qsTr("Everyone")
                        textFormat: TextFormat.Plain
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                    }
                    ImageView {
                        visible: picker.chosenCount == 0
                        verticalAlignment: VerticalAlignment.Center
                        preferredWidth: ui.du(5)
                        preferredHeight: ui.du(5)
                        imageSource: "asset:///images/tick_action.png"
                        filterColor: ui.palette.primary
                    }
                }
                Container {
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: ui.px(2)
                    background: nav.lineColor
                }
                ListView {
                    // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
                    property variant emojiStyle: nav.emojiStyle
                    property int bodySize: nav.bodySize
                    property int smallSize: nav.smallSize
                    property string pressColor: nav.pressColor
                    property variant lineColor: nav.lineColor
                    dataModel: picker.model
                    onTriggered: picker.toggle(indexPath)
                    listItemComponents: ListItemComponent {
                        type: ""
                        ContactRow {
                            contact: ListItemData
                        }
                    }
                }
            }
            Busy {
                running: !picker.loaded
                text: qsTr("Loading…")
            }
        }
    }
}
