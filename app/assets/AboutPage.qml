import bb.cascades 1.4

// About, from the swipe-down menu beside Settings, laid out as My profile (ProfilePage.qml): the
// logo for the picture, the app's name, its version where the number goes, then what it is, the
// author's GitHub and the thanks to whatsapp-light's author, each opening in the browser.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("About")
    }
    ScrollView {
        Container {
            horizontalAlignment: HorizontalAlignment.Fill
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                topPadding: zoom * ui.du(3)
                leftPadding: zoom * ui.du(1.5)
                rightPadding: zoom * ui.du(1.5)
                // 16 du is the icon's own 144 px on the Q10.
                ImageView {
                    horizontalAlignment: HorizontalAlignment.Center
                    preferredWidth: zoom * ui.du(16)
                    preferredHeight: zoom * ui.du(16)
                    scalingMethod: ScalingMethod.AspectFit
                    imageSource: "asset:///images/icon-144.png"
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    bottomMargin: 0
                    text: "HyperBerry"
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize * 1.3
                }
                Label {
                    horizontalAlignment: HorizontalAlignment.Center
                    topMargin: 0
                    text: appVersion
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize
                    textStyle.color: ui.palette.secondaryTextOnPlain
                }
            }
            Header {
                title: qsTr("About")
            }
            // Padding, not margins: margins only space siblings, never from the container's edge.
            Container {
                horizontalAlignment: HorizontalAlignment.Fill
                topPadding: zoom * ui.du(1.5)
                leftPadding: zoom * ui.du(1.5)
                rightPadding: zoom * ui.du(1.5)
                bottomPadding: zoom * ui.du(1.5)
                Label {
                    horizontalAlignment: HorizontalAlignment.Fill
                    multiline: true
                    text: qsTr("A client for WhatsApp based on hypermeow and meowcaller, for BlackBerry 10.")
                    textFormat: TextFormat.Plain
                    textStyle.fontSize: FontSize.PercentageValue
                    textStyle.fontSizeValue: nav.bodySize
                }
            }
            Header {
                title: qsTr("Developer")
            }
            LinkRow {
                text: "github.com/zsavajji"
                url: "https://github.com/zsavajji"
            }
            Header {
                title: qsTr("Thanks")
            }
            LinkRow {
                text: qsTr("vbbot, for whatsapp-light's patches to meowcaller")
                url: "https://github.com/vbbot/whatsapp-light"
            }
        }
    }
}
