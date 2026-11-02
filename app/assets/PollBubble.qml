import bb.cascades 1.4

// The options of a poll in a card (step 6.5e), under its question: per option a mark (● ○ for one
// choice, ☑ ☐ for several), the name, the count, and a bar of the share of the voters who chose it.
// A tap on an option votes (view.vote), a tap on its count says who chose it (view.showVoters). msg is
// a "msg" row of MessageModel (max, mine, voted, options: see Polls.h); view is the chat page's list.
// Cascades has no Repeater: the rows are made from rowDef whenever msg changes, since list items are
// recycled, and removeAll() deletes the old rows (container.h: it "frees up their memory").
Container {
    id: bubble
    property variant msg
    property variant view
    // Set once the first rows are made, so the first msg doesn't build them twice.
    property bool built: false
    topMargin: 0
    bottomMargin: 0
    onMsgChanged: {
        if (built) {
            rebuild()
        }
    }
    onCreationCompleted: {
        rebuild()
        built = true
    }

    function rebuild() {
        bubble.removeAll()
        var opts = msg.options
        for (var i = 0; i < opts.length; i++) {
            var r = rowDef.createObject()
            r.index = i
            r.name = opts[i].name
            r.count = opts[i].count
            r.total = msg.voted
            r.several = msg.max != 1
            r.picked = chosen(i)
            r.chose.connect(bubble.choose)
            r.who.connect(bubble.whoChose)
            bubble.add(r)
        }
    }
    // Whether our vote has option i.
    function chosen(i) {
        var mine = msg.mine
        for (var k = 0; k < mine.length; k++) {
            if (mine[k] == i) {
                return true
            }
        }
        return false
    }
    // The whole new vote: one choice moves or goes, several toggle i and stop at max (0: no limit).
    // ponytail: from the vote last drawn, so two taps before the engine answers act on the same
    // state; an optimistic copy of mine if that shows.
    function choose(i) {
        var mine = msg.mine
        var on = chosen(i)
        var next = []
        if (msg.max == 1) {
            next = on ? [] : [i]
        } else {
            for (var k = 0; k < mine.length; k++) {
                if (mine[k] != i) {
                    next.push(mine[k])
                }
            }
            if (!on) {
                if (msg.max > 0 && mine.length >= msg.max) {
                    return
                }
                next.push(i)
            }
        }
        view.vote(msg.id, next)
    }
    function whoChose(i) {
        var voters = msg.options[i].voters
        view.showVoters(voters != "" ? voters : qsTr("Nobody yet"))
    }
    attachedObjects: [
        ComponentDefinition {
            id: rowDef
            Container {
                id: row
                property int index
                property string name
                property int count
                // How many voted, for the bar.
                property int total
                property bool several
                property bool picked
                signal chose(int at)
                signal who(int at)
                topPadding: ui.du(1 * bubble.view.textScale)
                Container {
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    // The mark and the name vote; the count beside them, not inside, has its own tap:
                    // nested, a tap on the count could reach both handlers.
                    Container {
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: 1
                        }
                        layout: StackLayout {
                            orientation: LayoutOrientation.LeftToRight
                        }
                        gestureHandlers: TapHandler {
                            onTapped: row.chose(row.index)
                        }
                        // From the phone's fallback font (DejaVu Sans), as ✕ ▲ ▼ are, so not the emoji style:
                        // its font has ☑ as a colour emoji.
                        Label {
                            verticalAlignment: VerticalAlignment.Top
                            topMargin: 0
                            bottomMargin: 0
                            rightMargin: ui.du(1)
                            text: row.several ? (row.picked ? "☑" : "☐") : (row.picked ? "●" : "○")
                            textFormat: TextFormat.Plain
                            textStyle.base: SystemDefaults.TextStyles.BodyText
                            textStyle.fontSize: FontSize.PercentageValue
                            textStyle.fontSizeValue: Math.round(100 * bubble.view.textScale)
                            textStyle.color: row.picked ? ui.palette.primary : ui.palette.secondaryTextOnPlain
                        }
                        // The name in a container with the quota, as the card's text is.
                        Container {
                            verticalAlignment: VerticalAlignment.Top
                            layoutProperties: StackLayoutProperties {
                                spaceQuota: 1
                            }
                            Label {
                                topMargin: 0
                                bottomMargin: 0
                                multiline: true
                                // Without U+20E3, as the card's text.
                                text: row.name.replace(/⃣/g, "")
                                textFormat: TextFormat.Plain
                                textStyle.base: bubble.view.emojiStyle
                                textStyle.fontSize: FontSize.PercentageValue
                                textStyle.fontSizeValue: bubble.view.bodySize
                                textStyle.color: ui.palette.textOnPlain
                            }
                        }
                    }
                    // Its own tap, so it doesn't vote; the width is a thumb's, the number at the right.
                    Label {
                        verticalAlignment: VerticalAlignment.Top
                        topMargin: 0
                        bottomMargin: 0
                        leftMargin: ui.du(1)
                        minWidth: ui.du(5)
                        text: row.count
                        textFormat: TextFormat.Plain
                        textStyle.base: bubble.view.emojiStyle
                        textStyle.fontSize: FontSize.PercentageValue
                        textStyle.fontSizeValue: bubble.view.smallSize
                        textStyle.textAlign: TextAlign.Right
                        textStyle.color: ui.palette.secondaryTextOnPlain
                        gestureHandlers: TapHandler {
                            onTapped: row.who(row.index)
                        }
                    }
                }
                // The share of the voters who chose it: the primary colour for the count, grey for the
                // rest, all grey while nobody voted.
                Container {
                    topMargin: ui.du(0.5)
                    bottomMargin: 0
                    horizontalAlignment: HorizontalAlignment.Fill
                    preferredHeight: ui.du(0.5)
                    layout: StackLayout {
                        orientation: LayoutOrientation.LeftToRight
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Fill
                        background: ui.palette.primary
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: row.count
                        }
                    }
                    Container {
                        verticalAlignment: VerticalAlignment.Fill
                        background: ui.palette.secondaryTextOnPlain
                        opacity: 0.3
                        layoutProperties: StackLayoutProperties {
                            spaceQuota: row.total > 0 ? row.total - row.count : 1
                        }
                    }
                }
            }
        }
    ]
}
