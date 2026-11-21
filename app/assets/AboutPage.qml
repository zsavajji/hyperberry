import bb.cascades 1.4

// About, from the swipe-down menu beside Settings, laid out as My profile (ProfilePage.qml): the
// logo for the picture, the app's name, its version where the number goes, then what it is, the
// author's GitHub and the thanks to whatsapp-light's author, then the open source libraries with their
// licences and the app's own licence, each opening in the browser.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("About")
    }
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
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
                    // The user, 2026-11-21: say it's unofficial, not Meta's, and at your own risk (the README's note).
                    Label {
                        horizontalAlignment: HorizontalAlignment.Fill
                        multiline: true
                        topMargin: zoom * ui.du(1.5)
                        text: qsTr("HyperBerry is unofficial: it is not affiliated with or endorsed by WhatsApp or Meta. Using it may break WhatsApp's terms of service, so use it at your own risk.")
                        textFormat: TextFormat.Plain
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: nav.bodySize
                        textStyle.color: ui.palette.secondaryTextOnPlain
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
                // What the app and its engine are built from (engine/go.mod, docker/Dockerfile, the fetch-*.sh scripts), with
                // their licences; a row opens the project's page. Names and licences stay untranslated.
                Header {
                    title: qsTr("Open source libraries")
                }
                LinkRow {
                    text: "hypermeow (whatsmeow) · MPL 2.0, MIT"
                    url: "https://github.com/polymorfa/hypermeow"
                }
                LinkRow {
                    text: "libsignal-protocol-go · GPL 3.0"
                    url: "https://github.com/polymorfa/libsignal-protocol-go"
                }
                LinkRow {
                    text: "meowcaller · MIT"
                    url: "https://github.com/purpshell/meowcaller"
                }
                LinkRow {
                    text: "Pion (DTLS, SCTP, Opus) · MIT"
                    url: "https://github.com/pion"
                }
                LinkRow {
                    text: "libopus 1.6.1 · BSD 3-Clause"
                    url: "https://opus-codec.org"
                }
                LinkRow {
                    text: "go-sqlite3 1.14.52 · MIT; SQLite public domain"
                    url: "https://github.com/mattn/go-sqlite3"
                }
                LinkRow {
                    text: "Protocol Buffers for Go 1.36.12 · BSD 3-Clause"
                    url: "https://github.com/protocolbuffers/protobuf-go"
                }
                LinkRow {
                    text: "websocket 1.8.15 (Coder) · ISC"
                    url: "https://github.com/coder/websocket"
                }
                LinkRow {
                    text: "zerolog 1.35.1 · MIT"
                    url: "https://github.com/rs/zerolog"
                }
                LinkRow {
                    text: "go-mp3 0.3.4 · Apache License 2.0"
                    url: "https://github.com/hajimehoshi/go-mp3"
                }
                LinkRow {
                    text: "mautrix go-util · MPL 2.0"
                    url: "https://github.com/mautrix/go-util"
                }
                LinkRow {
                    text: "Go 1.27.2 for QNX (go-qnx) · BSD 3-Clause"
                    url: "https://github.com/XIAZY/go-qnx"
                }
                LinkRow {
                    text: "Go x/crypto, x/net, x/sys (go-sys-qnx) · BSD 3-Clause"
                    url: "https://github.com/XIAZY/go-sys-qnx"
                }
                LinkRow {
                    text: "libwebp 1.4.0 · BSD 3-Clause"
                    url: "https://chromium.googlesource.com/webm/libwebp"
                }
                LinkRow {
                    text: "QR Code generator 1.8.0 (Nayuki) · MIT"
                    url: "https://www.nayuki.io/page/qr-code-generator-library"
                }
                LinkRow {
                    text: "OpenMAX IL headers (Android's) · Apache 2.0, Khronos (MIT-style)"
                    url: "https://android.googlesource.com/platform/frameworks/native/+/refs/heads/main/headers/media_plugin/media/openmax"
                }
                LinkRow {
                    text: "Noto Color Emoji · SIL Open Font License 1.1"
                    url: "https://github.com/googlefonts/noto-emoji"
                }
                LinkRow {
                    text: "Unicode emoji data 18.0 · Unicode License v3"
                    url: "https://www.unicode.org/emoji/"
                }
                Header {
                    title: qsTr("License")
                }
                LinkRow {
                    text: "HyperBerry · GNU GPL 3.0"
                    url: "https://www.gnu.org/licenses/gpl-3.0.html"
                }
            }
        }
    }
}
