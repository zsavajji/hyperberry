import bb.cascades 1.4

// The app's root (PLAN.md §7): a NavigationPane that holds no page of its own. It shows the
// chat list when linked, else the linking page: not linked, the root is the linking page, not a sheet.
NavigationPane {
    id: nav
    // A new link stays on the linking page while the engine is syncing its recent chats, so the
    // chat list opens full, or empty for an account with none (the user's, 2026-11-01).
    property bool linked: engine.linked && !engine.link.syncing
    // The one shared emoji style (PLAN.md §7): a FontFaceRule's font reaches only the styles
    // built on it, and each rule loads the font again. Pages take it as nav.emojiStyle, list
    // items through a property on their ListView.
    property variant emojiStyle: emoji.style
    // Text sizes in percent of the emoji style at 100 % (capitals 20 px on the Q10), in one place,
    // scaled by the text size setting. body: messages and chat names, the stock Text Messages
    // body's 21 px; small: previews, senders, quotes and service lines. List items reach them
    // through properties on their ListView, as they reach emojiStyle.
    property int bodySize: Math.round(105 * settings.textScale)
    property int smallSize: Math.round(85 * settings.textScale)
    // The chat a notification tap asked for, until the chat list is up to open it.
    property string pendingChat
    Menu.definition: MenuDefinition {
        settingsAction: SettingsActionItem {
            onTriggered: nav.push(settingsDef.createObject())
        }
    }
    attachedObjects: [
        ComponentDefinition {
            id: chatsDef
            source: "ChatsPage.qml"
        },
        ComponentDefinition {
            id: linkDef
            source: "LinkPage.qml"
        },
        ComponentDefinition {
            id: settingsDef
            source: "SettingsPage.qml"
        },
        ComponentDefinition {
            id: coverDef
            source: "cover.qml"
        },
        TextStyleDefinition {
            id: emoji
            fontFamily: "sans-serif, HBEmoji"
            rules: FontFaceRule {
                source: "asset:///fonts/HyperBerryEmoji.ttf"
                fontFamily: "HBEmoji"
            }
        }
    ]
    onPopTransitionEnded: page.destroy()
    onLinkedChanged: show()
    onCreationCompleted: {
        // Not a Connections: Cascades' QML has none.
        openRequests.openChat.connect(nav.openRequested)
        // setCover, as the skills have it: cover belongs to Application, not to a page.
        Application.setCover(coverDef.createObject())
        show()
    }

    // A page swap, so there's no Back to the other root: the new page goes in at the bottom
    // and every page above it (link number page, a chat) goes out.
    function show() {
        var page = (linked ? chatsDef : linkDef).createObject()
        if (count() == 0) {
            push(page)
            return
        }
        insert(0, page)
        while (count() > 1) {
            var old = at(1)
            remove(old)
            old.destroy()
        }
        openPending()
    }

    function openRequested(jid) {
        pendingChat = jid
        openPending()
    }

    // The chat replaces whatever is above the list, so Back goes to the list.
    function openPending() {
        if (pendingChat == "" || !linked || count() == 0)
            return
        var chat = chats.chat(pendingChat)
        pendingChat = ""
        if (!chat.jid)
            return
        backToList()
        at(0).openChat(chat)
    }

    // Every page above the list goes out. Deleting a chat (step 5.8b) uses it too.
    function backToList() {
        while (count() > 1) {
            var old = at(count() - 1)
            remove(old)
            old.destroy()
        }
    }
}
