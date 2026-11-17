import bb.cascades 1.4

// The Active Frame (PLAN.md step 5.2), the user's design of 2026-11-17. The Q10 shows all of its 310 x 211 px,
// above the system's title strip (screenshot, 0.0.1.76), so nothing may run past the bottom. Idle: the app's
// glyph, and "All caught up" low on the left. Active (unread chats or missed calls): two equal rows on a panel,
// Messages and Missed calls, each a big count, its label and a line of detail. Margins are set, since unset
// ones put about 20 px between labels. Type is sized in percent: at 100 % BodyText and SmallText draw capitals
// 20 px tall in a cover (measured on the Q10, 0.2.0.354), so a size is the mockup's cap height over 0.2. It takes no touch. Created by main.qml, so nav, chats and callList are in its scope.
SceneCover {
    // It follows the theme (the user, 2026-11-17): Bright's page and text, its accent; Dark keeps black and the
    // bright green, which reads on black where Dark's #12543a accent would not. Written out: a cover's scene
    // may not share the app's palette.
    property bool bright: settings.theme == 1
    property variant ink: bright ? Color.create("#111111") : Color.White
    property variant quiet: bright ? Color.create("#5E5E5E") : Color.Gray
    property variant accent: Color.create(bright ? "#1eaa61" : "#1BD862")
    // The panel lifts the rows off the home screen's wallpaper behind the frame; the idle page is plain.
    property variant panel: bright ? Color.White : Color.create("#2B2B2B")
    property variant line: bright ? Color.create("#D0D0D0") : Color.create("#464646")
    property bool idle: chats.unreadTotal == 0 && callList.missedNew == 0
    content: Container {
        background: idle ? (bright ? Color.White : Color.Black) : panel
        preferredWidth: 310
        preferredHeight: 211
        layout: DockLayout {
        }
        // Idle. The glyph is white and tinted, as the bars' images are. Its 64 px box has 3 px of air above the
        // bubble, so the 17 puts the bubble's top at the mockup's 20. Padding, not margins: margins never space
        // from a container's edge, and the glyph sat in the corner (0.2.0.354).
        Container {
            visible: idle
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Top
            leftPadding: 18
            topPadding: 17
            ImageView {
                imageSource: "asset:///images/cover_glyph.png"
                filterColor: accent
            }
        }
        Container {
            visible: idle
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Bottom
            leftPadding: 18
            bottomPadding: 20
            Label {
                text: engine.linked ? qsTr("All caught up") : "HyperBerry"
                textStyle.base: SystemDefaults.TextStyles.BodyText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 95
                textStyle.color: ink
                topMargin: 0
                bottomMargin: 0
            }
            Label {
                text: engine.linked ? qsTr("No new messages or calls") : qsTr("Not logged in")
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                // 47: at 55 the Italian line ran to the frame's edge (the user, 0.2.0.356).
                textStyle.fontSizeValue: 47
                textStyle.fontWeight: FontWeight.Bold
                textStyle.color: quiet
                topMargin: 4
                bottomMargin: 0
            }
        }
        // Active: two rows of the same height, the separator between. A row's text column takes what the count
        // leaves (spaceQuota 1), so a long detail is elided at the frame's edge, not run past it. Details take the
        // emoji style, so names' emoji draw (they do, in a cover: 0.0.1.76). A count of 0 is quiet, not accent.
        Container {
            visible: !idle
            preferredWidth: 310
            preferredHeight: 211
            layout: StackLayout {
            }
            Container {
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                leftPadding: 10
                rightPadding: 10
                topMargin: 0
                bottomMargin: 0
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    preferredWidth: 46
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: 0
                    rightMargin: 0
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        text: chats.unreadTotal
                        textStyle.base: SystemDefaults.TextStyles.BodyText
                        textStyle.fontSize: FontSize.PercentageValue
                        // Three digits would not fit the column.
                        textStyle.fontSizeValue: chats.unreadTotal > 99 ? 95 : 140
                        textStyle.color: chats.unreadTotal > 0 ? accent : quiet
                        topMargin: 0
                        bottomMargin: 0
                    }
                }
                Container {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: 0
                    rightMargin: 0
                    Label {
                        text: qsTr("Messages")
                        textStyle.base: SystemDefaults.TextStyles.SmallText
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 65
                        textStyle.color: quiet
                        topMargin: 0
                        bottomMargin: 0
                    }
                    // The unread chats' names, newest first, ", " between them (ChatListModel builds the line).
                    Label {
                        text: chats.unreadTotal > 0 ? chats.unreadChats : qsTr("None")
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 55
                        textStyle.fontWeight: FontWeight.Bold
                        textStyle.color: ink
                        multiline: false
                        topMargin: 2
                        bottomMargin: 0
                    }
                }
            }
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                preferredHeight: 2
                background: line
                topMargin: 0
                bottomMargin: 0
            }
            Container {
                layoutProperties: StackLayoutProperties {
                    spaceQuota: 1
                }
                leftPadding: 10
                rightPadding: 10
                topMargin: 0
                bottomMargin: 0
                layout: StackLayout {
                    orientation: LayoutOrientation.LeftToRight
                }
                Container {
                    preferredWidth: 46
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: 0
                    rightMargin: 0
                    Label {
                        horizontalAlignment: HorizontalAlignment.Center
                        text: callList.missedNew
                        textStyle.base: SystemDefaults.TextStyles.BodyText
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: callList.missedNew > 99 ? 95 : 140
                        textStyle.color: callList.missedNew > 0 ? accent : quiet
                        topMargin: 0
                        bottomMargin: 0
                    }
                }
                Container {
                    layoutProperties: StackLayoutProperties {
                        spaceQuota: 1
                    }
                    verticalAlignment: VerticalAlignment.Center
                    leftMargin: 0
                    rightMargin: 0
                    Label {
                        text: qsTr("Missed calls")
                        textStyle.base: SystemDefaults.TextStyles.SmallText
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 65
                        textStyle.color: quiet
                        topMargin: 0
                        bottomMargin: 0
                    }
                    // The newest one, "Name · time" (CallsModel).
                    Label {
                        text: callList.missedNew > 0 ? callList.missedLast : qsTr("None")
                        textStyle.base: nav.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: 55
                        textStyle.fontWeight: FontWeight.Bold
                        textStyle.color: ink
                        multiline: false
                        topMargin: 2
                        bottomMargin: 0
                    }
                }
            }
        }
    }
}
