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

public:
    explicit Settings(EngineLink *engine, QObject *parent = 0);
    int theme() const;
    bool enterSends() const;
    bool notifyPreview() const;
    int textSize() const;
    qreal textScale() const;
    void setTheme(int theme);
    void setEnterSends(bool on);
    void setNotifyPreview(bool on);
    void setTextSize(int size);

signals:
    void themeChanged();
    void enterSendsChanged();
    void notifyPreviewChanged();
    void textSizeChanged();

private:
    void applyTheme();
    void store(const char *key, const QVariant &value);
    QSettings m_file;
    EngineLink *m_engine;
};

#endif
