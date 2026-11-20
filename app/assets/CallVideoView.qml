import bb.cascades 1.4
import hyperberry 1.0

// The video of a call (step 7c.3), over the call screen's stage and under its controls (CallPanel):
// the peer's picture fills the sheet, our own camera sits in a corner, and a dark band across the top
// carries the name and the clock while the controls show. A tap anywhere in it, the picture included,
// shows or hides the controls. A group call (step 7e) has up to four pictures, tiles 0 to 3, as they
// come in: one fills the sheet, two are stacked, three or four make a 2x2 grid; a tap on a tile shows
// it alone, full, and a tap again goes back to the grid (the others keep decoding), and each tile
// names its sender. The windows are CallVideo's (callVideo.start takes the ids below); they sit
// under Cascades' hole, so this view and every ancestor of the controls must have no background of
// their own, or the hole is painted over (0.0.1.190). Sizes in the Q10's px, scaled by sheet.px();
// the pictures' in the frame's own px.
Container {
    id: view
    // The four tiles' window ids, tile 0 first: callVideo.start's.
    property variant remoteIds: ["hyperberryCallVideo0", "hyperberryCallVideo1", "hyperberryCallVideo2", "hyperberryCallVideo3"]
    property alias finderId: finder.windowId
    // The view's own frame.
    property real w: 0
    property real h: 0
    // A group call's tile shown alone, -1 for them all.
    property int focusTile: -1
    // Bit i: tile i has a picture to show. In a 1:1 call that is tile 0 once the sheet shows the
    // picture. In a group call it is also held by someone (their tile in the call's people): a tile
    // that nobody holds any more keeps its last picture in its window, and is not shown.
    property int shownMask: sheet.group ? callVideo.tileMask & held(sheet.call.people) : (sheet.showPicture ? 1 : 0)
    // The tile shown alone: focusTile while it is still shown, else -1.
    property int solo: sheet.group && focusTile >= 0 && (shownMask & (1 << focusTile)) ? focusTile : -1
    // Bit i: tile i is on screen now.
    property int visibleMask: solo >= 0 ? 1 << solo : shownMask
    // Where each tile sits, [x, y, w, h]; and the same for a full grid, whose 4th cell is black
    // when there are three pictures.
    property variant cells: place(visibleMask, w, h)
    property variant gridCells: place(15, w, h)
    // When a tile last took a tap. One tap reaches both the tile's handler and this view's, in an
    // order that isn't known: a tile acts at once, the view waits a moment and lets a tile's tap win
    // (0.0.1.266: with the view's first, a full-screen tile never went back to the grid).
    property real lastTileTap: 0
    horizontalAlignment: HorizontalAlignment.Fill
    verticalAlignment: VerticalAlignment.Fill
    // Off while it rings, so the ringing controls under it get their touches; else on even for a
    // voice call, so the windows are bound when video starts.
    visible: !sheet.ringing
    layout: DockLayout {}
    attachedObjects: [
        Timer {
            id: viewTap
            singleShot: true
            interval: 60
            onTimeout: {
                if (new Date().getTime() - view.lastTileTap > 250) {
                    sheet.toggleControls()
                }
            }
        },
        LayoutUpdateHandler {
            onLayoutFrameChanged: {
                view.w = layoutFrame.width
                view.h = layoutFrame.height
            }
        }
    ]
    // What is not a tile: the controls toggle, unless a tile took this tap.
    gestureHandlers: TapHandler {
        onTapped: viewTap.start()
    }
    function bits(mask) {
        var n = 0
        for (var i = 0; i < 4; i++) {
            if (mask & (1 << i)) {
                n++
            }
        }
        return n
    }
    // Which tiles the call's people hold (their tile, -1 for none).
    function held(people) {
        var mask = 0
        for (var i = 0; people && i < people.length; i++) {
            var t = people[i].tile
            if (t >= 0 && t < 4) {
                mask |= 1 << t
            }
        }
        return mask
    }
    function nameOf(people, tile) {
        for (var i = 0; people && i < people.length; i++) {
            if (people[i].tile == tile) {
                return people[i].name
            }
        }
        return ""
    }
    // The tiles in mask, in tile order, in a w x h frame: one fills it, two are stacked, more make
    // a 2x2 grid. Halves are rounded down, the second half takes the rest. A tile not in mask gets
    // the whole frame (it is hidden).
    function place(mask, w, h) {
        var n = bits(mask)
        var hw = Math.floor(w / 2)
        var hh = Math.floor(h / 2)
        var out = []
        var rank = 0
        for (var i = 0; i < 4; i++) {
            var c = [0, 0, w, h]
            if (mask & (1 << i)) {
                var col = rank % 2
                var row = (rank - col) / 2
                if (n == 2) {
                    c = [0, rank == 0 ? 0 : hh, w, rank == 0 ? hh : h - hh]
                } else if (n > 2) {
                    c = [col == 0 ? 0 : hw, row == 0 ? 0 : hh, col == 0 ? hw : w - hw, row == 0 ? hh : h - hh]
                }
                rank++
            }
            out.push(c)
        }
        return out
    }
    // A tap on a tile. In a group call it shows alone, or all again; with only one tile on screen
    // there is nothing to show, and it toggles the controls as in a 1:1 call.
    function tap(tile) {
        lastTileTap = new Date().getTime()
        if (sheet.group && (solo >= 0 || bits(shownMask) > 1)) {
            focusTile = solo >= 0 ? -1 : tile
            sheet.wake()
        } else {
            sheet.toggleControls()
        }
    }
    // The pictures fill their cells: each control is its cell's size and CallVideo crops the picture
    // to its aspect (Screen's source viewport). A control bigger than its cell was squeezed into it
    // by Cascades, not cropped (0.0.1.203). ponytail: fill crops; fit would letterbox.
    Container {
        horizontalAlignment: HorizontalAlignment.Fill
        verticalAlignment: VerticalAlignment.Fill
        layout: AbsoluteLayout {}
        CallVideoTile {
            index: 0
            windowId: view.remoteIds[0]
            cell: view.cells[0]
            visible: (view.visibleMask & 1) != 0
            name: view.nameOf(sheet.call.people, 0)
            onTapped: view.tap(0)
        }
        CallVideoTile {
            index: 1
            windowId: view.remoteIds[1]
            cell: view.cells[1]
            visible: (view.visibleMask & 2) != 0
            name: view.nameOf(sheet.call.people, 1)
            onTapped: view.tap(1)
        }
        CallVideoTile {
            index: 2
            windowId: view.remoteIds[2]
            cell: view.cells[2]
            visible: (view.visibleMask & 4) != 0
            name: view.nameOf(sheet.call.people, 2)
            onTapped: view.tap(2)
        }
        CallVideoTile {
            index: 3
            windowId: view.remoteIds[3]
            cell: view.cells[3]
            visible: (view.visibleMask & 8) != 0
            name: view.nameOf(sheet.call.people, 3)
            onTapped: view.tap(3)
        }
        // The empty 4th cell of three pictures, black: nothing else is drawn there. It is no tile,
        // so it has no tap of its own and the view's toggles the controls.
        Container {
            visible: view.bits(view.visibleMask) == 3
            layoutProperties: AbsoluteLayoutProperties {
                positionX: view.gridCells[3][0]
                positionY: view.gridCells[3][1]
            }
            preferredWidth: view.gridCells[3][2]
            preferredHeight: view.gridCells[3][3]
            background: Color.Black
        }
    }
    // The name and the clock, as the stock app's video call: white on a dark band across the top.
    Container {
        visible: sheet.isVideo && sheet.controlsShown
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Top
        preferredWidth: sheet.px(720)
        preferredHeight: sheet.px(260)
        layout: AbsoluteLayout {}
        // The fade is dark at its foot: turned over, at the top.
        ImageView {
            rotationZ: 180
            preferredWidth: sheet.px(720)
            preferredHeight: sheet.px(260)
            scalingMethod: ScalingMethod.AspectFit
            loadEffect: ImageViewLoadEffect.None
            imageSource: "asset:///images/call_fade.png"
        }
        // The name's centre at y 135.
        Container {
            layoutProperties: AbsoluteLayoutProperties {
                positionY: sheet.px(95)
            }
            preferredWidth: sheet.px(720)
            preferredHeight: sheet.px(80)
            layout: DockLayout {}
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                preferredWidth: sheet.px(640)
                multiline: false
                text: sheet.who.name || sheet.number
                textFormat: TextFormat.Plain
                textStyle.base: nav.emojiStyle
                textStyle.fontSize: FontSize.PercentageValue
                textStyle.fontSizeValue: 150 * zoom
                textStyle.textAlign: TextAlign.Center
                textStyle.color: Color.White
            }
        }
        // The clock, else the call's state, with the peer's request beside the clock; centre at y 205.
        Container {
            layoutProperties: AbsoluteLayoutProperties {
                positionY: sheet.px(175)
            }
            preferredWidth: sheet.px(720)
            preferredHeight: sheet.px(60)
            layout: DockLayout {}
            Label {
                horizontalAlignment: HorizontalAlignment.Center
                verticalAlignment: VerticalAlignment.Center
                preferredWidth: sheet.px(640)
                multiline: false
                text: sheet.call.videoAsked ? (sheet.timed ? sheet.clock + " · " : "") + qsTr("Wants to turn on video") : (sheet.timed ? sheet.clock : sheet.line)
                textFormat: TextFormat.Plain
                textStyle.textAlign: TextAlign.Center
                textStyle.color: Color.White
            }
        }
    }
    // Our camera, small and in the lower right; the camera library makes its window. It sits just
    // above the grid's panel (y 415 on the stage) while the controls show, and drops to the foot
    // when they hide. The stage is the Q10's 720 px, kept to the bottom as the sheet's.
    Container {
        horizontalAlignment: HorizontalAlignment.Center
        verticalAlignment: VerticalAlignment.Bottom
        preferredWidth: sheet.px(720)
        preferredHeight: sheet.px(720)
        layout: AbsoluteLayout {}
        ForeignWindowControl {
            id: finder
            // Our camera's shape, in the stage's px: 352:640 when the crop is on (Settings' camera aspect
            // ratio: the camera's own portrait size, its window is already 9:16, so no viewport is
            // needed), 16:9 while the camera runs 1280x720, else 4:3. The window takes the control's
            // size, so a wrong shape squashes us. It keeps its right edge at 702 and its bottom at 398
            // (just above the grid's panel while the controls show) or 702 (the foot, when they hide).
            property real shownWidth: sheet.px(callVideo.cropped ? 88 : 200)
            property real shownHeight: sheet.px(callVideo.cropped ? 160 : (callVideo.wide ? 113 : 150))
            windowId: "hyperberryCallFinder"
            updatedProperties: WindowProperty.Size | WindowProperty.Position | WindowProperty.Visible
            // Shown while the people drawer is out, as the tiles' (CallVideoTile).
            // Hidden at the ear: the window is above Cascades, so the sheet's black can't cover it.
            visible: sheet.isVideo && !!sheet.call.videoOurs && !sheet.atEar && !sheet.previewHidden
            layoutProperties: AbsoluteLayoutProperties {
                positionX: sheet.px(702) - finder.shownWidth
                positionY: (sheet.controlsShown ? sheet.px(398) : sheet.px(702)) - finder.shownHeight
            }
            preferredWidth: finder.shownWidth
            preferredHeight: finder.shownHeight
        }
    }
}
