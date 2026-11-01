#include <bb/cascades/AbstractPane>
#include <bb/cascades/Application>
#include <bb/cascades/QmlDocument>
#include <bb/platform/NotificationDefaultApplicationSettings>
#include <bb/system/LocaleHandler>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QTextCodec>
#include <QTranslator>
#include <QtDeclarative>
#include <sys/slog2.h>

#include "ChatController.h"
#include "ChatListModel.h"
#include "ContactPicker.h"
#include "EmojiModel.h"
#include "EngineLink.h"
#include "OpenRequests.h"
#include "QrImage.h"
#include "Settings.h"

using namespace bb::cascades;

// qDebug and qWarning, QML's errors included, go nowhere on the phone by themselves: they go to
// the system log, where `slog2info -b hyperberry` shows them.
static slog2_buffer_t logBuffer;

static void toSystemLog(QtMsgType type, const char *msg)
{
    slog2c(logBuffer, 0, type == QtDebugMsg ? SLOG2_INFO : SLOG2_WARNING, msg);
}

static void logToSystem()
{
    slog2_buffer_set_config_t config = {};
    config.num_buffers = 1;
    config.buffer_set_name = "hyperberry";
    config.verbosity_level = SLOG2_INFO;
    config.buffer_config[0].buffer_name = "ui";
    config.buffer_config[0].num_pages = 4;
    if (slog2_register(&config, &logBuffer, 0) == 0)
        qInstallMsgHandler(toSystemLog);
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
    logToSystem();
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
    ChatListModel chats(&engine, false, &app);
    ChatListModel archivedChats(&engine, true, &app);
    // One for the run: the picker's table is built once, not per chat.
    EmojiModel emojis(&app);
    QrImage qr(&app);
    Settings settings(&engine, &app);
    // The engine lights the LED in the user's colours (step 5.6), so the system's own red for our
    // notifications is off by default. apply() leaves alone settings the user has changed.
    bb::platform::NotificationDefaultApplicationSettings notifyDefaults;
    notifyDefaults.setLed(bb::platform::NotificationPolicy::Deny);
    notifyDefaults.apply();
    OpenRequests openRequests(engine.invoker(), &app);

    qmlRegisterType<ChatController>("hyperberry", 1, 0, "ChatController");
    qmlRegisterType<ContactPicker>("hyperberry", 1, 0, "ContactPicker");
    QmlDocument *document = QmlDocument::create("asset:///main.qml").parent(&app);
    if (!loaded(document, "main.qml"))
        return 1;
    document->setContextProperty("engine", &engine);
    document->setContextProperty("chats", &chats);
    document->setContextProperty("archivedChats", &archivedChats);
    document->setContextProperty("emojis", &emojis);
    document->setContextProperty("qr", &qr);
    document->setContextProperty("settings", &settings);
    document->setContextProperty("openRequests", &openRequests);
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
