import bb.cascades 1.4

// Phase 0's gate page (PLAN.md §9): start the engine as a headless service and read what it
// says. Phase 2 replaces it with the real chat list.
Page {
    titleBar: TitleBar {
        title: qsTr("HyperBerry")
    }

    actions: [
        ActionItem {
            title: qsTr("Start engine")
            ActionBar.placement: ActionBarPlacement.Signature
            onTriggered: gates.startEngine()
        },
        ActionItem {
            title: qsTr("Refresh")
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: gates.refresh()
        },
        ActionItem {
            title: qsTr("SQLite test")
            ActionBar.placement: ActionBarPlacement.OnBar
            onTriggered: gates.startSqlTest()
        }
    ]

    ScrollView {
        Container {
            topPadding: ui.du(2)
            leftPadding: ui.du(2)
            rightPadding: ui.du(2)

            Label {
                text: gates.status
                multiline: true
            }
            Label {
                topMargin: ui.du(2)
                text: gates.sql
                multiline: true
                visible: text != ""
            }
            Label {
                topMargin: ui.du(2)
                text: gates.log
                multiline: true
                textStyle.base: SystemDefaults.TextStyles.SmallText
                textStyle.fontFamily: "Courier"
            }
        }
    }
}
