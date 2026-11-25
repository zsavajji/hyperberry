import bb.cascades 1.4

// One row of the address book (NewChatPage, NewGroupPage) or of a group's members (CallPickPage):
// the tile the chat will have in the chat list, its picture over it when the row has one, the name,
// a 2 px separator, a check when the contact is chosen and, for the rows that carry a time, when (a story's
// viewers). contact is a row of ContactPicker's model; the view supplies the emoji style and sizes as the
// chat list's.
Container {
    property variant contact
    id: row
    horizontalAlignment: HorizontalAlignment.Fill
    background: row.ListItem.active ? Color.create(row.ListItem.view.pressColor) : Color.Transparent
    Container {
        id: line
        horizontalAlignment: HorizontalAlignment.Fill
        // The chat list's sizes (ChatRows.cpp): Compact's 7 du tile in a 9.4 du row, else 8.2 in 11.4 (the user, 2026-11-25:
        // search's pictures were small).
        property bool compact: settings.compact
        minHeight: zoom * ui.du(compact ? 9.4 : 11.4)
        leftPadding: zoom * ui.du(1.5)
        rightPadding: zoom * ui.du(1.5)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        Container {
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: zoom * ui.du(line.compact ? 7 : 8.2)
            preferredHeight: zoom * ui.du(line.compact ? 7 : 8.2)
            background: Color.create(row.contact.color)
            layout: DockLayout {}
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                text: row.contact.initials
                textFormat: TextFormat.Plain
                textStyle.base: row.ListItem.view.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 118 * zoom
                textStyle.color: Color.White
            }
            // The picture over the initials, when the row has one (a group's members do, from
            // group.info: the call picker's and the group page's).
            ImageView {
                visible: !!row.contact.photo
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
                imageSource: row.contact.photo ? "file://" + row.contact.photo : ""
                scalingMethod: ScalingMethod.AspectFill
                loadEffect: ImageViewLoadEffect.None
            }
        }
        Label {
            verticalAlignment: VerticalAlignment.Center
            leftMargin: zoom * ui.du(1.5)
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            text: row.contact.name
            textFormat: TextFormat.Plain
            textStyle.base: row.ListItem.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: row.ListItem.view.bodySize
        }
        // When, at the right, for the rows that say (a story's viewers).
        Label {
            visible: !!row.contact.time
            verticalAlignment: VerticalAlignment.Center
            text: row.contact.time ? row.contact.time : ""
            textFormat: TextFormat.Plain
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: row.ListItem.view.smallSize
            textStyle.color: ui.palette.secondaryTextOnPlain
        }
        ImageView {
            visible: !!row.contact.chosen
            verticalAlignment: VerticalAlignment.Center
            preferredWidth: zoom * ui.du(5)
            preferredHeight: zoom * ui.du(5)
            imageSource: "asset:///images/tick_action.png"
            filterColor: ui.palette.primary
        }
    }
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        preferredHeight: ui.px(2)
        background: row.ListItem.view.lineColor
    }
}
