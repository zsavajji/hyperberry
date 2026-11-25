import bb.cascades 1.4

// The Active Frame (PLAN.md step 5.2), the user's design of 2026-11-17. The Q10 shows all of its 310 x 211 px,
// above the system's title strip (screenshot, 0.0.1.76), so nothing may run past the bottom. Idle: the app's
// glyph, and "All caught up" low on the left. Active (unread chats or missed calls): two equal rows on a panel,
// Messages and Missed calls, each a big count, its label and a line of detail. Margins are set, since unset
// ones put about 20 px between labels. Type is sized in percent: at 100 % BodyText and SmallText draw capitals
// 20 px tall in a cover (measured on the Q10, 0.2.0.354), so a size is the mockup's cap height over 0.2. It takes no touch. Created by main.qml, so nav, chats and callList are in its
// scope. The rows are CoverRow.qml; on the Passport main.qml sets PassportCover.qml, which holds this as its big frame.
SceneCover {
    id: cover
    // It follows the theme (the user, 2026-11-17): Bright's page and text, its accent; Dark keeps black and the
    // bright green, which reads on black where Dark's #12543a accent would not. Written out: a cover's scene
    // may not share the app's palette.
    property bool bright: settings.theme == 1
    property variant ink: bright ? Color.create("#111111") : Color.White
    property variant quiet: bright ? Color.create("#5E5E5E") : Color.Gray
    property variant accent: Color.create(bright ? "#1eaa61" : "#1BD862")
    // The panel lifts the rows off the home screen's wallpaper behind the frame; the idle page is plain.
    property variant panel: bright ? Color.White : Color.create("#2B2B2B")
    // Dark with Settings > Appearance > Dark background on Grey: the frame is the app's grey #181818, idle or not (the user,
    // 2026-11-19).
    property bool grey: !bright && settings.darkGrey
    property variant line: bright ? Color.create("#D0D0D0") : Color.create("#464646")
    property bool idle: chats.unreadTotal == 0 && callList.missedNew == 0
    // The Passport (1440 px): its frame is 440 x 486 (440 x 195 short), and the Q10's sizes looked small there (the user,
    // 2026-11-25, as Tengram's). Glyph, type and spacing are k times the Q10's, and the rows fill the frame; the other
    // phones keep 1.
    property real k: screenWidth >= 1440 ? 1.35 : 1
    // The text grows less than the rest: at 1.35 it was a little too big (the user, 2026-11-25).
    property real kt: k > 1 ? 1.2 : 1
    content: Container {
        background: grey ? Color.create("#181818") : idle ? (bright ? Color.White : Color.Black) : panel
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
            leftPadding: 18 * k
            topPadding: 17 * k
            ImageView {
                imageSource: "asset:///images/cover_glyph.png"
                filterColor: accent
                preferredWidth: 64 * k
                preferredHeight: 64 * k
            }
        }
        Container {
            visible: idle
            horizontalAlignment: HorizontalAlignment.Left
            verticalAlignment: VerticalAlignment.Bottom
            leftPadding: 18 * k
            bottomPadding: 20 * k
            Label {
                text: engine.linked ? qsTr("All caught up") : "HyperBerry"
                textStyle.base: SystemDefaults.TextStyles.BodyText
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 95 * kt
                textStyle.color: ink
                topMargin: 0
                bottomMargin: 0
            }
            Label {
                text: engine.linked ? qsTr("No new messages or calls") : qsTr("Not logged in")
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontSize: FontSize.PercentageValue
                // 47: at 55 the Italian line ran to the frame's edge (the user, 0.2.0.356).
                textStyle.fontSizeValue: 47 * kt
                textStyle.fontWeight: FontWeight.Bold
                textStyle.color: quiet
                topMargin: 4 * k
                bottomMargin: 0
            }
        }
        // Active: two rows of the same height, the separator between. Details take the emoji style, so names' emoji draw
        // (they do, in a cover: 0.0.1.76). A count of 0 is quiet, not accent.
        Container {
            visible: !idle
            preferredWidth: 310
            preferredHeight: 211
            horizontalAlignment: k > 1 ? HorizontalAlignment.Fill : HorizontalAlignment.Left
            verticalAlignment: k > 1 ? VerticalAlignment.Fill : VerticalAlignment.Top
            layout: StackLayout {
            }
            CoverRow {
                count: chats.unreadTotal
                label: qsTr("Messages")
                // The unread chats' names, newest first, ", " between them (ChatListModel builds the line).
                detail: chats.unreadChats
                ink: cover.ink
                quiet: cover.quiet
                accent: cover.accent
                emojiStyle: nav.emojiStyle
                k: cover.k
                kt: cover.kt
            }
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                // Fixed: the rows' share of the height squeezed it away (the user, 2026-11-25, in Tengram: no separators).
                minHeight: 2
                maxHeight: 2
                background: line
                topMargin: 0
                bottomMargin: 0
            }
            CoverRow {
                count: callList.missedNew
                label: qsTr("Missed calls")
                // The newest one, "Name · time" (CallsModel).
                detail: callList.missedLast
                ink: cover.ink
                quiet: cover.quiet
                accent: cover.accent
                emojiStyle: nav.emojiStyle
                k: cover.k
                kt: cover.kt
            }
        }
    }
}
