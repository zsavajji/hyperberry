import bb.cascades 1.4

// One row of the address book (NewChatPage, NewGroupPage): the tile the chat will have in the
// chat list, the name, a 2 px separator, and a check when the contact is chosen. contact is a
// row of ContactPicker's model; the view supplies the emoji style and sizes as the chat list's.
Container {
    property variant contact
    id: row
    horizontalAlignment: HorizontalAlignment.Fill
    background: row.ListItem.active ? Color.create(row.ListItem.view.pressColor) : Color.Transparent
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
            background: Color.create(row.contact.color)
            layout: DockLayout {}
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                text: row.contact.initials
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
            text: row.contact.name
            textFormat: TextFormat.Plain
            textStyle.base: row.ListItem.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: row.ListItem.view.bodySize
        }
        ImageView {
            visible: !!row.contact.chosen
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
        background: ui.palette.plain
    }
}
