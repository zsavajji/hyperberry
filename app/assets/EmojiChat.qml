import bb.cascades 1.4

// Step 0.7's realistic check (PLAN.md §9): 300 chat-like messages in a ListView, mostly text,
// emoji at a typical density from a pool of common and new ones, all at body size through one
// shared FontFaceRule. Does it scroll smoothly? The messages come from a fixed seed, so every
// run shows the same list.
Sheet {
    id: sheet

    Page {
        titleBar: TitleBar {
            title: qsTr("Emoji chat test")
            dismissAction: ActionItem {
                title: qsTr("Close")
                onTriggered: sheet.close()
            }
        }

        ListView {
            id: list
            // The items reach the shared style through ListItem.view.
            property variant emojiStyle: emoji.style

            dataModel: ArrayDataModel {
                id: messages
            }

            listItemComponents: [
                ListItemComponent {
                    Container {
                        id: item
                        topPadding: ui.du(1)
                        bottomPadding: ui.du(1)
                        leftPadding: ui.du(2)
                        rightPadding: ui.du(2)
                        Label {
                            text: ListItemData
                            multiline: true
                            textStyle.base: item.ListItem.view.emojiStyle
                        }
                    }
                }
            ]

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

            onCreationCompleted: {
                var pool = ["😂", "❤️", "🤣", "👍", "😭", "🙏", "😘", "🥰", "😍", "😊", "🎉",
                    "😁", "💕", "🥺", "😅", "🔥", "🤦", "🤷", "🙄", "😆", "🤗", "😉", "🎂",
                    "🤔", "👏", "🙂", "😳", "🥳", "😎", "👌", "💜", "😔", "💪", "✨", "👀",
                    "😋", "😏", "😢", "🫠", "🫡", "🥹", "🫶", "🫣", "🫨", "🫩"];
                var words = ["ok", "see", "you", "later", "tonight", "dinner", "at", "eight",
                    "did", "you", "get", "my", "message", "yes", "no", "maybe", "sure",
                    "thanks", "so", "much", "the", "train", "is", "late", "again", "call",
                    "me", "when", "you", "can", "happy", "birthday", "love", "it", "what",
                    "time", "tomorrow", "morning", "coffee", "great", "news", "photos"];
                var seed = 7;
                function next(n) {
                    seed = (seed * 1103515245 + 12345) % 2147483648;
                    return seed % n;
                }
                var all = [];
                for (var i = 0; i < 300; i++) {
                    var text = "";
                    if (next(10) == 0) {
                        // An emoji-only message.
                        for (var e = next(3); e >= 0; e--)
                            text += pool[next(pool.length)];
                    } else {
                        for (var w = 3 + next(18); w > 0; w--) {
                            text += (text ? " " : "") + words[next(words.length)];
                            if (next(7) == 0)
                                text += " " + pool[next(pool.length)];
                        }
                    }
                    all.push(text);
                }
                messages.append(all);
            }
        }
    }
}
