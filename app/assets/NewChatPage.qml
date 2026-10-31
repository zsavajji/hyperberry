import bb.cascades 1.4
import hyperberry 1.0

// New chat (PLAN.md §7, step 3.6): the address book with type-to-filter. A tap, or Enter in the
// search field, picks the first contact shown; ChatsPage opens that chat, or an empty one the
// first message creates.
Page {
    id: page
    signal picked(variant chat)
    titleBar: TitleBar {
        title: qsTr("New chat")
    }
    attachedObjects: ContactPicker {
        id: picker
        link: engine
    }
    onCreationCompleted: picker.load()
    // Called after the push, so on the Q10 typing filters at once (as ChatPage.focusComposer).
    function focusSearch() {
        search.requestFocus()
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
                TextField {
                    id: search
                    hintText: qsTr("Search contacts")
                    input.submitKey: SubmitKey.Search
                    input.onSubmitted: {
                        if (picker.shown > 0) {
                            page.picked(picker.model.data([0]))
                        }
                    }
                    onTextChanging: picker.filter(text)
                }
            }
            ListView {
                // The rows reach the shared emoji style and sizes through the view, as the chat list's do.
                property variant emojiStyle: nav.emojiStyle
                property int bodySize: nav.bodySize
                property int smallSize: nav.smallSize
                dataModel: picker.model
                onTriggered: page.picked(dataModel.data(indexPath))
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
                                text: ListItemData.name
                                textFormat: TextFormat.Plain
                                textStyle.base: row.ListItem.view.emojiStyle
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: row.ListItem.view.bodySize
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
            running: !picker.loaded
            text: qsTr("Loading…")
        }
        Label {
            visible: picker.loaded && picker.shown == 0
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Center
            multiline: true
            textStyle.textAlign: TextAlign.Center
            textStyle.color: ui.palette.secondaryTextOnPlain
            text: picker.total == 0 ? qsTr("No contacts yet. Your phone sends them shortly after linking.") : qsTr("No contact matches.")
        }
    }
}
