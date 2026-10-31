#ifndef OPENREQUESTS_H
#define OPENREQUESTS_H

#include <bb/system/InvokeManager>
#include <bb/system/InvokeRequest>
#include <QObject>
#include <QString>

// A tap on the engine's notification (engine/notify_qnx.go) starts or resumes the app with
// action com.zsavajji.hyperberry.OPEN and the chat key as the payload. The signal reaches QML
// as it comes; main.qml holds it until the chat list is up.
class OpenRequests : public QObject
{
    Q_OBJECT
public:
    // EngineLink's manager: one per app, so an invocation can't go to an instance nobody
    // listens on.
    OpenRequests(bb::system::InvokeManager *manager, QObject *parent = 0);

signals:
    void openChat(const QString &jid);

private slots:
    void invoked(const bb::system::InvokeRequest &request);
};

#endif
