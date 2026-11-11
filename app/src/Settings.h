#ifndef SETTINGS_H
#define SETTINGS_H

#include <bb/system/LocaleHandler>
#include <QObject>
#include <QSettings>

class EngineLink;

// The user's settings (step 5.4), in settings.ini, which EngineLink reads with its own QSettings:
// each write is synced at once. QML takes them as `settings`.
class Settings : public QObject
{
    Q_OBJECT
    Q_PROPERTY(int theme READ theme WRITE setTheme NOTIFY themeChanged)             // 0 Dark, 1 Bright
    Q_PROPERTY(bool enterSends READ enterSends WRITE setEnterSends NOTIFY enterSendsChanged)
    Q_PROPERTY(bool notifyPreview READ notifyPreview WRITE setNotifyPreview NOTIFY notifyPreviewChanged)
    Q_PROPERTY(bool startAtBoot READ startAtBoot WRITE setStartAtBoot NOTIFY startAtBootChanged)
    Q_PROPERTY(bool mediaOnMobile READ mediaOnMobile WRITE setMediaOnMobile NOTIFY mediaOnMobileChanged)
    Q_PROPERTY(int cacheLimit READ cacheLimit WRITE setCacheLimit NOTIFY cacheLimitChanged)         // MB
    Q_PROPERTY(int textSize READ textSize WRITE setTextSize NOTIFY textSizeChanged) // 0..2
    Q_PROPERTY(qreal textScale READ textScale NOTIFY textSizeChanged)
    // How many group calls showed the people drawer's first-time hint (CallDrawer.qml shows it in the first 2).
    Q_PROPERTY(int callHints READ callHints WRITE setCallHints NOTIFY callHintsChanged)

public:
    explicit Settings(EngineLink *engine, QObject *parent = 0);
    int theme() const;
    bool enterSends() const;
    bool notifyPreview() const;
    bool startAtBoot() const;
    bool mediaOnMobile() const;
    int cacheLimit() const;
    int textSize() const;
    qreal textScale() const;
    int callHints() const;
    void setTheme(int theme);
    void setEnterSends(bool on);
    void setNotifyPreview(bool on);
    void setStartAtBoot(bool on);
    void setMediaOnMobile(bool on);
    void setCacheLimit(int mb);
    void setTextSize(int size);
    void setCallHints(int count);
    Q_INVOKABLE QString cacheSize();
    Q_INVOKABLE QString clearCache();
    // A developer test: one BB10 notification with the grey hub.png as its iconUrl (the SDK: "the
    // icon to be shown in the preview"), taken back from the Hub after 10 s. Whether the instant
    // preview draws it, where the Hub account's white icon shows today.
    Q_INVOKABLE void testBanner();

signals:
    void themeChanged();
    void enterSendsChanged();
    void notifyPreviewChanged();
    void startAtBootChanged();
    void mediaOnMobileChanged();
    void cacheLimitChanged();
    void textSizeChanged();
    void callHintsChanged();

private slots:
    void bootTimerReplied();
    void dropBanner();

private:
    void applyTheme();
    void applyBootTimer(bool on);
    void store(const char *key, const QVariant &value);
    QSettings m_file;
    QObject *m_banner; // testBanner()'s notification, until dropBanner()
    EngineLink *m_engine;
    bb::system::LocaleHandler m_region; // for the size text, as the chat models have it
};

#endif
