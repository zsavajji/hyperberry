#include "Settings.h"

#include <bb/cascades/Application>
#include <bb/cascades/Color>
#include <bb/cascades/ThemeSupport>
#include <bb/cascades/VisualStyle>
#include <bb/platform/Notification>
#include <bb/system/InvokeDateTime>
#include <bb/system/InvokeManager>
#include <bb/system/InvokeRecurrenceRule>
#include <bb/system/InvokeRecurrenceRuleFrequency>
#include <bb/system/InvokeReply>
#include <bb/system/InvokeReplyError>
#include <bb/system/InvokeTimerRequest>
#include <QDate>
#include <QDebug>
#include <QDir>
#include <QDirIterator>
#include <QFile>
#include <QFileInfo>
#include <QTime>
#include <QTimer>
#include <QUrl>

#include "EngineLink.h"
#include "sizes.h"

Settings::Settings(EngineLink *engine, QObject *parent)
    : QObject(parent),
      m_file(QDir::homePath() + "/settings.ini", QSettings::IniFormat),
      m_banner(0),
      m_engine(engine),
      m_region(bb::system::LocaleType::Region)
{
    // Dark is the bar descriptor's theme (CASCADES_THEME): nothing to apply, and no reload.
    if (theme() == 1)
        applyTheme();
    // Refused while the timer stands (2, 12292, at every start on the Q10), though navigator_invoke.h
    // says a new one replaces the old. Harmless: it survived a reboot and about ten reinstalls. It
    // should add the timer again if one is ever lost, which is unproven.
    if (startAtBoot())
        applyBootTimer(true);
}

int Settings::theme() const { return m_file.value("theme", 0).toInt() == 1 ? 1 : 0; }
bool Settings::enterSends() const { return m_file.value("enterSends", true).toBool(); }
bool Settings::notifyPreview() const { return m_file.value("notifyPreview", true).toBool(); }
bool Settings::startAtBoot() const { return m_file.value("startAtBoot", false).toBool(); }
bool Settings::mediaOnMobile() const { return m_file.value("mediaOnMobile", true).toBool(); }
int Settings::cacheLimit() const { return qBound(64, m_file.value("cacheLimit", 64).toInt(), 512); }
// Larger (3, 140 %) went at the user's word (2026-11-01); a saved 3 reads as Large.
int Settings::textSize() const { return qBound(0, m_file.value("textSize", 1).toInt(), 2); }
int Settings::callHints() const { return qMax(0, m_file.value("callHints", 0).toInt()); }

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

void Settings::setStartAtBoot(bool on)
{
    if (on == startAtBoot())
        return;
    store("startAtBoot", on);
    applyBootTimer(on);
    emit startAtBootChanged();
}

// Start at boot (step 8.2). It can't happen at boot itself: an unsigned app runs only once root is
// up. So a recurring timer on the engine's headless target, which survives reboots
// (navigator_invoke.h): the fires before root is up fail, the first after starts the engine, and one
// that finds it running does nothing, so a killed engine comes back too. Every 6 minutes is the
// shortest the service takes (InvokeRecurrenceRule.hpp).
static const char *const kBootTimer = "hyperberry-boot";

void Settings::applyBootTimer(bool on)
{
    bb::system::InvokeManager *invoker = m_engine->invoker();
    bb::system::InvokeReply *reply = 0;
    if (on) {
        bb::system::InvokeRecurrenceRule rule(bb::system::InvokeRecurrenceRuleFrequency::Minutely,
                                              bb::system::InvokeDateTime(QDate::currentDate(), QTime(0, 0)));
        rule.setInterval(6);
        if (!rule.isValid()) {
            qWarning() << "boot timer: the rule is not valid";
            return;
        }
        reply = invoker->registerTimer(bb::system::InvokeTimerRequest(kBootTimer, rule, "com.zsavajji.hyperberry.service"));
    } else {
        reply = invoker->deregisterTimer(kBootTimer);
    }
    if (!reply) {
        qWarning() << "boot timer: request not sent";
        return;
    }
    reply->setProperty("on", on);
    connect(reply, SIGNAL(finished()), this, SLOT(bootTimerReplied()));
}

void Settings::bootTimerReplied()
{
    bb::system::InvokeReply *reply = qobject_cast<bb::system::InvokeReply *>(sender());
    if (!reply)
        return;
    if (reply->error() == bb::system::InvokeReplyError::None)
        qDebug() << "boot timer:" << (reply->property("on").toBool() ? "registered" : "removed");
    else
        qWarning() << "boot timer: failed," << int(reply->error()) << reply->errorCode();
    reply->deleteLater();
}

void Settings::setMediaOnMobile(bool on)
{
    if (on == mediaOnMobile())
        return;
    store("mediaOnMobile", on);
    // Not connected: the hello path sends it on the next connect.
    if (m_engine->connected()) {
        QVariantMap prefs;
        prefs["mediaOnMobile"] = on;
        m_engine->request("prefs.set", prefs);
    }
    emit mediaOnMobileChanged();
}

void Settings::setCacheLimit(int mb)
{
    mb = qBound(64, mb, 512);
    if (mb == cacheLimit())
        return;
    store("cacheLimit", mb);
    // Not connected: the hello path sends it on the next connect. The engine prunes, since it decodes.
    if (m_engine->connected()) {
        QVariantMap prefs;
        prefs["cacheMB"] = mb;
        m_engine->request("prefs.set", prefs);
    }
    emit cacheLimitChanged();
}

void Settings::setTextSize(int size)
{
    size = qBound(0, size, 2);
    if (size == textSize())
        return;
    store("textSize", size);
    emit textSizeChanged();
}

void Settings::setCallHints(int count)
{
    count = qMax(0, count);
    if (count == callHints())
        return;
    store("callHints", count);
    emit callHintsChanged();
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

void Settings::testBanner()
{
    if (m_banner)
        return;
    bb::platform::Notification *n = new bb::platform::Notification(this);
    // The public assets: the sandbox's app link is /apps/<package>, the Hub's icons in public/.
    const QString icon = QFileInfo(QDir::current().absoluteFilePath("app")).canonicalFilePath() + "/public/hub.png";
    n->setTitle("HyperBerry");
    n->setBody(tr("Test banner: is this icon grey?"));
    n->setIconUrl(QUrl::fromLocalFile(icon));
    const int rc = n->notify();
    qDebug("test banner: notify -> %d, icon %s (exists %d)", rc, qPrintable(n->iconUrl().toString()), int(QFile::exists(icon)));
    m_banner = n;
    QTimer::singleShot(10000, this, SLOT(dropBanner()));
}

void Settings::dropBanner()
{
    bb::platform::Notification *n = static_cast<bb::platform::Notification *>(m_banner);
    m_banner = 0;
    if (!n)
        return;
    qDebug("test banner: delete from the Hub -> %d", int(n->deleteFromInbox()));
    n->deleteLater();
}
