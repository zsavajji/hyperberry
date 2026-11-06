import bb.cascades 1.4

// A text's link preview (step 6.5f), under the text and drawn as the quote above it is: a
// primary-colour bar at the left, then the picture (when there is one), the title and the
// description. A tap opens the link in the browser. msg is a "msg" row of MessageModel
// (linkTitle, linkDesc, linkUrl, linkThumb, hasLinkThumb); view is the chat page's list, for the
// text style and size.
Container {
    id: preview
    property variant msg
    property variant view
    // A link typed without its scheme ("www.…") gets one, or the browser has nothing to open.
    property string link: msg.linkUrl == "" ? "" : /^[a-z][a-z0-9+.\-]*:\/\//i.test(msg.linkUrl) ? msg.linkUrl : "http://" + msg.linkUrl
    topMargin: ui.du(0.5)
    bottomMargin: 0
    horizontalAlignment: HorizontalAlignment.Fill
    layout: StackLayout {
        orientation: LayoutOrientation.LeftToRight
    }
    gestureHandlers: TapHandler {
        onTapped: {
            if (preview.link != "" && !preview.view.joiner.openLink(preview.link)) {
                browser.trigger("bb.action.OPEN")
            }
        }
    }
    // The query is sent to the system again whenever a property changes (updateQuery), so a
    // recycled card's new link is the one a tap opens. The invocation is armed a moment after.
    attachedObjects: [
        Invocation {
            id: browser
            query {
                invokeTargetId: "sys.browser"
                invokeActionId: "bb.action.OPEN"
                uri: preview.link
                onQueryChanged: browser.query.updateQuery()
            }
        }
    ]

    Container {
        verticalAlignment: VerticalAlignment.Fill
        preferredWidth: ui.du(0.5)
        background: ui.palette.primary
    }
    // The picture is square and grows with the text size, as the lines beside it do.
    ImageView {
        visible: preview.msg.hasLinkThumb
        verticalAlignment: VerticalAlignment.Top
        leftMargin: ui.du(1)
        preferredWidth: ui.du(10 * preview.view.textScale)
        preferredHeight: ui.du(10 * preview.view.textScale)
        image: preview.msg.linkThumb
        scalingMethod: ScalingMethod.AspectFill
        loadEffect: ImageViewLoadEffect.None
    }
    Container {
        leftMargin: ui.du(1)
        layoutProperties: StackLayoutProperties {
            spaceQuota: 1
        }
        Label {
            bottomMargin: 0
            multiline: true
            autoSize.maxLineCount: 2
            text: preview.msg.linkTitle
            textFormat: TextFormat.Plain
            textStyle.base: preview.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: preview.view.smallSize
            textStyle.fontWeight: FontWeight.Bold
        }
        Label {
            visible: preview.msg.linkDesc != ""
            topMargin: 0
            bottomMargin: 0
            multiline: true
            autoSize.maxLineCount: 2
            text: preview.msg.linkDesc
            textFormat: TextFormat.Plain
            textStyle.base: preview.view.emojiStyle
            textStyle.fontSize: FontSize.PercentageValue
            textStyle.fontSizeValue: preview.view.smallSize
            textStyle.color: ui.palette.secondaryTextOnPlain
        }
    }
}
