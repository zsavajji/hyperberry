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
    // What downloads by itself when it arrives (the engine's prefs, on top of mediaOnMobile): one signal for the four.
    Q_PROPERTY(bool autoPhoto READ autoPhoto WRITE setAutoPhoto NOTIFY autoDownloadChanged)
    Q_PROPERTY(bool autoAudio READ autoAudio WRITE setAutoAudio NOTIFY autoDownloadChanged) // voice notes and audio files
    Q_PROPERTY(bool autoVideo READ autoVideo WRITE setAutoVideo NOTIFY autoDownloadChanged)
    Q_PROPERTY(bool autoDoc READ autoDoc WRITE setAutoDoc NOTIFY autoDownloadChanged)
    // Off for a phone whose proximity sensor misreads: voice notes stay on the speaker, the call screen never blanks.
    // The engine's lookups through Cloudflare (1.1.1.1, 1.0.0.1) instead of the network's DNS; off by default.
    Q_PROPERTY(bool cloudflareDns READ cloudflareDns WRITE setCloudflareDns NOTIFY cloudflareDnsChanged)
    Q_PROPERTY(bool earSensor READ earSensor WRITE setEarSensor NOTIFY earSensorChanged)
    Q_PROPERTY(int cacheLimit READ cacheLimit WRITE setCacheLimit NOTIFY cacheLimitChanged)         // MB
    Q_PROPERTY(int textSize READ textSize WRITE setTextSize NOTIFY textSizeChanged) // 0..2
    Q_PROPERTY(qreal textScale READ textScale NOTIFY textSizeChanged)
    // Interface: Normal (false) has the chat rows' bigger pictures and padding, Compact the old rows (ChatRows.cpp).
    Q_PROPERTY(bool compact READ compact WRITE setCompact NOTIFY compactChanged)
    // The Dark theme's page background (the user's ask, 2026-11-19): pure black (false, the look so far) or #181818, like Android's.
    // main.qml's pageBackground reads it; the Bright theme has no such choice.
    Q_PROPERTY(bool darkGrey READ darkGrey WRITE setDarkGrey NOTIFY darkGreyChanged)
    // How many group calls showed the people drawer's first-time hint (CallDrawer.qml shows it in the first 2).
    Q_PROPERTY(int callHints READ callHints WRITE setCallHints NOTIFY callHintsChanged)
    // Video calls, our camera: 0 is 1280x720, 1 is 640x480 (default), 2 is 320x240 (CallVideo's quality).
    Q_PROPERTY(int videoQuality READ videoQuality WRITE setVideoQuality NOTIFY videoQualityChanged)

public:
    explicit Settings(EngineLink *engine, QObject *parent = 0);
    int theme() const;
    bool enterSends() const;
    bool notifyPreview() const;
    bool startAtBoot() const;
    bool mediaOnMobile() const;
    bool autoPhoto() const;
    bool autoAudio() const;
    bool autoVideo() const;
    bool autoDoc() const;
    bool earSensor() const;
    bool cloudflareDns() const;
    int cacheLimit() const;
    int textSize() const;
    qreal textScale() const;
    bool compact() const;
    bool darkGrey() const;
    int callHints() const;
    int videoQuality() const;
    void setTheme(int theme);
    void setEnterSends(bool on);
    void setNotifyPreview(bool on);
    void setStartAtBoot(bool on);
    void setMediaOnMobile(bool on);
    void setAutoPhoto(bool on);
    void setAutoAudio(bool on);
    void setAutoVideo(bool on);
    void setAutoDoc(bool on);
    void setCloudflareDns(bool on);
    void setEarSensor(bool on);
    void setCacheLimit(int mb);
    void setTextSize(int size);
    void setCompact(bool on);
    void setDarkGrey(bool on);
    void setCallHints(int count);
    void setVideoQuality(int quality);
    Q_INVOKABLE QString cacheSize();
    Q_INVOKABLE QString clearCache();

signals:
    void themeChanged();
    void enterSendsChanged();
    void notifyPreviewChanged();
    void startAtBootChanged();
    void mediaOnMobileChanged();
    void cloudflareDnsChanged();
    void autoDownloadChanged();
    void earSensorChanged();
    void cacheLimitChanged();
    void textSizeChanged();
    void compactChanged();
    void darkGreyChanged();
    void callHintsChanged();
    void videoQualityChanged(int quality);

private slots:
    void bootTimerReplied();

private:
    void applyTheme();
    void applyBootTimer(bool on);
    void store(const char *key, const QVariant &value);
    void storePref(const char *key, bool on);
    QSettings m_file;
    EngineLink *m_engine;
    bb::system::LocaleHandler m_region; // for the size text, as the chat models have it
};

#endif
