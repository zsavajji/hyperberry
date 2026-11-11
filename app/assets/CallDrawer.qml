import bb.cascades 1.4
import hyperberry 1.0

// The people drawer of a group call (step 7e V3), in the look of the stock BB10 TabbedPane sidebar
// (measured on the Q10, 2026-11-11), and not a TabbedPane, whose rows would swap out the video
// windows. Like the stock one it reveals: the sidebar (CallSidebar) stays still at the left, and the
// call screen's page (CallSheet's callPage: the stage, the video, the controls, the header band, the
// People button, and over them the dim) slides right off it by the sidebar's width. This is the
// logic and what stays still above the page: the state, the glide, the taps and drags, the edge
// strip and the first-time hint. A row's tap (pick) puts that person full screen (into a tile first
// when they have none) and closes the drawer; in a voice call it just closes. It opens by a drag from
// the left edge (a strip of 30 px that takes the touch) or by the People button, and closes by a tap
// or a drag to the left on the dimmed page, a drag on the sidebar to the left, or the call's end. It
// does not depend on the video's controls (sheet.controlsShown): they may hide while it is open.
// The motion is CallSheet's: callPage's translationX (slide) and the dim's opacity (fade) are set
// from here, and the dim and callPage's black show while shown. The first 2 group calls show a hint
// for it (Hint.qml). Sizes are the Q10's px, scaled by sheet.px().
Container {
    id: drawer
    // The sidebar's width, and how far the page slides.
    property real reach: sheet.px(548)
    // Not while the call rings, nor once it has ended.
    property bool allowed: sheet.group && !sheet.ringing && !sheet.ended
    // The drawer is out, or going out; shown: any of it is on screen (also while it comes or goes).
    property bool open: false
    property bool shown: false
    // A finger is on the edge, the sidebar or the dimmed page; moved: it has gone past the slop, so it
    // is a drag. startX is where it went down (window px), startAt where the drawer was then, pos
    // where it is now: from -reach (shut) to 0 (out); the page's shift is pos + reach. A glide (the
    // 200 ms slide) ignores touches until it settles.
    property bool dragging: false
    property bool moved: false
    property bool animating: false
    property real startX: 0
    property real startAt: 0
    property real pos: -sheet.px(548)
    property real slop: sheet.px(10)
    // The People button (in the page) shows in a voice call, and in a video call with the controls.
    property bool buttonShown: allowed && (!sheet.isVideo || sheet.controlsShown)
    // The hint is for the first 2 group calls, once each, as soon as the call is active.
    property bool active: sheet.group && sheet.phase == "active"
    property string hintedFor: ""
    visible: allowed
    // It covers the whole call screen, the drawer shut too: without this it took every touch under it,
    // and the controls could not come back nor be pressed (0.0.1.263). Its own parts still take theirs.
    overlapTouchPolicy: OverlapTouchPolicy.Allow
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
    // Settles the glide if its animation never reports the end.
    attachedObjects: Timer {
        id: backstop
        interval: 400
        singleShot: true
        onTimeout: {
            if (drawer.animating) {
                drawer.settled()
            }
        }
    }
    // The page goes with pos (from 0 shut to reach out), the dim with the page (black at 70 % at most).
    // Set, not bound: the slide's transition runs over it, and a finger sets it as it moves.
    function place() {
        callPage.translationX = pos + reach
        dim.opacity = 1 + pos / reach
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
        place()
        hint.hide()
    }
    // A finger on the edge strip (from -reach, the drawer shut) or on the sidebar or the dimmed page
    // (from 0, out).
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
    // The page goes with the finger, the dim with the page.
    function follow(x) {
        if (!moved && Math.abs(x - startX) < slop) {
            return
        }
        moved = true
        if (!shown) {
            sidebar.sync(sheet.call.people)
            shown = true
        }
        hint.hide()
        pos = Math.max(-reach, Math.min(0, startAt + x - startX))
        place()
    }
    // Let go: out from the edge once pulled past a third of the width, in from the sidebar or the page
    // once pushed past a third. A touch that never moved (a tap, a scroll) does nothing.
    function release() {
        dragging = false
        if (!moved) {
            return
        }
        glide(startAt == 0 ? pos > -reach / 3 : pos > -reach * 2 / 3)
    }
    // Slides to out (on) or shut, from where the page is: 200 ms, as the stock sidebar's.
    function glide(on) {
        if (animating) {
            return
        }
        var from = pos
        animating = true
        open = on
        if (on) {
            sidebar.sync(sheet.call.people)
            shown = true
            hint.hide()
        }
        pos = on ? 0 : -reach
        slide.fromX = from + reach
        slide.toX = pos + reach
        fade.fromOpacity = 1 + from / reach
        fade.toOpacity = 1 + pos / reach
        slide.play()
        fade.play()
        backstop.start()
    }
    // The slide is over: the values are where it left them, and a shut drawer leaves the screen.
    function settled() {
        animating = false
        place()
        if (sheet.isVideo) {
            callVideo.logWindows(open ? "drawer open" : "drawer shut")
        }
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
                // The one already full screen goes back to the grid.
                videoView.focusTile = p.tile == videoView.solo ? -1 : p.tile
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
    // The first-time hint, near the top: our camera's viewfinder is a window above Cascades from
    // y 248 at the right, and a hint in the middle went under it (0.0.1.263). Its chevrons fall at the
    // left edge, clear of it. Under the edge strip, so that a swipe that starts on them still opens
    // the drawer.
    Hint {
        id: hint
        verticalAlignment: VerticalAlignment.Top
        topPadding: sheet.px(70)
        text: qsTr("Swipe from the left edge to see who is in the call")
        arrowRotation: 90
        arrowAtLeft: true
    }
    // The edge strip: transparent (no background: it lies over the video) and 30 px wide, so a drag
    // from the left edge opens the drawer. A finger gets no more touches once it leaves the node it
    // went down on, so the strip takes the whole width while a finger is down on it (onTouchExit
    // lets go, should the finger leave anyway). It lies over the left 30 px of the People button
    // (CallSheet's, 72 px, in the page), so a tap in that corner is passed on to it.
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
        gestureHandlers: TapHandler {
            onTapped: {
                if (drawer.buttonShown && event.y < sheet.px(72)) {
                    drawer.toggle()
                }
            }
        }
    }
}
