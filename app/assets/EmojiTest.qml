import bb.cascades 1.4

// Step 0.7 (PLAN.md §9): modern emoji in Cascades text. Cascades draws text with Monotype iType,
// not fontconfig, and the system splices Slate Pro with its version-1 emoji font only for
// Unicode 6 code points (monotype/SlatePro-Regular_Color.cfr). A FontFaceRule loads
// HyperBerryEmoji.ttf, derived from Google's Noto Color Emoji by scripts/build-emoji-font.py,
// after the system's text font. iType applies no GSUB, so sequences show as their parts
// (user, 2026-10-29: fine).
//
// The sample holds one emoji per emoji version, E0.6 to E18.0: for each, the first
// fully-qualified one in Unicode's emoji-test.txt 18.0, or a sequence for the versions that
// added only sequences (E2.0, E4.0, E12.1, E13.1, E15.1). Then a ZWJ family, a skin tone, a
// flag and a keycap.
Sheet {
    id: sheet

    property string sample: "😃 😐 😀 👁️‍🗨️ 🤣 👱‍♀️ 🤩 🥰 🥱 🧑‍🦰 🥲 😶‍🌫️ 🫠 🫨 🙂‍↔️ 🫩 🫪 🫫 👨‍👩‍👧‍👦 👍🏽 🇮🇹 1️⃣"

    attachedObjects: [
        TextStyleDefinition {
            id: emoji
            base: SystemDefaults.TextStyles.BodyText
            fontFamily: "sans-serif, HBEmoji"
            rules: [
                FontFaceRule {
                    source: "asset:///fonts/HyperBerryEmoji.ttf"
                    fontFamily: "HBEmoji"
                }
            ]
        }
    ]

    Page {
        titleBar: TitleBar {
            title: qsTr("Emoji test")
            dismissAction: ActionItem {
                title: qsTr("Close")
                onTriggered: sheet.close()
            }
        }

        actions: [
            // The composer of §7. It has no textStyle, so it keeps the system's emoji.
            TextInputActionItem {
                text: sheet.sample
            }
        ]

        ScrollView {
            Container {
                leftPadding: ui.du(1)
                rightPadding: ui.du(1)

                Label {
                    text: qsTr("Label, body")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                }
                Label {
                    text: "Ab 12 " + sheet.sample
                    multiline: true
                    textStyle.base: emoji.style
                }
                Label {
                    text: qsTr("Label, extra large")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                }
                Label {
                    text: "Ab 12 " + sheet.sample
                    multiline: true
                    textStyle.base: emoji.style
                    textStyle.fontSize: FontSize.XLarge
                }
                Label {
                    text: qsTr("TextArea")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                }
                TextArea {
                    text: "Ab 12 " + sheet.sample
                    textStyle.base: emoji.style
                }
                Label {
                    text: qsTr("TextField")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                }
                TextField {
                    text: "Ab 12 " + sheet.sample
                    textStyle.base: emoji.style
                }
                Label {
                    text: qsTr("Label, system style (for comparison)")
                    textStyle.base: SystemDefaults.TextStyles.SubtitleText
                }
                Label {
                    text: "Ab 12 " + sheet.sample
                    multiline: true
                }
            }
        }
    }
}
