import bb.cascades 1.4

// Settings (step 5.4), now a list of groups, each its own page: saved as they change, so there's no Save
// button. Each setting lives in `settings`.
Page {
    id: page
    titleBar: TitleBar {
        title: qsTr("Settings")
    }
    attachedObjects: [
        ComponentDefinition {
            id: profileDef
            source: "ProfilePage.qml"
        },
        ComponentDefinition {
            id: appearanceDef
            source: "AppearanceSettingsPage.qml"
        },
        ComponentDefinition {
            id: chatSettingsDef
            source: "ChatSettingsPage.qml"
        },
        ComponentDefinition {
            id: notificationDef
            source: "NotificationSettingsPage.qml"
        },
        ComponentDefinition {
            id: dataDef
            source: "DataSettingsPage.qml"
        },
        ComponentDefinition {
            id: translationDef
            source: "TranslationSettingsPage.qml"
        }
    ]
    onCreationCompleted: {
        var items = []
        // Off while unlinked, as the menu's Profile entry is: there is no account to show.
        if (engine.linked) {
            items.push({ title: qsTr("Account"), description: qsTr("Name, about, picture, unlink"), target: "account" })
        }
        items.push({ title: qsTr("Appearance"), description: qsTr("Theme, text size, interface"), target: "appearance" })
        items.push({ title: qsTr("Chats and Calls"), description: qsTr("Enter key, proximity sensor, video calls"), target: "chats" })
        items.push({ title: qsTr("Notifications"), description: qsTr("Preview, start at boot"), target: "notifications" })
        items.push({ title: qsTr("Data and storage"), description: qsTr("Automatic downloads, mobile data, cache"), target: "data" })
        items.push({ title: qsTr("Translation"), description: qsTr("Service, API key"), target: "translation" })
        rows.append(items)
    }
    function openPage(target) {
        if (target == "account") {
            nav.push(profileDef.createObject())
        } else if (target == "appearance") {
            nav.push(appearanceDef.createObject())
        } else if (target == "chats") {
            nav.push(chatSettingsDef.createObject())
        } else if (target == "notifications") {
            nav.push(notificationDef.createObject())
        } else if (target == "data") {
            nav.push(dataDef.createObject())
        } else if (target == "translation") {
            nav.push(translationDef.createObject())
        }
    }
    Container {
        // The page colour the user picked (Settings > Appearance > Dark background).
        background: nav.pageBackground
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        ListView {
            layoutProperties: StackLayoutProperties {
                spaceQuota: 1
            }
            dataModel: ArrayDataModel {
                id: rows
            }
            listItemComponents: ListItemComponent {
                StandardListItem {
                    title: ListItemData.title
                    description: ListItemData.description
                }
            }
            onTriggered: page.openPage(dataModel.data(indexPath).target)
        }
    }
}
