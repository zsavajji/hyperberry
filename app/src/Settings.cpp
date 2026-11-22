#include "Settings.h"

#include <bb/cascades/Application>
#include <bb/cascades/Color>
#include <bb/cascades/ThemeSupport>
#include <bb/cascades/VisualStyle>
#include <bb/device/DisplayInfo>
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

#include "EngineLink.h"
#include "StickerPlayer.h"
#include "sizes.h"

int Settings::s_stickerPlay = 0; // Loop, as stickerPlay() reads it

Settings::Settings(EngineLink *engine, QObject *parent)
    : QObject(parent),
      m_file(QDir::homePath() + "/settings.ini", QSettings::IniFormat),
      m_engine(engine),
      m_region(bb::system::LocaleType::Region)
{
    s_stickerPlay = stickerPlay();
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
bool Settings::autoPhoto() const { return m_file.value("autoPhoto", true).toBool(); }
bool Settings::autoAudio() const { return m_file.value("autoAudio", true).toBool(); }
bool Settings::autoVideo() const { return m_file.value("autoVideo", false).toBool(); }
bool Settings::autoDoc() const { return m_file.value("autoDoc", false).toBool(); }
bool Settings::earSensor() const { return m_file.value("earSensor", true).toBool(); }
bool Settings::cloudflareDns() const { return m_file.value("cloudflareDNS", false).toBool(); }
int Settings::cacheLimit() const { return qBound(64, m_file.value("cacheLimit", 64).toInt(), 512); }
// Larger (3, 140 %) went at the user's word (2026-11-01); a saved 3 reads as Large.
int Settings::textSize() const { return qBound(0, m_file.value("textSize", 1).toInt(), 2); }
bool Settings::compact() const { return m_file.value("compact", false).toBool(); }
// Grey by default on an LCD (the Classic, the Passport, the user's ask, 2026-11-19): black only saves power on an OLED,
// the Q10's. The Passport also by its 1440 px screen, in case it reports no technology.
bool Settings::darkGrey() const
{
    static const bool lcd = bb::device::DisplayInfo().displayTechnology() == bb::device::DisplayTechnology::Lcd
                            || bb::device::DisplayInfo().pixelSize().width() == 1440;
    return m_file.value("darkGrey", lcd).toBool();
}
int Settings::callHints() const { return qMax(0, m_file.value("callHints", 0).toInt()); }
int Settings::stickerPlay() const { return qBound(0, m_file.value("stickerPlay", 0).toInt(), 2); }
int Settings::videoQuality() const { return qBound(0, m_file.value("videoQuality", 1).toInt(), 2); }
bool Settings::cameraCrop() const { return m_file.value("cameraCrop", false).toBool(); }
QString Settings::translateProvider() const { return m_file.value("translateProvider", "google").toString(); }
QString Settings::googleKey() const { return m_file.value("googleKey").toString(); }
QString Settings::deeplKey() const { return m_file.value("deeplKey").toString(); }
QString Settings::libreURL() const { return m_file.value("libreURL").toString(); }
QString Settings::libreKey() const { return m_file.value("libreKey").toString(); }

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

// The accent per theme (the user's, 2026-11-17): #1eaa61 on Bright, its base 20 % darker; #12543a on Dark, its
// base the old bright green, in case the trackpad's highlight follows the base (it was too dark with a dark one).
// Dark's is the bar descriptor's CASCADES_THEME, the colours before this runs.
void Settings::applyTheme()
{
    using namespace bb::cascades;
    const bool bright = theme() == 1;
    Application::instance()->themeSupport()->setVisualStyleAndPrimaryColor(
        bright ? VisualStyle::Bright : VisualStyle::Dark, Color::fromARGB(bright ? 0xff1eaa61 : 0xff12543a),
        Color::fromARGB(bright ? 0xff18884e : 0xff1bd862));
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

void Settings::setEarSensor(bool on)
{
    if (on == earSensor())
        return;
    store("earSensor", on);
    emit earSensorChanged();
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

// A setting the engine keeps too (prefs.set): stored here, and sent when connected.
void Settings::storePref(const char *key, const QVariant &value)
{
    store(key, value);
    // Not connected: the hello path sends it on the next connect.
    if (m_engine->connected()) {
        QVariantMap prefs;
        prefs[key] = value;
        m_engine->request("prefs.set", prefs);
    }
}

void Settings::setTranslate(const char *key, const QString &value)
{
    if (value == m_file.value(key, QString(key) == "translateProvider" ? "google" : "").toString())
        return;
    storePref(key, value);
    emit translateChanged();
}

void Settings::setTranslateProvider(const QString &value) { setTranslate("translateProvider", value); }
void Settings::setGoogleKey(const QString &value) { setTranslate("googleKey", value); }
void Settings::setDeeplKey(const QString &value) { setTranslate("deeplKey", value); }
void Settings::setLibreURL(const QString &value) { setTranslate("libreURL", value); }
void Settings::setLibreKey(const QString &value) { setTranslate("libreKey", value); }

void Settings::setMediaOnMobile(bool on)
{
    if (on == mediaOnMobile())
        return;
    storePref("mediaOnMobile", on);
    emit mediaOnMobileChanged();
}

void Settings::setCloudflareDns(bool on)
{
    if (on == cloudflareDns())
        return;
    storePref("cloudflareDNS", on);
    emit cloudflareDnsChanged();
}

void Settings::setAutoPhoto(bool on)
{
    if (on == autoPhoto())
        return;
    storePref("autoPhoto", on);
    emit autoDownloadChanged();
}

void Settings::setAutoAudio(bool on)
{
    if (on == autoAudio())
        return;
    storePref("autoAudio", on);
    emit autoDownloadChanged();
}

void Settings::setAutoVideo(bool on)
{
    if (on == autoVideo())
        return;
    storePref("autoVideo", on);
    emit autoDownloadChanged();
}

void Settings::setAutoDoc(bool on)
{
    if (on == autoDoc())
        return;
    storePref("autoDoc", on);
    emit autoDownloadChanged();
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

void Settings::setCompact(bool on)
{
    if (on == compact())
        return;
    store("compact", on);
    emit compactChanged();
}

void Settings::setDarkGrey(bool on)
{
    if (on == darkGrey())
        return;
    store("darkGrey", on);
    emit darkGreyChanged();
}

void Settings::setCallHints(int count)
{
    count = qMax(0, count);
    if (count == callHints())
        return;
    store("callHints", count);
    emit callHintsChanged();
}

void Settings::setStickerPlay(int mode)
{
    mode = qBound(0, mode, 2);
    if (mode == stickerPlay())
        return;
    store("stickerPlay", mode);
    s_stickerPlay = mode;
    emit stickerPlayChanged();
}

void Settings::coverStickers(bool covered)
{
    StickerPlayer::setCovered(covered);
}

void Settings::setVideoQuality(int quality)
{
    quality = qBound(0, quality, 2);
    if (quality == videoQuality())
        return;
    store("videoQuality", quality);
    emit videoQualityChanged(quality);
}

void Settings::setCameraCrop(bool on)
{
    if (on == cameraCrop())
        return;
    store("cameraCrop", on);
    emit cameraCropChanged(on);
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
