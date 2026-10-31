#include "OpenRequests.h"

using namespace bb::system;

// The invocation that started the app is delivered through the same signal, once the event
// loop runs: main() connects before exec().
OpenRequests::OpenRequests(InvokeManager *manager, QObject *parent)
    : QObject(parent)
{
    connect(manager, SIGNAL(invoked(const bb::system::InvokeRequest &)), this,
            SLOT(invoked(const bb::system::InvokeRequest &)));
}

void OpenRequests::invoked(const InvokeRequest &request)
{
    if (request.action() == "com.zsavajji.hyperberry.OPEN")
        emit openChat(QString::fromUtf8(request.data().constData(), request.data().size()));
}
