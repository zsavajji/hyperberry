import bb.cascades 1.4

// The app's root (PLAN.md §7): a NavigationPane that holds no page of its own. It shows the
// chat list when linked, else the linking page: not linked, the root is the linking page, not a sheet.
NavigationPane {
    id: nav
    property bool linked: engine.linked
    // The one shared emoji style (PLAN.md §7): a FontFaceRule's font reaches only the styles
    // built on it, and each rule loads the font again. Pages take it as nav.emojiStyle, list
    // items through a property on their ListView.
    property variant emojiStyle: emoji.style
    // Text sizes in percent of the emoji style at 100 % (capitals 20 px on the Q10), in one place
    // for the font size setting (5.4). body: messages and chat names, the stock Text Messages
    // body's 21 px; small: previews, senders, quotes and service lines. List items reach them
    // through properties on their ListView, as they reach emojiStyle.
    property int bodySize: 105
    property int smallSize: 85
    // The chat a notification tap asked for, until the chat list is up to open it.
    property string pendingChat
    Menu.definition: MenuDefinition {
        // Settings, far right in the application menu, come in step 5.4.
        settingsAction: SettingsActionItem {
            enabled: false
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
        while (count() > 1) {
            var old = at(count() - 1)
            remove(old)
            old.destroy()
        }
        at(0).openChat(chat)
    }
}
