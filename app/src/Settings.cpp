#include "Settings.h"

#include <bb/cascades/Application>
#include <bb/cascades/Color>
#include <bb/cascades/ThemeSupport>
#include <bb/cascades/VisualStyle>
#include <QDir>
#include <QDirIterator>
#include <QFile>
#include <QFileInfo>

#include "EngineLink.h"
#include "sizes.h"

Settings::Settings(EngineLink *engine, QObject *parent)
    : QObject(parent),
      m_file(QDir::homePath() + "/settings.ini", QSettings::IniFormat),
      m_engine(engine),
      m_region(bb::system::LocaleType::Region)
{
    // Dark is the bar descriptor's theme (CASCADES_THEME): nothing to apply, and no reload.
    if (theme() == 1)
        applyTheme();
}

int Settings::theme() const { return m_file.value("theme", 0).toInt() == 1 ? 1 : 0; }
bool Settings::enterSends() const { return m_file.value("enterSends", true).toBool(); }
bool Settings::notifyPreview() const { return m_file.value("notifyPreview", true).toBool(); }
// Larger (3, 140 %) went at the user's word (2026-11-01); a saved 3 reads as Large.
int Settings::textSize() const { return qBound(0, m_file.value("textSize", 1).toInt(), 2); }

qreal Settings::textScale() const
{
    static const qreal scales[] = { 0.9, 1.0, 1.2 };
    return scales[textSize()];
}

void Settings::store(const char *key, const QVariant &value)
{
    m_file.setValue(key, value);
    m_file.sync();
}

// With the primary colours of the bar descriptor's CASCADES_THEME, so a switch keeps them.
void Settings::applyTheme()
{
    using namespace bb::cascades;
    Application::instance()->themeSupport()->setVisualStyleAndPrimaryColor(
        theme() == 1 ? VisualStyle::Bright : VisualStyle::Dark, Color::fromARGB(0xff1bd862),
        Color::fromARGB(0xff14a249));
}

void Settings::setTheme(int value)
{
    value = value == 1 ? 1 : 0;
    if (value == theme())
        return;
    store("theme", value);
    applyTheme();
    emit themeChanged();
}

void Settings::setEnterSends(bool on)
{
    if (on == enterSends())
        return;
    store("enterSends", on);
    emit enterSendsChanged();
}

void Settings::setNotifyPreview(bool on)
{
    if (on == notifyPreview())
        return;
    store("notifyPreview", on);
    // Not connected: the hello path sends it on the next connect.
    if (m_engine->connected()) {
        QVariantMap prefs;
        prefs["notifyPreview"] = on;
        m_engine->request("prefs.set", prefs);
    }
    emit notifyPreviewChanged();
}

void Settings::setTextSize(int size)
{
    size = qBound(0, size, 2);
    if (size == textSize())
        return;
    store("textSize", size);
    emit textSizeChanged();
}

// The cache is data/cache/ except upload/, which holds an upload in progress. Everything else in
// it is made again when needed (a voice note's WAV on its next play, a photo's preview on its next
// showing). Collected first, so nothing is deleted while the iterator is reading the directory.
static QStringList cacheFiles()
{
    const QString root = QDir::homePath() + "/cache";
    QStringList files;
    QDirIterator it(root, QDir::Files | QDir::Hidden, QDirIterator::Subdirectories);
    while (it.hasNext()) {
        const QString path = it.next();
        if (!path.startsWith(root + "/upload/"))
            files << path;
    }
    return files;
}

// An empty cache reads "0 KB", not the empty text sizeText gives for an unknown size.
static QString cacheText(qint64 bytes, const QLocale &locale)
{
    return bytes > 0 ? sizeText(bytes, locale) : QString("0 KB");
}

QString Settings::cacheSize()
{
    qint64 bytes = 0;
    foreach (const QString &path, cacheFiles())
        bytes += QFileInfo(path).size();
    return cacheText(bytes, m_region.locale());
}

// Returns the size freed; a file that can't be removed is skipped. Empty directories stay.
QString Settings::clearCache()
{
    qint64 freed = 0;
    foreach (const QString &path, cacheFiles()) {
        const qint64 size = QFileInfo(path).size();
        if (QFile::remove(path))
            freed += size;
    }
    return cacheText(freed, m_region.locale());
}
