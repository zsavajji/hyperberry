#ifndef MEDIAOPENER_H
#define MEDIAOPENER_H

#include <bb/system/InvokeManager>
#include <QObject>
#include <QPointer>
#include <QString>

class EngineLink;

// Fetches a received file and opens it (step 6.1), made by QML in the chat page's attachedObjects:
// the request's owner dies with the page, so no connection to the engine outlives it.
class MediaOpener : public QObject
{
    Q_OBJECT
    // The EngineLink, set as ContactInfo's link is.
    Q_PROPERTY(QObject *link READ link WRITE setLink)
    // The message being fetched, "" when none: its bubble shows a spinner.
    Q_PROPERTY(QString loadingId READ loadingId NOTIFY loadingIdChanged)
public:
    explicit MediaOpener(QObject *parent = 0);

    QObject *link() const;
    void setLink(QObject *link);
    QString loadingId() const;

    // Asks the engine for the file of message id (media.get); ready() follows. A failure shows in
    // the engine's failure. A newer get replaces an older one, whose reply is dropped.
    Q_INVOKABLE void get(const QString &chat, const QString &id, const QString &kind);
    // Hands the file to the system (bb.action.OPEN); false when the system gave no reply. Whether
    // an app opens it is not known.
    Q_INVOKABLE bool openFile(const QString &path);
    // Copies a photo into Pictures (shared/photos/HyperBerry/); the copy's path, "" on failure.
    Q_INVOKABLE QString saveToPictures(const QString &path);

signals:
    void loadingIdChanged();
    // The file of message id is at path; kind is what get() was given.
    void ready(const QString &id, const QString &kind, const QString &path);
    // The system found no app to open a file, or the app refused it.
    void openFailed();

private slots:
    void onReplied(const QString &id, const QString &cmd, const QVariant &result);
    void onFailure();
    void onInvoked();

private:
    void setLoadingId(const QString &id);

    QPointer<EngineLink> m_engine;
    bb::system::InvokeManager m_invoker;
    QString m_loadingId;
    QString m_kind;
    QString m_reqId; // the media.get request we wait for
};

#endif
