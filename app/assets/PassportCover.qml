import bb.cascades 1.4

// The Passport's Active Frame (the user, 2026-11-25, as Tengram's). Its home screen shows a big frame (440 x 486) or, with
// more apps open, a short one (440 x 195), where a lone SceneCover gave way to a picture of the app's screen. So a
// MultiCover: the big one is Cover.qml, the short one the three counts side by side under their icons: unread messages,
// missed calls and unseen stories. main.qml sets it on the Passport only; the other phones keep Cover.qml alone. Created
// by main.qml, so nav, chats, callList, storyList and settings are in its scope.
MultiCover {
    Cover {
        id: big
        MultiCover.level: CoverDetailLevel.High
    }
    SceneCover {
        MultiCover.level: CoverDetailLevel.Medium
        content: Container {
            background: big.grey ? Color.create("#181818") : big.panel
            horizontalAlignment: HorizontalAlignment.Fill
            verticalAlignment: VerticalAlignment.Fill
            leftPadding: 10
            rightPadding: 10
            layout: StackLayout {
                orientation: LayoutOrientation.LeftToRight
            }
            CoverCount {
                count: chats.unreadTotal
                icon: "asset:///images/message.png"
                quiet: big.quiet
                accent: big.accent
            }
            CoverCount {
                count: callList.missedNew
                icon: "file:///usr/share/icons/ic_phone.png"
                quiet: big.quiet
                accent: big.accent
            }
            CoverCount {
                count: storyList.unseen
                icon: "asset:///images/stories.png"
                quiet: big.quiet
                accent: big.accent
            }
        }
    }
}
