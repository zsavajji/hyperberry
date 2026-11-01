import bb.cascades 1.4

// The chat page's title bar (PLAN.md §7, step 3.1), moved out of ChatPage to make room: the tile,
// name and status line. chat is a ChatListModel row; a tap on the bar emits tapped().
TitleBar {
    id: bar
    property variant chat
    property string status
    signal tapped
    kind: TitleBarKind.FreeForm
    // The status line must stay; the default let the bar scroll away with the list (Q10).
    scrollBehavior: TitleBarScrollBehavior.Sticky
    // Sized as the stock Text Messages conversation (Q10, 2026-10-31): an 85 px bar, the
    // picture 81 px square flush at the right, 2 px from the bar's edges and 7 px from the
    // screen's.
    kindProperties: FreeFormTitleBarKindProperties {
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            leftPadding: ui.du(1.5)
            rightPadding: ui.px(7)
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            gestureHandlers: TapHandler {
                onTapped: bar.tapped()
            }
            Container {
                verticalAlignment: VerticalAlignment.Center
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                // Two lines fit 85 px only without the labels' default margins (about 20 px),
                // and below the stock title's 25 px capitals (125 %; 100 % draws 20 px):
                // the name 22 px (110 %), the status 15 px (75 %). Q10, 2026-10-31.
                Label {
                    bottomMargin: 0
                    text: bar.chat ? bar.chat.name : ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: 110
                }
                Label {
                    visible: bar.status != ""
                    topMargin: 0
                    text: bar.status
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: 75
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            // The initials tile where the stock app has the picture; photos come in Phase 6.
            Container {
                verticalAlignment: VerticalAlignment.Center
                leftMargin: ui.du(1.5)
                preferredWidth: ui.px(81)
                preferredHeight: ui.px(81)
                background: bar.chat ? Color.create(bar.chat.color) : Color.Transparent
                layout: DockLayout {}
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    verticalAlignment: VerticalAlignment.Center
                    text: bar.chat ? bar.chat.initials : ""
                    textFormat: TextFormat.Plain
                    textStyle.base: nav.emojiStyle
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: 150
                    textStyle.color: Color.White
                }
            }
        }
    }
}
