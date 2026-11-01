#ifndef SETTINGS_H
#define SETTINGS_H

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
    Q_PROPERTY(int textSize READ textSize WRITE setTextSize NOTIFY textSizeChanged) // 0..3
    Q_PROPERTY(qreal textScale READ textScale NOTIFY textSizeChanged)
    // The LED's colour per kind of chat (step 5.6): chats, groups, channels; 0x00rrggbb, 0 off.
    Q_PROPERTY(QVariantList leds READ leds NOTIFY ledsChanged)

public:
    explicit Settings(EngineLink *engine, QObject *parent = 0);
    int theme() const;
    bool enterSends() const;
    bool notifyPreview() const;
    int textSize() const;
    qreal textScale() const;
    QVariantList leds() const;
    void setTheme(int theme);
    void setEnterSends(bool on);
    void setNotifyPreview(bool on);
    void setTextSize(int size);
    Q_INVOKABLE void setLed(int kind, int rgb);

signals:
    void themeChanged();
    void enterSendsChanged();
    void notifyPreviewChanged();
    void textSizeChanged();
    void ledsChanged();

private:
    void applyTheme();
    void store(const char *key, const QVariant &value);
    QSettings m_file;
    EngineLink *m_engine;
};

#endif
