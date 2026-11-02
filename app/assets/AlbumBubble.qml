import bb.cascades 1.4

// An album in a card (step 6.5g): 4 or more photos and videos sent together (MessageModel makes the
// entry; Albums.h has the rule) as four square tiles, two by two, as wide as a single photo
// (MediaBubble's 40 du). With more than four, the fourth tile is darkened with "+N", N the album's
// size less 3, and opens the album's page (the list's showAlbum); the others open their member as
// its own row would (the list's openMedia). The four tiles are fixed and each takes its picture
// from the current msg, so a recycled row never keeps the previous row's. msg is the album's map:
// the first member's plus album (the members' maps, oldest first) and albumCount.
// ponytail: the long-press menu, the reactions and the search tint are the first photo's (msg is
// its map), so Delete and Reply act on that one only; per-member menus if that is missed.
Container {
    id: bubble
    property variant msg
    property variant view
    property real gap: ui.du(0.5)
    property real size: (ui.du(40) - gap) / 2
    property bool more: msg.albumCount > 4
    // As MediaBubble's picture: no margin above, air above and below.
    topMargin: 0
    topPadding: ui.du(1)
    bottomPadding: ui.du(1)
    // Never undefined: a row that stops being an album meanwhile falls back on its own map.
    function memberAt(i) {
        return msg.album[i] || msg
    }
    function open(i) {
        view.openMedia(msg.album[i])
    }
    // The fourth tile: the page of all of them, or just its member when there are four.
    function openFourth() {
        if (more) {
            view.showAlbum(msg.album)
        } else {
            open(3)
        }
    }
    Container {
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        AlbumTile {
            preferredWidth: bubble.size
            preferredHeight: bubble.size
            member: bubble.memberAt(0)
            loadingId: bubble.view.mediaLoadingId
            onTapped: bubble.open(0)
        }
        AlbumTile {
            leftMargin: bubble.gap
            preferredWidth: bubble.size
            preferredHeight: bubble.size
            member: bubble.memberAt(1)
            loadingId: bubble.view.mediaLoadingId
            onTapped: bubble.open(1)
        }
    }
    Container {
        topMargin: bubble.gap
        layout: StackLayout {
            orientation: LayoutOrientation.LeftToRight
        }
        AlbumTile {
            preferredWidth: bubble.size
            preferredHeight: bubble.size
            member: bubble.memberAt(2)
            loadingId: bubble.view.mediaLoadingId
            onTapped: bubble.open(2)
        }
        AlbumTile {
            leftMargin: bubble.gap
            preferredWidth: bubble.size
            preferredHeight: bubble.size
            member: bubble.memberAt(3)
            more: bubble.more ? "+" + (bubble.msg.albumCount - 3) : ""
            loadingId: bubble.view.mediaLoadingId
            onTapped: bubble.openFourth()
        }
    }
}
