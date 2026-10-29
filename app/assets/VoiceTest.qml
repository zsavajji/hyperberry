import bb.cascades 1.4
import bb.multimedia 1.4

// Step 0.8 (PLAN.md §9): the voice-note audio path. AudioRecorder writes data/voice/test.wav
// in whatever format the phone records (it offers no rate or format of its own); the engine
// encodes it to OGG Opus and decodes that back to decoded.wav (engine/gates.go), and
// MediaPlayer plays either file. The engine's log shows the recorder's format and the times.
Sheet {
    id: sheet

    property string note

    function play(name) {
        player.stop();
        player.sourceUrl = gates.voiceUrl(name);
        var error = player.play();
        note = error == MediaError.None ? qsTr("Playing %1").arg(name)
                                        : qsTr("Can't play %1: error %2").arg(name).arg(error);
    }

    Page {
        titleBar: TitleBar {
            title: qsTr("Voice test")
            dismissAction: ActionItem {
                title: qsTr("Close")
                onTriggered: {
                    recorder.reset();
                    player.stop();
                    sheet.close();
                }
            }
        }

        actions: [
            ActionItem {
                title: recorder.mediaState == MediaState.Started ? qsTr("Stop") : qsTr("Record")
                ActionBar.placement: ActionBarPlacement.Signature
                onTriggered: {
                    if (recorder.mediaState == MediaState.Started) {
                        recorder.reset(); // finishes the file
                        sheet.note = qsTr("Recorded. Encode next.");
                    } else {
                        player.stop();
                        var error = recorder.record();
                        sheet.note = error == MediaError.None ? qsTr("Recording…")
                                                              : qsTr("Can't record: error %1").arg(error);
                    }
                }
            },
            ActionItem {
                title: qsTr("Encode")
                ActionBar.placement: ActionBarPlacement.OnBar
                onTriggered: {
                    gates.invoke("com.zsavajji.hyperberry.VOICETEST");
                    sheet.note = qsTr("Encoding: tap Refresh in a few seconds.");
                }
            },
            ActionItem {
                title: qsTr("Play decoded")
                ActionBar.placement: ActionBarPlacement.OnBar
                onTriggered: sheet.play("decoded.wav")
            },
            ActionItem {
                title: qsTr("Play recording")
                onTriggered: sheet.play("test.wav")
            },
            ActionItem {
                title: qsTr("Refresh")
                onTriggered: gates.refresh()
            }
        ]

        ScrollView {
            Container {
                topPadding: ui.du(2)
                leftPadding: ui.du(2)
                rightPadding: ui.du(2)

                Label {
                    text: sheet.note
                    multiline: true
                }
                Label {
                    text: qsTr("Recorded %1 s").arg((recorder.duration / 1000).toFixed(1))
                    visible: recorder.duration > 0
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

    attachedObjects: [
        AudioRecorder {
            id: recorder
            outputUrl: gates.voiceUrl("test.wav")
        },
        MediaPlayer {
            id: player
        }
    ]
}
