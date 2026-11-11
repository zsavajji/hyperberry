import bb.cascades 1.4
import hyperberry 1.0

// Who is in a group call (step 7e V3): our own slide-out from the left, in the look of the stock
// BB10 sidebar (measured on the Q10, 2026-11-11), and not a TabbedPane, whose rows would swap out
// the video windows. A row per person of the call event's people: picture, name, a camera mark
// while they send video, dimmed until they are connected; in a video call the row of the tile shown
// alone (videoView.solo) is lit, and a tap puts that person full screen (into a tile first when they
// have none) and closes the drawer; in a voice call a tap just closes it. It opens by a drag from the
// left edge (a strip of 30 px that takes the touch) or by the People button (CallSheet's top left,
// while the controls show), and closes by a tap on the dimmed side, a drag of the panel to the left,
// or the call's end. It does not depend on the video's controls (sheet.controlsShown): they may hide
// while it is open. Over the video, never an ancestor of its windows: what Cascades draws shows over
// them, and no ancestor of the video controls may have a background. The first 2 group calls show a
// hint for it (Hint.qml). Sizes are the Q10's px, scaled by sheet.px().
Container {
    id: drawer
    // The panel's width.
    property real reach: sheet.px(548)
    // Not while the call rings, nor once it has ended.
    property bool allowed: sheet.group && !sheet.ringing && !sheet.ended
    // The drawer is out, or going out; shown: any of it is on screen (also while it comes or goes).
    property bool open: false
    property bool shown: false
    // A finger is on the edge or the panel; moved: it has gone past the slop, so it is a drag. startX
    // is where it went down (window px), startAt where the panel was then, pos where it is now: from
    // -reach (in) to 0 (out). A glide (the 200 ms slide) ignores touches until it settles.
    property bool dragging: false
    property bool moved: false
    property bool animating: false
    property real startX: 0
    property real startAt: 0
    property real pos: -sheet.px(548)
    property real slop: sheet.px(10)
    // The hint is for the first 2 group calls, once each, as soon as the call is active.
    property bool active: sheet.group && sheet.phase == "active"
    property string hintedFor: ""
    visible: allowed
    horizontalAlignment: HorizontalAlignment.Fill
    verticalAlignment: VerticalAlignment.Fill
    layout: DockLayout {}
    onAllowedChanged: {
        if (!allowed) {
            reset()
        }
    }
    onActiveChanged: {
        var callId = sheet.call.id || ""
        if (active && hintedFor != callId) {
            hintedFor = callId
            if (settings.callHints < 2) {
                settings.callHints = settings.callHints + 1
                hint.show(5000)
            }
        }
    }
    attachedObjects: [
        // The rows, kept by person (sync), so they don't flicker or jump as people come and go.
        ArrayDataModel {
            id: rows
        },
        // Settles the glide if its animation never reports the end.
        Timer {
            id: backstop
            interval: 400
            singleShot: true
            onTimeout: {
                if (drawer.animating) {
                    drawer.settled()
                }
            }
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
    // Shut at once, with no slide: the call ended or has not begun.
    function reset() {
        slide.stop()
        fade.stop()
        backstop.stop()
        open = false
        shown = false
        dragging = false
        moved = false
        animating = false
        pos = -reach
        pane.translationX = pos
        dim.opacity = 0
        hint.hide()
    }
    // A finger on the edge strip (from -reach, the panel in) or on the open panel (from 0).
    function dragTouch(event, from) {
        if (event.isDown()) {
            if (!animating) {
                startX = event.windowX
                startAt = from
                moved = false
                dragging = true
            }
        } else if (dragging && event.isMove()) {
            follow(event.windowX)
        } else if (dragging && (event.isUp() || event.isCancel())) {
            release()
        }
    }
    // The panel goes with the finger, the dim with the panel.
    function follow(x) {
        if (!moved && Math.abs(x - startX) < slop) {
            return
        }
        moved = true
        if (!shown) {
            sync(sheet.call.people)
            shown = true
        }
        hint.hide()
        pos = Math.max(-reach, Math.min(0, startAt + x - startX))
        pane.translationX = pos
        dim.opacity = 1 + pos / reach
    }
    // Let go: out from the edge once pulled past a third of the width, in from the panel once pushed
    // past a third. A touch that never moved (a tap, a scroll) does nothing.
    function release() {
        dragging = false
        if (!moved) {
            return
        }
        glide(startAt == 0 ? pos > -reach / 3 : pos > -reach * 2 / 3)
    }
    // Slides to out (on) or in, from where the panel is: 200 ms, as the stock sidebar's.
    function glide(on) {
        if (animating) {
            return
        }
        var from = pos
        animating = true
        open = on
        if (on) {
            sync(sheet.call.people)
            shown = true
            hint.hide()
        }
        pos = on ? 0 : -reach
        slide.fromX = from
        slide.toX = pos
        fade.fromOpacity = 1 + from / reach
        fade.toOpacity = 1 + pos / reach
        slide.play()
        fade.play()
        backstop.start()
    }
    // The slide is over: the values are where it left them, and a closed drawer leaves the screen.
    function settled() {
        animating = false
        pane.translationX = pos
        dim.opacity = 1 + pos / reach
        if (!open) {
            shown = false
        }
    }
    function toggle() {
        if (animating) {
            return
        }
        glide(!open)
    }
    // A tap on a row. In a video call the person goes full screen: in their tile when they have one,
    // else, when they send video, in the tile shown alone, or the last one, with the others kept
    // (call.tiles takes the four jids). Then it closes.
    function pick(p) {
        if (sheet.isVideo && p) {
            if (p.tile >= 0) {
                videoView.focusTile = p.tile
            } else if (p.video) {
                var t = videoView.solo >= 0 ? videoView.solo : 3
                var jids = ["", "", "", ""]
                var people = sheet.call.people
                for (var i = 0; people && i < people.length; i++) {
                    if (people[i].tile >= 0 && people[i].tile < 4) {
                        jids[people[i].tile] = people[i].jid
                    }
                }
                jids[t] = p.jid
                engine.request("call.tiles", {"id": sheet.call.id, "jids": jids})
                videoView.focusTile = t
            }
        }
        glide(false)
    }
    // The dimmed side: black at 70 %, it takes the taps that close the drawer. Fades with the panel.
    Container {
        id: dim
        visible: drawer.shown
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        opacity: 0
        background: Color.create("#b3000000")
        attachedObjects: ImplicitAnimationController {
            propertyName: "opacity"
            enabled: false
        }
        animations: FadeTransition {
            id: fade
            duration: 200
        }
        gestureHandlers: TapHandler {
            onTapped: drawer.glide(false)
        }
    }
    // The panel and its shadow, which slide as one.
    Container {
        id: pane
        visible: drawer.shown
        horizontalAlignment: HorizontalAlignment.Left
        verticalAlignment: VerticalAlignment.Fill
        translationX: -sheet.px(548)
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        // The finger sets translationX itself while it drags; an implicit animation would trail it.
        attachedObjects: ImplicitAnimationController {
            propertyName: "translationX"
            enabled: false
        }
        animations: TranslateTransition {
            id: slide
            duration: 200
            easingCurve: StockCurve.CubicOut
            onEnded: drawer.settled()
        }
        Container {
            preferredWidth: drawer.reach
            verticalAlignment: VerticalAlignment.Fill
            background: Color.create("#1e1e1e")
            layout: DockLayout {}
            // A drag to the left closes: its touches arrive here from the rows too.
            onTouch: drawer.dragTouch(event, 0)
            onTouchExit: {
                if (drawer.dragging) {
                    drawer.release()
                }
            }
            ListView {
                id: list
                horizontalAlignment: HorizontalAlignment.Fill
                verticalAlignment: VerticalAlignment.Fill
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
        // The panel's shadow, 12 px along its right edge, stretched down its height.
        ImageView {
            verticalAlignment: VerticalAlignment.Fill
            preferredWidth: sheet.px(12)
            scalingMethod: ScalingMethod.Fill
            loadEffect: ImageViewLoadEffect.None
            imageSource: "asset:///images/call_drawer_shadow.png"
        }
    }
    // The first-time hint, over the middle of the screen. Under the edge strip, so that a swipe that
    // starts on its chevrons still opens the drawer.
    Hint {
        id: hint
        verticalAlignment: VerticalAlignment.Center
        text: qsTr("Swipe from the left edge to see who is in the call")
        arrowRotation: 90
        arrowAtLeft: true
    }
    // The edge strip: transparent (no background: it lies over the video) and 30 px wide, so a drag
    // from the left edge opens the drawer. A finger gets no more touches once it leaves the node it
    // went down on, so the strip takes the whole width while a finger is down on it (onTouchExit
    // lets go, should the finger leave anyway).
    Container {
        visible: !drawer.open
        implicitLayoutAnimationsEnabled: false
        horizontalAlignment: HorizontalAlignment.Left
        verticalAlignment: VerticalAlignment.Fill
        preferredWidth: drawer.dragging ? sheet.px(720) : sheet.px(30)
        onTouch: drawer.dragTouch(event, -drawer.reach)
        onTouchExit: {
            if (drawer.dragging) {
                drawer.release()
            }
        }
    }
    // The People button: a 72 px touch target at the top left (in a voice call over the header band,
    // in a video call with the controls), above the strip. The icon sits in the band's height.
    Container {
        visible: !drawer.shown && (!sheet.isVideo || sheet.controlsShown)
        horizontalAlignment: HorizontalAlignment.Left
        verticalAlignment: VerticalAlignment.Top
        preferredWidth: sheet.px(72)
        preferredHeight: sheet.px(72)
        topPadding: sheet.px(3)
        layout: DockLayout {}
        ImageView {
            horizontalAlignment: HorizontalAlignment.Center
            verticalAlignment: VerticalAlignment.Top
            preferredWidth: sheet.px(48)
            preferredHeight: sheet.px(48)
            scalingMethod: ScalingMethod.AspectFit
            loadEffect: ImageViewLoadEffect.None
            imageSource: "asset:///images/contacts_action.png"
            filterColor: Color.White
        }
        gestureHandlers: TapHandler {
            onTapped: drawer.toggle()
        }
    }
}
