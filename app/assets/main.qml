import bb.cascades 1.4

// The app's root (PLAN.md §7): a NavigationPane that holds no page of its own. It shows the
// chat list when linked, else the linking page: not linked, the root is the linking page, not a sheet.
NavigationPane {
    id: nav
    property bool linked: engine.linked
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
        }
    ]
    onPopTransitionEnded: page.destroy()
    onLinkedChanged: show()
    onCreationCompleted: show()

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
    }
}
