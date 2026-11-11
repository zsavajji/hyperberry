import bb.cascades 1.4
import hyperberry 1.0

// The people drawer's sidebar (step 7e V3), in the look of the stock BB10 TabbedPane sidebar (measured
// on the Q10, 2026-11-11): #1e1e1e, 548 px wide, a row of 99 px per person (picture, name, a camera
// mark while they send video, dimmed until they are connected) with a 2 px #343434 line at its foot,
// the list centred vertically (it fills the height and scrolls only when the rows are more than fit).
// It never moves: it sits at the left, under the call screen's page (CallSheet's callPage), which
// slides right off it, and shows only while the drawer does (drawer.shown). Only then: with video the
// page is transparent over the video's windows, and an opaque sidebar under it would hide them while
// the drawer is shut. In a video call the row of the tile shown alone (videoView.solo) is lit. The
// rows are kept by person (sync), so they don't flicker or jump as people come and go. CallDrawer
// has the logic: a tap on a row is drawer.pick, a drag on the sidebar to the left drawer.dragTouch.
// Sizes are the Q10's px, scaled by sheet.px().
Container {
    id: side
    // The sidebar's height, from its layout (the list is capped to it).
    property real h: 0
    // How many rows there are (rows.size() is no binding).
    property int count: 0
    visible: drawer.shown
    horizontalAlignment: HorizontalAlignment.Left
    verticalAlignment: VerticalAlignment.Fill
    preferredWidth: drawer.reach
    background: Color.create("#1e1e1e")
    layout: DockLayout {}
    // A drag to the left closes: its touches arrive here from the rows too.
    onTouch: drawer.dragTouch(event, 0)
    onTouchExit: {
        if (drawer.dragging) {
            drawer.release()
        }
    }
    attachedObjects: [
        LayoutUpdateHandler {
            onLayoutFrameChanged: side.h = layoutFrame.height
        },
        ArrayDataModel {
            id: rows
        }
    ]
    // Fills rows with people: the rows are walked in step with people, a person's row moved to its
    // place (removeAt + insert), replaced when it changed, inserted when missing, and the rows
    // left over past the end removed. Each person's row is looked for from this index on only: the
    // ones before are the earlier people's.
    function sync(people) {
        var n = people ? people.length : 0
        for (var i = 0; i < n; i++) {
            var p = people[i]
            var j = rowOf(p.jid, i)
            if (j < 0) {
                rows.insert(i, p)
            } else {
                if (j != i) {
                    var moving = rows.value(j)
                    rows.removeAt(j)
                    rows.insert(i, moving)
                }
                if (!same(rows.value(i), p)) {
                    rows.replace(i, p)
                }
            }
        }
        while (rows.size() > n) {
            rows.removeAt(rows.size() - 1)
        }
        count = rows.size()
    }
    function rowOf(jid, from) {
        for (var j = from; j < rows.size(); j++) {
            if (rows.value(j).jid == jid) {
                return j
            }
        }
        return -1
    }
    function same(a, b) {
        return a.name == b.name && a.photo == b.photo && a.connected == b.connected && a.video == b.video && a.tile == b.tile
    }
    ListView {
        id: list
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Center
        // As tall as its rows, so that they sit in the middle, and no taller than the sidebar (then
        // it fills it and scrolls); the rows' own height until the sidebar has reported its.
        preferredHeight: side.h > 0 ? Math.min(side.count * list.rowHeight, side.h) : side.count * list.rowHeight
        dataModel: rows
        // What a row reads (a delegate reaches the app's values only through its view).
        property variant emojiStyle: nav.emojiStyle
        property int solo: videoView.solo
        property real rowHeight: sheet.px(99)
        property real pic: sheet.px(72)
        property real edge: sheet.px(18)
        property real mark: sheet.px(40)
        property real markEdge: sheet.px(24)
        property real line: sheet.px(2)
        onTriggered: drawer.pick(rows.data(indexPath))
        listItemComponents: [
            ListItemComponent {
                Container {
                    id: row
                    // The person whose tile is the one shown alone.
                    property bool selected: ListItemData.tile >= 0 && ListItemData.tile == row.ListItem.view.solo
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: row.ListItem.view.rowHeight
                    opacity: ListItemData.connected ? 1.0 : 0.5
                    background: row.selected ? Color.create("#0092cc") : Color.Transparent
                    layout: DockLayout {}
                    Container {
                        horizontalAlignment: HorizontalAlignment.Fill
                        verticalAlignment: VerticalAlignment.Center
                        leftPadding: row.ListItem.view.edge
                        rightPadding: row.ListItem.view.markEdge
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        // Square, as our pictures are; the initial on grey while there is none.
                        Container {
                            verticalAlignment: VerticalAlignment.Center
                            preferredWidth: row.ListItem.view.pic
                            preferredHeight: row.ListItem.view.pic
                            background: Color.create("#555555")
                            layout: DockLayout {}
                            Label {
                                visible: !ListItemData.photo
                                horizontalAlignment: HorizontalAlignment.Center
                                verticalAlignment: VerticalAlignment.Center
                                text: (ListItemData.name || "?").charAt(0).toUpperCase()
                                textFormat: TextFormat.Plain
                                textStyle.base: row.ListItem.view.emojiStyle
                                textStyle.color: Color.White
                            }
                            ImageView {
                                visible: !!ListItemData.photo
                                horizontalAlignment: HorizontalAlignment.Fill
                                verticalAlignment: VerticalAlignment.Fill
                                imageSource: ListItemData.photo ? "file://" + ListItemData.photo : ""
                                scalingMethod: ScalingMethod.AspectFill
                                loadEffect: ImageViewLoadEffect.None
                            }
                        }
                        Label {
                            verticalAlignment: VerticalAlignment.Center
                            leftMargin: row.ListItem.view.edge
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            multiline: false
                            text: ListItemData.name || "…"
                            textFormat: TextFormat.Plain
                            textStyle.base: row.ListItem.view.emojiStyle
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: 120
                            textStyle.color: Color.White
                        }
                        ImageView {
                            visible: !!ListItemData.video
                            verticalAlignment: VerticalAlignment.Center
                            leftMargin: row.ListItem.view.edge
                            preferredWidth: row.ListItem.view.mark
                            preferredHeight: row.ListItem.view.mark
                            scalingMethod: ScalingMethod.AspectFit
                            loadEffect: ImageViewLoadEffect.None
                            imageSource: "asset:///images/call_video.png"
                            filterColor: Color.create("#aaaaaa")
                        }
                    }
                    // The separator at the row's foot, and the lit row's line at its top.
                    Container {
                        horizontalAlignment: HorizontalAlignment.Fill
                        verticalAlignment: VerticalAlignment.Bottom
                        preferredHeight: row.ListItem.view.line
                        background: Color.create("#343434")
                    }
                    Container {
                        visible: row.selected
                        horizontalAlignment: HorizontalAlignment.Fill
                        verticalAlignment: VerticalAlignment.Top
                        preferredHeight: row.ListItem.view.line
                        background: Color.create("#199dd1")
                    }
                }
            }
        ]
    }
}
