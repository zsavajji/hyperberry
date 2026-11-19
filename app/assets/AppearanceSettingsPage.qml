import bb.cascades 1.4

// Settings > Appearance: saved as they change. Each lives in `settings`.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Appearance")
    }
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ScrollView {
            Container {
                topPadding: zoom * ui.du(2)
                leftPadding: zoom * ui.du(2)
                rightPadding: zoom * ui.du(2)
                bottomPadding: zoom * ui.du(2)
                DropDown {
                    title: qsTr("Theme")
                    options: [
                        Option { text: qsTr("Dark"); selected: settings.theme == 0 },
                        Option { text: qsTr("Bright"); selected: settings.theme == 1 }
                    ]
                    onSelectedIndexChanged: settings.theme = selectedIndex
                }
                Label {
                    multiline: true
                    topMargin: 0
                    bottomMargin: zoom * ui.du(2)
                    text: qsTr("Dark or bright colours for the whole app.")
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                // The Dark theme's page: pure black, or #181818 like Android's (the user's ask, 2026-11-19). The Bright theme has
                // one page colour, so there it is not offered.
                DropDown {
                    visible: settings.theme == 0
                    title: qsTr("Dark background")
                    options: [
                        Option { text: qsTr("Black"); selected: !settings.darkGrey },
                        Option { text: qsTr("Grey"); selected: settings.darkGrey }
                    ]
                    onSelectedIndexChanged: settings.darkGrey = selectedIndex == 1
                }
                Label {
                    visible: settings.theme == 0
                    multiline: true
                    topMargin: 0
                    bottomMargin: zoom * ui.du(2)
                    text: qsTr("The colour behind chats and lists: pure black, or a softer dark grey.")
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                DropDown {
                    title: qsTr("Text size")
                    options: [
                        Option { text: qsTr("Small"); selected: settings.textSize == 0 },
                        Option { text: qsTr("Normal"); selected: settings.textSize == 1 },
                        Option { text: qsTr("Large"); selected: settings.textSize == 2 }
                    ]
                    onSelectedIndexChanged: settings.textSize = selectedIndex
                }
                Label {
                    multiline: true
                    topMargin: 0
                    bottomMargin: zoom * ui.du(2)
                    text: qsTr("The size of the text in chats and in the chat list.")
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
                DropDown {
                    title: qsTr("Interface")
                    options: [
                        Option { text: qsTr("Normal"); selected: !settings.compact },
                        Option { text: qsTr("Compact"); selected: settings.compact }
                    ]
                    onSelectedIndexChanged: settings.compact = selectedIndex == 1
                }
                Label {
                    multiline: true
                    topMargin: 0
                    bottomMargin: zoom * ui.du(2)
                    text: qsTr("Compact fits more chats on the screen, with smaller pictures and rows.")
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.smallSize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
        }
    }
}
