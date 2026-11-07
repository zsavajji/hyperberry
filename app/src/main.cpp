#include <bb/cascades/AbstractPane>
#include <bb/cascades/Application>
#include <bb/cascades/QmlDocument>
#include <bb/platform/NotificationDefaultApplicationSettings>
#include <bb/system/LocaleHandler>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QTextCodec>
#include <QTimer>
#include <QTranslator>
#include <QtDeclarative>
#include <cstdio>
#include <sys/slog2.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include "ChatController.h"
#include "CallAudio.h"
#include "CallVideo.h"
#include "CallsModel.h"
#include "ChatListModel.h"
#include "ContactBook.h"
#include "ContactInfo.h"
#include "ContactPicker.h"
#include "EmojiModel.h"
#include "EngineLink.h"
#include "Invites.h"
#include "MapChooser.h"
#include "MediaOpener.h"
#include "OpenRequests.h"
#include "Profile.h"
#include "Proximity.h"
#include "QrImage.h"
#include "Settings.h"
#include "VideoTest.h"
#include "CameraTest.h"

using namespace bb::cascades;

// qDebug and qWarning, QML's errors included, go nowhere on the phone by themselves: they go to
// the system log, where `slog2info -b hyperberry` shows them, and to data/logs/ui.log. The file
// outlives the process: the system recycles a closed app's log within minutes, and a freeze on
// 0.0.1.132 left nothing to read (Q10, 2026-11-02).
static slog2_buffer_t logBuffer;
static bool toSlog = false;
static FILE *logFile = 0;

static void writeLog(QtMsgType type, const char *msg)
{
    if (toSlog)
        slog2c(logBuffer, 0, type == QtDebugMsg ? SLOG2_INFO : SLOG2_WARNING, msg);
    if (!logFile)
        return;
    timeval now;
    gettimeofday(&now, 0);
    tm local;
    localtime_r(&now.tv_sec, &local);
    char stamp[32];
    strftime(stamp, sizeof stamp, "%b %d %H:%M:%S", &local); // slog2info's form, phone time
    fprintf(logFile, "%s.%03d %s\n", stamp, int(now.tv_usec / 1000), msg);
    fflush(logFile); // with the OS at once: a freeze or a kill keeps every line before it
}

static void startLog()
{
    slog2_buffer_set_config_t config = {};
    config.num_buffers = 1;
    config.buffer_set_name = "hyperberry";
    config.verbosity_level = SLOG2_INFO;
    config.buffer_config[0].buffer_name = "ui";
    config.buffer_config[0].num_pages = 4;
    toSlog = slog2_register(&config, &logBuffer, 0) == 0;
    const QString path = QDir::homePath() + "/logs/ui.log";
    QDir().mkpath(QFileInfo(path).path());
    // ponytail: rotated at start only, so one long run can pass 256 KB; the UI writes little.
    if (QFileInfo(path).size() > 256 * 1024) {
        QFile::remove(path + ".1");
        QFile::rename(path, path + ".1");
    }
    logFile = fopen(QFile::encodeName(path).constData(), "a");
    qInstallMsgHandler(writeLog);
    qDebug("UI started, pid %d", int(getpid()));
}

// A QML error otherwise quits the app at start with nothing to show: write it where the user
// can find it (File Manager > misc, or the app's data folder).
static bool loaded(QmlDocument *document, const char *name)
{
    if (!document->hasErrors())
        return true;
    QFileInfo misc("/accounts/1000/shared/misc");
    QFile log((misc.isDir() && misc.isWritable() ? misc.filePath() : QDir::homePath())
              + "/hyperberry-qml-errors.log");
    if (log.open(QIODevice::WriteOnly | QIODevice::Append)) {
        foreach (const QDeclarativeError &error, document->errors())
            log.write(QString("%1: %2\n").arg(name, error.toString()).toUtf8());
    }
    return false;
}

Q_DECL_EXPORT int main(int argc, char **argv)
{
    Application app(argc, argv);
    startLog();
    qDebug() << "ui: main";
    // The sources are UTF-8; Qt 4 reads tr() and C-string literals as Latin-1 unless told, and
    // "typing…" showed as "typingà|" (Q10, 2026-10-31).
    QTextCodec::setCodecForTr(QTextCodec::codecForName("UTF-8"));
    QTextCodec::setCodecForCStrings(QTextCodec::codecForName("UTF-8"));
    // The phone's language, once at start (no live switching): the .qm is in the bar's assets, and
    // English, the source text, has none. Before the QML loads, whose qsTr reads it then.
    QTranslator translator;
    const QString language = bb::system::LocaleHandler(bb::system::LocaleType::Messages).locale().name().section('_', 0, 0);
    if (translator.load("hyperberry_" + language, QCoreApplication::applicationDirPath() + "/assets/translations"))
        app.installTranslator(&translator);
    EngineLink engine(&app);
    engine.setLanguage(language); // before engine.start() below: the hello sends it
    ContactBook book(&engine, &app);
    ChatListModel chats(&engine, ChatListModel::Chats, &app);
    ChatListModel archivedChats(&engine, ChatListModel::Archived, &app);
    ChatListModel channelChats(&engine, ChatListModel::Channels, &app);
    CallsModel callList(&engine, &app);
    CallAudio callAudio(&engine, &app);
    CallVideo callVideo(&engine, &app);
    Proximity proximity(&app);
    // One for the run: the picker's table is built once, not per chat.
    EmojiModel emojis(&app);
    QrImage qr(&app);
    Settings settings(&engine, &app);
    // With a Hub account (step 5.7) instant previews become possible, but BB10 starts them off,
    // and 5.6's try left the LED off: both on by default, as BBTelega does. apply() leaves alone
    // what the user has changed in Settings -> Notifications (Q10, 0.0.1.100).
    bb::platform::NotificationDefaultApplicationSettings notifyDefaults;
    notifyDefaults.setPreview(bb::platform::NotificationPriorityPolicy::Allow);
    notifyDefaults.setLed(bb::platform::NotificationPolicy::Allow);
    if (notifyDefaults.apply() != bb::platform::NotificationSettingsError::None)
        qWarning() << "notification defaults refused";
    OpenRequests openRequests(engine.invoker(), &app);
    Invites invites(&engine, &app);

    // bb.cascades has no Timer: the call screen's clock and its 2 s close (step 7.4).
    qmlRegisterType<QTimer>("hyperberry", 1, 0, "Timer");
    qmlRegisterType<ChatController>("hyperberry", 1, 0, "ChatController");
    qmlRegisterType<ContactInfo>("hyperberry", 1, 0, "ContactInfo");
    qmlRegisterType<ContactPicker>("hyperberry", 1, 0, "ContactPicker");
    qmlRegisterType<MapChooser>("hyperberry", 1, 0, "MapChooser");
    qmlRegisterType<MediaOpener>("hyperberry", 1, 0, "MediaOpener");
    qmlRegisterType<Profile>("hyperberry", 1, 0, "Profile");
    qmlRegisterType<VideoTest>("hyperberry", 1, 0, "VideoTest");
    qmlRegisterType<CameraTest>("hyperberry", 1, 0, "CameraTest");
    QmlDocument *document = QmlDocument::create("asset:///main.qml").parent(&app);
    if (!loaded(document, "main.qml"))
        return 1;
    document->setContextProperty("engine", &engine);
    document->setContextProperty("chats", &chats);
    document->setContextProperty("archivedChats", &archivedChats);
    document->setContextProperty("channelChats", &channelChats);
    document->setContextProperty("callList", &callList);
    document->setContextProperty("callAudio", &callAudio);
    document->setContextProperty("callVideo", &callVideo);
    document->setContextProperty("proximity", &proximity);
    document->setContextProperty("emojis", &emojis);
    document->setContextProperty("qr", &qr);
    document->setContextProperty("settings", &settings);
    document->setContextProperty("openRequests", &openRequests);
    document->setContextProperty("invites", &invites);
    // The phone's region and language ("IT", "it"): the linking page's first country, and the
    // language of country names.
    QVariantMap phoneLocale;
    phoneLocale["region"] = bb::system::LocaleHandler(bb::system::LocaleType::Region).locale().name().section('_', 1, 1);
    phoneLocale["language"] = bb::system::LocaleHandler(bb::system::LocaleType::Messages).locale().name().section('_', 0, 0);
    document->documentContext()->setContextProperty("phoneLocale", phoneLocale);
    AbstractPane *root = document->createRootObject<AbstractPane>();
    if (!root)
        return 1;
    app.setScene(root);
    // Nothing else starts the engine (no boot start, user 2026-10-29), so the UI starts it on
    // open, and again whenever the link drops.
    engine.start();
    return Application::exec();
}
