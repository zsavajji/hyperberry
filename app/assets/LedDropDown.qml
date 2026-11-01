import bb.cascades 1.4

// One kind of chat's LED colour (step 5.6): kind 0 chats, 1 groups, 2 channels. The LED shows
// only these 7 colours (bps/led.h); Off lights nothing.
DropDown {
    property int kind
    function colours() {
        return [0, 0xFF0000, 0x00FF00, 0x0000FF, 0x00FFFF, 0xFF00FF, 0xFFFF00, 0xFFFFFF]
    }
    onCreationCompleted: {
        var names = [qsTr("Off"), qsTr("Red"), qsTr("Green"), qsTr("Blue"), qsTr("Cyan"),
            qsTr("Magenta"), qsTr("Yellow"), qsTr("White")]
        for (var i = 0; i < names.length; i++) {
            var o = optionDef.createObject()
            o.text = names[i]
            add(o)
        }
        selectedIndex = Math.max(0, colours().indexOf(settings.leds[kind]))
    }
    onSelectedIndexChanged: {
        if (selectedIndex >= 0) {
            settings.setLed(kind, colours()[selectedIndex])
        }
    }
    attachedObjects: ComponentDefinition {
        id: optionDef
        Option {}
    }
}
