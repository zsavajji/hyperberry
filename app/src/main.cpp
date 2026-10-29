#include <bb/cascades/AbstractPane>
#include <bb/cascades/Application>
#include <bb/cascades/QmlDocument>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QtDeclarative>

#include "EngineLink.h"
#include "QrImage.h"

using namespace bb::cascades;

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
    EngineLink engine(&app);
    QrImage qr(&app);

    QmlDocument *document = QmlDocument::create("asset:///main.qml").parent(&app);
    if (!loaded(document, "main.qml"))
        return 1;
    document->setContextProperty("engine", &engine);
    document->setContextProperty("qr", &qr);
    AbstractPane *root = document->createRootObject<AbstractPane>();
    if (!root)
        return 1;
    app.setScene(root);
    // Nothing else starts the engine (no boot start, user 2026-10-29), so the UI starts it on
    // open, and again whenever the link drops.
    engine.start();
    return Application::exec();
}
